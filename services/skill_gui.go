package services

import (
	"fmt"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"

	"pvfine/internal/pvf"
)

// Placeholder spellings describe the template itself, independently of the
// archive container. Normalize aliases once so all GUI consumers share the
// same integer / decimal formatting contract, including mixed templates.
var skillNumericPlaceholder = regexp.MustCompile(`<(?:(int|quorum)|(float|decimal)(\d*))>`)

func normalizeSkillTemplate(value string) string {
	return skillNumericPlaceholder.ReplaceAllStringFunc(normalizeDisplayText(value), func(placeholder string) string {
		parts := skillNumericPlaceholder.FindStringSubmatch(placeholder)
		if parts[1] != "" {
			return "<int>"
		}
		return "<float" + parts[3] + ">"
	})
}

func isSkillTemplateToken(kind int32) bool { return kind == 6 || kind == 8 || kind == 10 }

// SkillNumber retains exact UTF-16 source positions so GUI edits preserve all
// unrelated tags, formatting, and numeric token types in the editor draft.
type SkillNumber struct {
	Value     float64 `json:"value"`
	Start     int     `json:"start"`
	End       int     `json:"end"`
	TokenType int32   `json:"tokenType"`
}
type SkillProperty struct {
	Template string                 `json:"template"`
	Bindings []SkillPropertyBinding `json:"bindings"`
}
type SkillPropertyBinding struct {
	Dynamic    bool    `json:"dynamic"`
	Index      int     `json:"index"`
	Multiplier float64 `json:"multiplier"`
}
type SkillMode struct {
	ID         string          `json:"id"`
	Static     []SkillNumber   `json:"static"`
	Width      int             `json:"width"`
	Levels     [][]SkillNumber `json:"levels"`
	Properties []SkillProperty `json:"properties"`
	Issues     []string        `json:"issues"`
}
type SkillDocument struct {
	Name  string      `json:"name"`
	Modes []SkillMode `json:"modes"`
}
type SkillEntry struct {
	FileIndex int32  `json:"fileIndex"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Job       string `json:"job"`
}

func (s *FileGUIService) ListSkills() ([]SkillEntry, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	entries := []SkillEntry{}
	for i := int32(0); i < a.FileCount(); i++ {
		p := a.Path(i)
		if !strings.HasPrefix(strings.ToLower(p), "skill/") || !strings.EqualFold(path.Ext(p), ".skl") {
			continue
		}
		name := path.Base(p)
		if m, err := a.ScriptMetadata(i); err == nil && m.HasName {
			name = resolvePreviewText(a, m.Name)
		}
		parts := strings.Split(p, "/")
		job := "其他"
		if len(parts) > 2 && strings.EqualFold(parts[0], "skill") {
			job = parts[1]
		}
		entries = append(entries, SkillEntry{FileIndex: i, Path: p, Name: name, Job: job})
	}
	return entries, nil
}

func (s *FileGUIService) ReadSkill(fileIndex int32, text string) (*SkillDocument, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	if err := validateAnnotationIndex(a, fileIndex); err != nil {
		return nil, err
	}
	if !strings.EqualFold(path.Ext(a.Path(fileIndex)), ".skl") {
		return nil, fmt.Errorf("技能 GUI 仅支持 .skl 文件")
	}
	if len(text) > int(s.c.editableByteLimit()) {
		return nil, fmt.Errorf("技能文本过大")
	}
	doc := parseSkillWithTemplates(text, func(value string) string { return resolvePreviewDescription(a, value) })
	doc.Name = resolvePreviewText(a, doc.Name)
	return doc, nil
}

func parseSkill(text string) *SkillDocument {
	return parseSkillWithTemplates(text, nil)
}

func parseSkillWithTemplates(text string, resolve func(string) string) *SkillDocument {
	view := pvf.ParseScriptView(text)
	normalizeViewSkillTemplates(&view, resolve)

	scopes := collectSkillTokensByScope(&view)
	doc := &SkillDocument{Name: scopes.name, Modes: []SkillMode{}}

	sharedIssues := []string{}
	sharedProperties := []SkillProperty{}
	for _, group := range scopes.root.property {
		sharedProperties = append(sharedProperties, parseSkillProperties(group, &sharedIssues)...)
	}

	rootStaticIssues := []string{}
	rootStaticNumbers := parseSkillStatic(scopes.root.static, &rootStaticIssues)

	hasDungeon := scopes.hasExplicit["dungeon"] || len(scopes.root.levelInfo) > 0 || len(scopes.root.static) > 0
	hasPvp := scopes.hasExplicit["pvp"]

	specs := []struct {
		id      string
		present bool
	}{
		{id: "dungeon", present: hasDungeon},
		{id: "pvp", present: hasPvp},
	}

	for _, spec := range specs {
		if !spec.present {
			continue
		}
		mode := buildSkillMode(
			spec.id,
			scopes.modes[spec.id],
			&scopes.root,
			rootStaticNumbers,
			rootStaticIssues,
			sharedProperties,
			sharedIssues,
		)
		doc.Modes = append(doc.Modes, mode)
	}
	return doc
}

type skillSectionGroups struct {
	static         [][]pvf.ScriptElement
	levelInfo      [][]pvf.ScriptElement
	property       [][]pvf.ScriptElement
	lastStaticID   int
	lastLevelID    int
	lastPropertyID int
}

func newSkillSectionGroups() *skillSectionGroups {
	return &skillSectionGroups{
		lastStaticID:   -1,
		lastLevelID:    -1,
		lastPropertyID: -1,
	}
}

func appendSkillTokenGroup(groups *[][]pvf.ScriptElement, lastID *int, e pvf.ScriptElement) {
	if *lastID != e.SectionID {
		*groups = append(*groups, []pvf.ScriptElement{})
		*lastID = e.SectionID
	}
	(*groups)[len(*groups)-1] = append((*groups)[len(*groups)-1], e)
}

type skillTokensByScope struct {
	name        string
	root        skillSectionGroups
	modes       map[string]*skillSectionGroups
	hasExplicit map[string]bool
}

func normalizeViewSkillTemplates(view *pvf.ScriptView, resolve func(string) string) {
	for i := range view.Elements {
		e := &view.Elements[i]
		if e.Kind != pvf.ScriptElementToken || e.Section != "level property" || !isSkillTemplateToken(e.TokenType) {
			continue
		}
		if resolve != nil {
			e.Value = resolve(e.Value)
		}
		e.Value = normalizeSkillTemplate(e.Value)
	}
}

func collectSkillTokensByScope(view *pvf.ScriptView) skillTokensByScope {
	scopes := skillTokensByScope{
		root: *newSkillSectionGroups(),
		modes: map[string]*skillSectionGroups{
			"dungeon": newSkillSectionGroups(),
			"pvp":     newSkillSectionGroups(),
		},
		hasExplicit: map[string]bool{},
	}

	for _, e := range view.Elements {
		if e.Kind == pvf.ScriptElementSection {
			if e.Section == "dungeon" || e.Section == "pvp" {
				scopes.hasExplicit[e.Section] = true
			}
			continue
		}
		if e.Kind != pvf.ScriptElementToken {
			continue
		}
		if e.Section == "name" && scopes.name == "" {
			scopes.name = e.Value
		}

		targetMode := ""
		for _, parent := range e.SectionPath {
			if parent == "dungeon" || parent == "pvp" {
				targetMode = parent
				break
			}
		}

		var target *skillSectionGroups
		if targetMode != "" {
			target = scopes.modes[targetMode]
		} else if len(e.SectionPath) == 1 {
			target = &scopes.root
		} else {
			continue
		}

		switch e.Section {
		case "static data":
			appendSkillTokenGroup(&target.static, &target.lastStaticID, e)
		case "level info":
			appendSkillTokenGroup(&target.levelInfo, &target.lastLevelID, e)
		case "level property":
			appendSkillTokenGroup(&target.property, &target.lastPropertyID, e)
		}
	}
	return scopes
}

func buildSkillMode(
	modeID string,
	mData *skillSectionGroups,
	rootData *skillSectionGroups,
	rootStatic []SkillNumber,
	rootStaticIssues []string,
	sharedProps []SkillProperty,
	sharedPropIssues []string,
) SkillMode {
	mode := SkillMode{
		ID:         modeID,
		Static:     []SkillNumber{},
		Levels:     [][]SkillNumber{},
		Properties: []SkillProperty{},
		Issues:     []string{},
	}

	// 1. static data: 优先使用当前模式自身定义的；若无，继承/回退根级
	if mData != nil && len(mData.static) > 0 {
		mode.Static = parseSkillStatic(mData.static, &mode.Issues)
	} else if rootData != nil && len(rootData.static) > 0 {
		mode.Static = rootStatic
		mode.Issues = append(mode.Issues, rootStaticIssues...)
	}

	// 2. level info: 优先使用当前模式自身定义的；若无且为 dungeon 模式，回退根级
	if mData != nil && len(mData.levelInfo) > 0 {
		mode.Width, mode.Levels = parseSkillLevels(mData.levelInfo, &mode.Issues)
	} else if modeID == "dungeon" && rootData != nil && len(rootData.levelInfo) > 0 {
		mode.Width, mode.Levels = parseSkillLevels(rootData.levelInfo, &mode.Issues)
	}

	// 3. level property: 优先使用当前模式自身定义的；若无，使用根级共享属性
	if mData != nil && len(mData.property) > 0 {
		for _, group := range mData.property {
			mode.Properties = append(mode.Properties, parseSkillProperties(group, &mode.Issues)...)
		}
	} else {
		mode.Properties = sharedProps
		mode.Issues = append(mode.Issues, sharedPropIssues...)
	}

	return mode
}

func parseSkillNumbers(group []pvf.ScriptElement, issues *[]string, sectionName string) []SkillNumber {
	values := make([]SkillNumber, 0, len(group))
	for _, e := range group {
		n, err := strconv.ParseFloat(e.Value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || (e.TokenType != 0 && e.TokenType != 2) {
			*issues = append(*issues, "["+sectionName+"] 包含非数值，已禁用该区域")
			return nil
		}
		values = append(values, SkillNumber{Value: n, Start: e.Start, End: e.End, TokenType: e.TokenType})
	}
	return values
}

func parseSkillStatic(groups [][]pvf.ScriptElement, issues *[]string) []SkillNumber {
	if len(groups) > 1 {
		*issues = append(*issues, "重复的 [static data]，为避免误改已禁用该区域")
		return nil
	}
	if len(groups) == 0 {
		return nil
	}
	return parseSkillNumbers(groups[0], issues, "static data")
}

func parseSkillLevels(groups [][]pvf.ScriptElement, issues *[]string) (int, [][]SkillNumber) {
	if len(groups) > 1 {
		*issues = append(*issues, "重复的 [level info]，为避免误改已禁用该区域")
		return 0, nil
	}
	if len(groups) == 0 {
		return 0, nil
	}
	values := parseSkillNumbers(groups[0], issues, "level info")
	if len(values) == 0 {
		return 0, nil
	}
	width := int(values[0].Value)
	if width <= 0 || float64(width) != values[0].Value || (len(values)-1)%width != 0 {
		*issues = append(*issues, "[level info] 列数或等级数据长度异常，已禁用动态编辑")
		return 0, nil
	}
	levels := make([][]SkillNumber, 0, (len(values)-1)/width)
	for i := 1; i < len(values); i += width {
		levels = append(levels, values[i:i+width])
	}
	return width, levels
}

func parseSkillProperties(tokens []pvf.ScriptElement, issues *[]string) []SkillProperty {
	result := []SkillProperty{}
	for len(tokens) > 0 {
		if len(tokens) < 3 || !isSkillTemplateToken(tokens[2].TokenType) {
			*issues = append(*issues, "[level property] 描述模板结构异常")
			break
		}
		p := SkillProperty{Template: tokens[2].Value, Bindings: []SkillPropertyBinding{}}
		tokens = tokens[3:]
		count := len(skillNumericPlaceholder.FindAllString(p.Template, -1))
		for i := 0; i < count; i++ {
			if len(tokens) < 3 {
				*issues = append(*issues, "[level property] 占位符与数据绑定数量不匹配")
				return append(result, p)
			}
			kind, e1 := strconv.ParseInt(tokens[0].Value, 10, 32)
			index, e2 := strconv.ParseInt(tokens[1].Value, 10, 32)
			multiplier, e3 := strconv.ParseFloat(tokens[2].Value, 64)
			if e1 != nil || e2 != nil || e3 != nil || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || index < 0 {
				*issues = append(*issues, "[level property] 数据绑定异常")
				// Compound expression bindings are outside this editor's numeric
				// model. Keep the template visible without guessing its targets.
				p.Bindings = []SkillPropertyBinding{}
				return append(result, p)
			}
			p.Bindings = append(p.Bindings, SkillPropertyBinding{Dynamic: kind < 0, Index: int(index), Multiplier: multiplier})
			tokens = tokens[3:]
		}
		result = append(result, p)
		if len(tokens) > 0 && (len(tokens) < 3 || !isSkillTemplateToken(tokens[2].TokenType)) {
			*issues = append(*issues, "[level property] 存在额外的数据绑定，未用于描述预览")
			for len(tokens) >= 3 && !isSkillTemplateToken(tokens[2].TokenType) {
				tokens = tokens[3:]
			}
			if len(tokens) < 3 {
				break
			}
		}
	}
	return result
}

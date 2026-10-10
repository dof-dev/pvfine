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

var skillFloatPlaceholder = regexp.MustCompile(`<float\d*>`)

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
	doc := parseSkill(text)
	doc.Name = resolvePreviewText(a, doc.Name)
	for mi := range doc.Modes {
		for pi := range doc.Modes[mi].Properties {
			p := &doc.Modes[mi].Properties[pi]
			p.Template = resolvePreviewDescription(a, p.Template)
		}
	}
	return doc, nil
}

func parseSkill(text string) *SkillDocument {
	view := pvf.ParseScriptView(text)
	doc := &SkillDocument{Modes: []SkillMode{}}
	shared := []SkillProperty{}
	for _, id := range []string{"dungeon", "pvp"} {
		mode := SkillMode{ID: id, Static: []SkillNumber{}, Levels: [][]SkillNumber{}, Properties: []SkillProperty{}, Issues: []string{}}
		sections := map[string][][]pvf.ScriptElement{}
		lastID := map[string]int{}
		present := false
		for _, e := range view.Elements {
			if e.Kind == pvf.ScriptElementSection && e.Section == id {
				present = true
			}
			if e.Kind != pvf.ScriptElementToken {
				continue
			}
			if e.Section == "name" && doc.Name == "" {
				doc.Name = e.Value
			}
			inMode := false
			for _, parent := range e.SectionPath {
				if parent == id {
					inMode = true
				}
			}
			if !inMode || (e.Section != "static data" && e.Section != "level info" && e.Section != "level property") {
				continue
			}
			if lastID[e.Section] != e.SectionID {
				sections[e.Section] = append(sections[e.Section], []pvf.ScriptElement{})
				lastID[e.Section] = e.SectionID
			}
			groups := sections[e.Section]
			groups[len(groups)-1] = append(groups[len(groups)-1], e)
		}
		for _, name := range []string{"static data", "level info"} {
			groups := sections[name]
			if len(groups) > 1 {
				mode.Issues = append(mode.Issues, "重复的 ["+name+"]，为避免误改已禁用该区域")
				continue
			}
			if len(groups) == 0 {
				continue
			}
			values := []SkillNumber{}
			valid := true
			for _, e := range groups[0] {
				n, err := strconv.ParseFloat(e.Value, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || (e.TokenType != 0 && e.TokenType != 2) {
					valid = false
					break
				}
				values = append(values, SkillNumber{Value: n, Start: e.Start, End: e.End, TokenType: e.TokenType})
			}
			if !valid {
				mode.Issues = append(mode.Issues, "["+name+"] 包含非数值，已禁用该区域")
				continue
			}
			if name == "static data" {
				mode.Static = values
				continue
			}
			if len(values) == 0 {
				continue
			}
			width := int(values[0].Value)
			if width <= 0 || float64(width) != values[0].Value || (len(values)-1)%width != 0 {
				mode.Issues = append(mode.Issues, "[level info] 列数或等级数据长度异常，已禁用动态编辑")
				continue
			}
			mode.Width = width
			for i := 1; i < len(values); i += width {
				mode.Levels = append(mode.Levels, values[i:i+width])
			}
		}
		for _, group := range sections["level property"] {
			mode.Properties = append(mode.Properties, parseSkillProperties(group, &mode.Issues)...)
		}
		if present {
			doc.Modes = append(doc.Modes, mode)
		}
	}
	groups := map[int][]pvf.ScriptElement{}
	order := []int{}
	for _, e := range view.Elements {
		if e.Kind != pvf.ScriptElementToken || e.Section != "level property" || len(e.SectionPath) != 1 {
			continue
		}
		if _, ok := groups[e.SectionID]; !ok {
			order = append(order, e.SectionID)
		}
		groups[e.SectionID] = append(groups[e.SectionID], e)
	}
	issues := []string{}
	for _, id := range order {
		shared = append(shared, parseSkillProperties(groups[id], &issues)...)
	}
	for i := range doc.Modes {
		if len(doc.Modes[i].Properties) == 0 {
			doc.Modes[i].Properties = shared
		}
		doc.Modes[i].Issues = append(doc.Modes[i].Issues, issues...)
	}
	return doc
}

func parseSkillProperties(tokens []pvf.ScriptElement, issues *[]string) []SkillProperty {
	result := []SkillProperty{}
	for len(tokens) > 0 {
		if len(tokens) < 3 || (tokens[2].TokenType != 6 && tokens[2].TokenType != 8) {
			*issues = append(*issues, "[level property] 描述模板结构异常")
			break
		}
		p := SkillProperty{Template: tokens[2].Value, Bindings: []SkillPropertyBinding{}}
		tokens = tokens[3:]
		count := strings.Count(p.Template, "<int>")
		// Match all supported floating point placeholders, including <float>.
		for _, match := range skillFloatPlaceholder.FindAllString(p.Template, -1) {
			if match != "" {
				count++
			}
		}
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
		if len(tokens) > 0 && (len(tokens) < 3 || (tokens[2].TokenType != 6 && tokens[2].TokenType != 8)) {
			*issues = append(*issues, "[level property] 存在额外的数据绑定，未用于描述预览")
			for len(tokens) >= 3 && tokens[2].TokenType != 6 && tokens[2].TokenType != 8 {
				tokens = tokens[3:]
			}
			if len(tokens) < 3 {
				break
			}
		}
	}
	return result
}

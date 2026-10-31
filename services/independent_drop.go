package services

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"unicode/utf16"

	"pvfine/internal/pvf"
)

const independentDropPath = "etc/independent_drop.etc"

type IndependentDropList struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	FileIndex int32  `json:"fileIndex"`
}
type IndependentDropItem struct {
	Fields []SkillNumber `json:"fields"`
	Name   string        `json:"name"`
}
type IndependentDropRow struct {
	ListTagStart  int                   `json:"listTagStart"`
	Fields        []SkillNumber         `json:"fields"`
	Items         []IndependentDropItem `json:"items"`
	MonsterName   string                `json:"monsterName"`
	ItemName      string                `json:"itemName"`
	Start         int                   `json:"start"`
	End           int                   `json:"end"`
	ListStart     int                   `json:"listStart"`
	ListEnd       int                   `json:"listEnd"`
	HasExtensions bool                  `json:"hasExtensions"`
}
type IndependentDropDocument struct {
	Version string                `json:"version"`
	Rows    []IndependentDropRow  `json:"rows"`
	Lists   []IndependentDropList `json:"lists"`
	Balance []SkillNumber         `json:"balance"`
	BodyEnd int                   `json:"bodyEnd"`
	IsList  bool                  `json:"isList"`
	Issues  []string              `json:"issues"`
}

// Positions use the editor's normalized UTF-16 coordinate system. Unknown
// sections are never reserialized; the GUI changes only selected source spans.
func parseIndependentDrop(text string, listOnly bool) (*IndependentDropDocument, error) {
	view := pvf.ParseScriptViewWithNestedSections(text, func(parent, child string) bool {
		return parent == "independent drop" && (child == "list" || child == "replace list")
	})
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	units := utf16.Encode([]rune(normalized))
	closing := func(start int, tag string) (int, error) {
		needle := utf16.Encode([]rune(tag))
		for at := start; at+len(needle) <= len(units); at++ {
			if units[at] == '#' {
				for at < len(units) && units[at] != '\n' {
					at++
				}
				continue
			}
			if units[at] == '`' {
				at++
				for at < len(units) {
					if units[at] == '`' {
						if at+1 < len(units) && units[at+1] == '`' {
							at += 2
							continue
						}
						break
					}
					at++
				}
				continue
			}

			if units[at] != '[' {
				continue
			}
			match := true
			for j, want := range needle {
				got := units[at+j]
				if got >= 'A' && got <= 'Z' {
					got += 'a' - 'A'
				}
				if got != want {
					match = false
					break
				}
			}
			if match {
				return at, nil
			}
		}
		return 0, fmt.Errorf("缺少 %s，无法安全编辑", tag)
	}

	doc := &IndependentDropDocument{Rows: []IndependentDropRow{}, Lists: []IndependentDropList{}, Balance: []SkillNumber{}, Issues: []string{}, IsList: listOnly, BodyEnd: -1}
	pending := []SkillNumber{}
	listTokens := []SkillNumber{}
	currentList := -1
	found := false
	flushList := func() error {
		if currentList < 0 {
			return nil
		}
		r := &doc.Rows[currentList]
		mode := float64(1)
		if !listOnly {
			mode = r.Fields[16].Value
		}
		if mode == 2 {
			if len(listTokens) != 1 {
				return fmt.Errorf("外部列表引用需要一个列表 ID")
			}
			r.Items = append(r.Items, IndependentDropItem{Fields: listTokens})
			return nil
		}
		if len(listTokens)%2 != 0 {
			return fmt.Errorf("[list] 的物品 ID 与权重不成对")
		}
		for i := 0; i < len(listTokens); i += 2 {
			r.Items = append(r.Items, IndependentDropItem{Fields: append([]SkillNumber(nil), listTokens[i:i+2]...)})
		}
		return nil
	}
	for _, e := range view.Elements {
		if e.Kind == pvf.ScriptElementSection {
			if e.Section == "independent drop" && len(e.SectionPath) == 1 && !listOnly {
				if found {
					return nil, fmt.Errorf("重复 [independent drop]")
				}
				found = true
				end, err := closing(e.End, "[/independent drop]")
				if err != nil {
					return nil, err
				}
				doc.BodyEnd = end
			}
			if e.Section == "list" && ((listOnly && len(e.SectionPath) == 1) || (!listOnly && len(e.SectionPath) == 2 && e.SectionPath[0] == "independent drop")) {
				if len(pending) > 0 {
					return nil, fmt.Errorf("独立掉落记录不足 17 项")
				}
				if err := flushList(); err != nil {
					return nil, err
				}
				if listOnly {
					if found {
						return nil, fmt.Errorf("重复 [list]")
					}
					found = true
					doc.Rows = append(doc.Rows, IndependentDropRow{Fields: []SkillNumber{}, Items: []IndependentDropItem{}, ListStart: -1, ListEnd: -1})
				}
				if len(doc.Rows) == 0 {
					return nil, fmt.Errorf("[list] 前没有独立掉落记录")
				}
				currentList = len(doc.Rows) - 1
				listTokens = []SkillNumber{}
				r := &doc.Rows[currentList]
				if r.ListStart >= 0 {
					return nil, fmt.Errorf("同一记录包含多个 [list]")
				}
				r.ListTagStart = e.Start
				r.ListStart = e.End
				end, err := closing(e.End, "[/list]")
				if err != nil {
					return nil, err
				}
				r.ListEnd = end
				if listOnly {
					doc.BodyEnd = end
				}
			} else if len(e.SectionPath) > 1 && e.SectionPath[0] == "independent drop" && len(doc.Rows) > 0 {
				doc.Rows[len(doc.Rows)-1].HasExtensions = true
			}
			continue
		}
		if e.Kind != pvf.ScriptElementToken {
			continue
		}
		isMain := !listOnly && len(e.SectionPath) == 1 && e.Section == "independent drop"
		isList := e.Section == "list" && ((listOnly && len(e.SectionPath) == 1) || (!listOnly && len(e.SectionPath) == 2 && e.SectionPath[0] == "independent drop"))
		isBalance := !listOnly && e.Section == "dungeon drop rate balance" && len(e.SectionPath) == 1
		if !isMain && !isList && !isBalance {
			continue
		}
		n, err := strconv.ParseFloat(e.Value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || (e.TokenType != 0 && e.TokenType != 2) {
			return nil, fmt.Errorf("[%s] 包含非数值字段", e.Section)
		}
		field := SkillNumber{Value: n, Start: e.Start, End: e.End, TokenType: e.TokenType}
		if isBalance {
			doc.Balance = append(doc.Balance, field)
			continue
		}
		if isList {
			if currentList < 0 {
				return nil, fmt.Errorf("列表结构无效")
			}
			listTokens = append(listTokens, field)
			continue
		}
		if currentList >= 0 {
			if err := flushList(); err != nil {
				return nil, err
			}
			currentList = -1
			listTokens = nil
		}
		pending = append(pending, field)
		if len(pending) == 17 {
			doc.Rows = append(doc.Rows, IndependentDropRow{Fields: pending, Items: []IndependentDropItem{}, Start: pending[0].Start, ListStart: -1, ListEnd: -1})
			pending = []SkillNumber{}
		}
	}
	if err := flushList(); err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("未找到可编辑的独立掉落 section")
	}
	if len(pending) > 0 {
		return nil, fmt.Errorf("独立掉落记录不足 17 项（余 %d 项）", len(pending))
	}
	for i := range doc.Rows {
		r := &doc.Rows[i]
		r.End = doc.BodyEnd
		if i+1 < len(doc.Rows) {
			r.End = doc.Rows[i+1].Start
		}
		if listOnly {
			continue
		}
		mode := r.Fields[16].Value
		if mode == 0 && r.ListStart >= 0 {
			return nil, fmt.Errorf("第 %d 条直接掉落记录包含列表", i+1)
		}
		if (mode == 1 || mode == 2) && r.ListStart < 0 {
			return nil, fmt.Errorf("第 %d 条记录缺少 [list]", i+1)
		}
		if mode != 0 && mode != 1 && mode != 2 {
			doc.Issues = append(doc.Issues, fmt.Sprintf("第 %d 条使用扩展模式 %g；保留原始模式与扩展条件，仅开放已确认字段", i+1, mode))
		}
	}
	return doc, nil
}

func (s *FileGUIService) ReadIndependentDrop(fileIndex int32, text string) (*IndependentDropDocument, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	if err := validateAnnotationIndex(a, fileIndex); err != nil {
		return nil, err
	}
	p := strings.ToLower(strings.ReplaceAll(a.Path(fileIndex), "\\", "/"))
	listOnly := p != independentDropPath
	if listOnly && !strings.HasPrefix(p, "etc/independentdrop/") {
		return nil, fmt.Errorf("独立掉落 GUI 不支持此路径")
	}
	if len(text) > int(s.c.editableByteLimit()) {
		return nil, fmt.Errorf("独立掉落文本过大")
	}
	doc, err := parseIndependentDrop(text, listOnly)
	if err != nil {
		return nil, err
	}
	doc.Version = a.ClientVersion()
	if i, ok := a.Find("etc/independentdrop.lst"); ok {
		lst, err := a.Text(i)
		if err != nil {
			doc.Issues = append(doc.Issues, "外部列表索引读取失败")
		} else if pairs, err := pvf.ParseListText(lst); err != nil {
			doc.Issues = append(doc.Issues, "外部列表索引解析失败")
		} else {
			for _, pair := range pairs {
				p := path.Join("etc", strings.ReplaceAll(pair.Path, "\\", "/"))
				idx, exists := a.Find(p)
				if !exists {
					idx = -1
				}
				doc.Lists = append(doc.Lists, IndependentDropList{ID: pair.ID, Path: p, FileIndex: idx})
			}
		}
	}
	// Resolve APCs through their list because not every client's semantic index
	// registers an APC relation. Cache IDs and names once per document.
	apcs := map[string]int32{}
	if index, ok := a.FindList("aicharacter/aicharacter.lst"); ok {
		if pairs, err := a.ListPairs(index); err == nil {
			for _, pair := range pairs {
				if _, index, ok := findListTargetInArchive(a, a.Path(index), pair.Path); ok {
					apcs[pair.ID] = index
				}
			}
		}
	}
	names := map[string]string{}
	resolve := func(relation string, value float64) string {
		id := strconv.FormatInt(int64(value), 10)
		key := relation + ":" + id
		if name, ok := names[key]; ok {
			return name
		}
		name := ""
		if s.c.annotationEngine != nil {
			if ref, ok := s.c.resolveAnnotationReferenceLocked(relation, id); ok {
				name = resolvePreviewText(a, ref.Name)
			}
		}
		if name == "" && relation == "APC" {
			if index, ok := apcs[id]; ok {
				if metadata, err := a.ScriptMetadata(index); err == nil && metadata.HasName {
					name = resolvePreviewText(a, markedName(metadata))
				}
			}
		}
		names[key] = name
		return name
	}
	for i := range doc.Rows {
		r := &doc.Rows[i]
		if !listOnly {
			relation := "怪物"
			if r.Fields[0].Value == 1 {
				relation = "APC"
			}
			r.MonsterName = resolve(relation, r.Fields[1].Value)
			if r.Fields[16].Value == 0 {
				r.ItemName = resolve("物品", r.Fields[2].Value)
			}
		}
		for j := range r.Items {
			if len(r.Items[j].Fields) == 2 {
				r.Items[j].Name = resolve("物品", r.Items[j].Fields[0].Value)
			}
		}
	}
	return doc, nil
}

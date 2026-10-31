package services

import (
	"fmt"
	"math"
	"pvfine/internal/pvf"
	"strconv"
)

type ForgeSection struct {
	Name   string        `json:"name"`
	Fields []SkillNumber `json:"fields"`
}
type ForgeDocument struct {
	// Rows use the shared four-field editing contract. Six-column source rows
	// retain their two additional read-only coefficients in FieldsT2.
	Rows      []UpgradeRow   `json:"rows"`
	Grades    []UpgradeRow   `json:"grades"`
	Grades100 []UpgradeRow   `json:"grades100"`
	Maximum   SkillNumber    `json:"maximum"`
	Extras    []ForgeSection `json:"extras"`
}

func (s *FileGUIService) ReadForge(fileIndex int32, text string) (*ForgeDocument, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	if err := validateAnnotationIndex(a, fileIndex); err != nil {
		return nil, err
	}
	if !sameSearchPath(a.Path(fileIndex), "etc/upgrade_separate.etc") {
		return nil, fmt.Errorf("锻造 GUI 仅支持 etc/upgrade_separate.etc")
	}
	if len(text) > int(s.c.editableByteLimit()) {
		return nil, fmt.Errorf("锻造文本过大")
	}
	doc, err := parseForge(text)
	if err != nil {
		return nil, err
	}
	names := map[int]string{}
	for _, rows := range [][]UpgradeRow{doc.Grades, doc.Grades100} {
		for i := range rows {
			id := int(rows[i].Fields[0].Value)
			name, found := names[id]
			if !found {
				if s.c.annotationEngine != nil {
					if ref, ok := s.c.resolveAnnotationReferenceLocked("物品", strconv.Itoa(id)); ok {
						name = resolvePreviewText(a, ref.Name)
					}
				}
				names[id] = name
			}
			rows[i].CostName = name
		}
	}
	return doc, nil
}

func parseForge(text string) (*ForgeDocument, error) {
	sections := map[string][]SkillNumber{}
	order := []string{}
	current := ""
	for _, e := range pvf.ParseScriptView(text).Elements {
		if e.Kind == pvf.ScriptElementSection {
			if len(e.SectionPath) != 1 {
				return nil, fmt.Errorf("锻造配置不支持嵌套 section")
			}
			if _, ok := sections[e.Section]; ok {
				return nil, fmt.Errorf("重复的 [%s]", e.Section)
			}
			current = e.Section
			sections[current] = []SkillNumber{}
			order = append(order, current)
		}
		if e.Kind != pvf.ScriptElementToken {
			continue
		}
		if e.Section != current || current == "" {
			return nil, fmt.Errorf("存在 section 以外的数据")
		}
		n, err := strconv.ParseFloat(e.Value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || (e.TokenType != 0 && e.TokenType != 2) || n < 0 || (e.TokenType == 0 && n > math.MaxInt32) {
			return nil, fmt.Errorf("[%s] 包含无效数值", current)
		}
		sections[current] = append(sections[current], SkillNumber{Value: n, Start: e.Start, End: e.End, TokenType: e.TokenType})
	}
	values := sections["table"]
	maximum := sections["separate upgrade max"]
	if len(maximum) != 1 || maximum[0].TokenType != 0 || maximum[0].Value < 1 {
		return nil, fmt.Errorf("最大锻造等级必须为正整数")
	}
	width := 0
	for _, candidate := range []int{4, 6} {
		if len(values) == 0 || len(values)%candidate != 0 || maximum[0].Value > float64(len(values)/candidate) {
			continue
		}
		valid := true
		for i := candidate - 2; i < len(values); i += candidate {
			if values[i].TokenType != 0 || values[i].Value > 10000 {
				valid = false
				break
			}
		}
		if valid {
			if width != 0 {
				return nil, fmt.Errorf("[table] 的 4 列和 6 列布局存在歧义，请在 DSL 中编辑")
			}
			width = candidate
		}
	}
	if width == 0 {
		return nil, fmt.Errorf("[table] 每级需要 4 或 6 项，成功率必须是 0～10000 的整数，最大等级不能超过数据行数")
	}
	doc := &ForgeDocument{Rows: []UpgradeRow{}, Grades: []UpgradeRow{}, Grades100: []UpgradeRow{}, Maximum: maximum[0], Extras: []ForgeSection{}}
	for i := 0; i < len(values); i += width {
		fields := append([]SkillNumber(nil), values[i:i+2]...)
		fields = append(fields, values[i+width-2:i+width]...)
		extra := []SkillNumber{}
		if width == 6 {
			extra = append(extra, values[i+2:i+4]...)
		}
		doc.Rows = append(doc.Rows, UpgradeRow{Level: i/width + 1, Fields: fields, FieldsT2: extra})
	}
	for _, name := range []string{"item weights by grade", "item weights by grade 100lv"} {
		fields := sections[name]
		if (name == "item weights by grade" && len(fields) == 0) || len(fields)%3 != 0 {
			return nil, fmt.Errorf("[%s] 每条需要 3 项", name)
		}
		rows := []UpgradeRow{}
		previous := 0
		for i := 0; i < len(fields); i += 3 {
			for _, field := range fields[i : i+3] {
				if field.TokenType != 0 {
					return nil, fmt.Errorf("[%s] 字段必须为整数", name)
				}
			}
			level := int(fields[i].Value)
			if level <= previous {
				return nil, fmt.Errorf("[%s] 装备等级必须递增且大于 0", name)
			}
			previous = level
			rows = append(rows, UpgradeRow{Level: level, Fields: append([]SkillNumber(nil), fields[i+1:i+3]...), FieldsT2: []SkillNumber{}})
		}
		if name == "item weights by grade" {
			doc.Grades = rows
		} else {
			doc.Grades100 = rows
		}
	}
	for _, name := range order {
		if name != "table" && name != "separate upgrade max" && name != "item weights by grade" && name != "item weights by grade 100lv" {
			doc.Extras = append(doc.Extras, ForgeSection{Name: name, Fields: sections[name]})
		}
	}
	return doc, nil
}

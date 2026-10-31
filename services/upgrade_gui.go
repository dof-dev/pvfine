package services

import (
	"fmt"
	"math"
	"strconv"

	"pvfine/internal/pvf"
)

// UpgradeRow exposes only the five editable columns. Level follows the source
// tool's row order. Fields holds weapon settings and shared materials;
// FieldsT2 holds armor probability, failure operation, and downgrade parameter.
// All other table parameters retain their source text.
type UpgradeRow struct {
	Level    int           `json:"level"`
	Fields   []SkillNumber `json:"fields"`
	FieldsT2 []SkillNumber `json:"fieldsT2"`
	CostName string        `json:"costName"`
}

type UpgradeDocument struct {
	Rows      []UpgradeRow     `json:"rows"`
	SafeRows  []SafeUpgradeRow `json:"safeRows"`
	SafeIssue string           `json:"safeIssue"`
}

// SafeUpgradeRow preserves eight source fields. Column 2 is failure probability
// with denominator 100000; 3 is correction, 5/6 are material ID/count, 7 is gold.
// Columns 1/4 remain unidentified and are preserved by GUI edits.
type SafeUpgradeRow struct {
	Fields   []SkillNumber `json:"fields"`
	CostName string        `json:"costName"`
}

func isUpgradePath(path string) bool {
	return sameSearchPath(path, "etc/upgrade.etc") || sameSearchPath(path, "etc/amplifyupgrade.etc")
}

func (s *FileGUIService) ReadUpgrade(fileIndex int32, text string) (*UpgradeDocument, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	if err := validateAnnotationIndex(a, fileIndex); err != nil {
		return nil, err
	}
	if !isUpgradePath(a.Path(fileIndex)) {
		return nil, fmt.Errorf("强化/增幅 GUI 仅支持 etc/upgrade.etc 和 etc/amplifyupgrade.etc")
	}
	if len(text) > int(s.c.editableByteLimit()) {
		return nil, fmt.Errorf("强化/增幅文本过大")
	}
	tableType := "normal"
	if sameSearchPath(a.Path(fileIndex), "etc/amplifyupgrade.etc") {
		tableType = "amplify"
	}
	doc, err := parseUpgrade(text, tableType)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string)
	resolveCostName := func(value float64) string {
		id := strconv.FormatInt(int64(value), 10)
		if name, ok := names[id]; ok {
			return name
		}
		name := ""
		if s.c.annotationEngine != nil {
			if ref, ok := s.c.resolveAnnotationReferenceLocked("物品", id); ok {
				name = resolvePreviewText(a, ref.Name)
			}
		}
		names[id] = name
		return name
	}
	for i := range doc.Rows {
		row := &doc.Rows[i]
		row.CostName = resolveCostName(row.Fields[3].Value)
	}
	for i := range doc.SafeRows {
		row := &doc.SafeRows[i]
		row.CostName = resolveCostName(row.Fields[5].Value)
	}
	return doc, nil
}

func upgradeTableTokens(text, tableType string) ([]pvf.ScriptElement, bool, error) {
	return pvf.SelectUpgradeTableTokens(text, tableType)
}

func parseUpgrade(text, tableType string) (*UpgradeDocument, error) {
	tokens, typed, err := upgradeTableTokens(text, tableType)
	if err != nil {
		return nil, err
	}
	layout, err := pvf.DetectUpgradeTableLayout(tokens, typed)
	if err != nil {
		return nil, err
	}
	values := []SkillNumber{}
	for _, e := range tokens {
		n, err := strconv.ParseFloat(e.Value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || (e.TokenType != 0 && e.TokenType != 2) {
			return nil, fmt.Errorf("[table] 包含非数值数据")
		}
		values = append(values, SkillNumber{Value: n, Start: e.Start, End: e.End, TokenType: e.TokenType})
	}
	width := layout.Width
	columns := layout.Columns
	doc := &UpgradeDocument{Rows: []UpgradeRow{}}
	doc.SafeRows, err = parseSafeUpgrade(text)
	if err != nil {
		doc.SafeIssue = err.Error()
	}
	limit := len(values) / width
	// The legacy editor exposes 31 rows. Typed tables expose every level.
	if !typed && limit > 31 {
		limit = 31
	}
	for level := 0; level < limit; level++ {
		offset := level * width
		fields := make([]SkillNumber, 0, 5)
		for _, column := range columns {
			fields = append(fields, values[offset+column])
		}
		row := UpgradeRow{Level: level + 1, Fields: fields, FieldsT2: []SkillNumber{}}
		if layout.HasT2 {
			for _, column := range layout.ColumnsT2 {
				row.FieldsT2 = append(row.FieldsT2, values[offset+column])
			}
		}
		allFields := append(append([]SkillNumber(nil), row.Fields...), row.FieldsT2...)
		for _, field := range allFields {
			if field.TokenType != 0 || field.Value < 0 || field.Value > math.MaxInt32 {
				return nil, fmt.Errorf("第 %d 级：可编辑字段必须是非负 32 位整数", level+1)
			}
		}
		if fields[0].Value > 100000 || fields[1].Value < 1 || fields[1].Value > 3 || (!layout.HasT2 && fields[2].Value > 31) {
			return nil, fmt.Errorf("第 %d 级：失败概率、失败操作或降级参数超出范围", level+1)
		}
		if layout.HasT2 && (row.FieldsT2[0].Value > 100000 || row.FieldsT2[1].Value < 1 || row.FieldsT2[1].Value > 3) {
			return nil, fmt.Errorf("第 %d 级：防具失败概率或失败操作超出范围", level+1)
		}
		doc.Rows = append(doc.Rows, row)
	}
	return doc, nil
}

func parseSafeUpgrade(text string) ([]SafeUpgradeRow, error) {
	rows := []SafeUpgradeRow{}
	values := []SkillNumber{}
	sections := 0
	for _, e := range pvf.ParseScriptView(text).Elements {
		if e.Kind == pvf.ScriptElementSection && len(e.SectionPath) > 1 && e.SectionPath[0] == "safe upgrade" {
			return rows, fmt.Errorf("[safe upgrade] 包含未知子 section")
		}
		if e.Kind == pvf.ScriptElementSection && e.Section == "safe upgrade" {
			sections++
			if len(e.SectionPath) != 1 {
				return rows, fmt.Errorf("[safe upgrade] 必须位于顶层")
			}
		}
		if e.Kind != pvf.ScriptElementToken || e.Section != "safe upgrade" {
			continue
		}
		n, err := strconv.ParseInt(e.Value, 10, 32)
		if err != nil || e.TokenType != 0 {
			return rows, fmt.Errorf("[safe upgrade] 字段必须是 32 位整数")
		}
		values = append(values, SkillNumber{Value: float64(n), Start: e.Start, End: e.End, TokenType: e.TokenType})
	}
	if sections == 0 {
		return rows, nil
	}
	if sections != 1 {
		return rows, fmt.Errorf("存在多个 [safe upgrade]，无法确定数据结构")
	}
	if len(values) == 0 || len(values)%8 != 0 {
		return rows, fmt.Errorf("[safe upgrade] 每条需要 8 项，当前共 %d 项", len(values))
	}
	for offset := 0; offset < len(values); offset += 8 {
		if values[offset+2].Value < 0 || values[offset+2].Value > 100000 {
			return []SafeUpgradeRow{}, fmt.Errorf("[safe upgrade] 第 %d 条失败率必须位于 0～100000", offset/8+1)
		}
		rows = append(rows, SafeUpgradeRow{Fields: append([]SkillNumber(nil), values[offset:offset+8]...)})
	}
	return rows, nil
}

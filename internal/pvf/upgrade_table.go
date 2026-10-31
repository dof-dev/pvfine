package pvf

import (
	"fmt"
	"strconv"
)

// upgradeTableTokens selects by each table's own type, including values that
// remain under the unpaired [table type] child until [/table]. Untyped legacy
// files are accepted only when exactly one table is present.
func SelectUpgradeTableTokens(text, tableType string) ([]ScriptElement, bool, error) {
	view := ParseScriptView(text)
	type table struct {
		kind         string
		typeSections int
		awaitingType bool
		values       []ScriptElement
	}
	tables := []*table{}
	var current *table
	for _, e := range view.Elements {
		if e.Kind == ScriptElementSection && e.Section == "table" {
			if len(e.SectionPath) != 1 {
				return nil, false, fmt.Errorf("[table] 必须位于顶层")
			}
			current = &table{}
			tables = append(tables, current)
			continue
		}
		inside := len(e.SectionPath) > 0 && e.SectionPath[0] == "table"
		if current == nil || !inside {
			continue
		}
		if e.Kind == ScriptElementSection && e.Section == "table type" {
			current.typeSections++
			current.awaitingType = true
			continue
		}
		if e.Kind != ScriptElementToken {
			continue
		}
		if current.awaitingType {
			current.kind = e.Value
			current.awaitingType = false
			continue
		}
		current.values = append(current.values, e)
	}
	var selected *table
	for _, candidate := range tables {
		if candidate.kind != tableType {
			continue
		}
		if candidate.typeSections != 1 {
			return nil, false, fmt.Errorf("%s 表存在重复的 [table type]", tableType)
		}
		if selected != nil {
			return nil, false, fmt.Errorf("存在多个 table type = %s 的表，无法确定编辑目标", tableType)
		}
		selected = candidate
	}
	if selected != nil {
		return selected.values, true, nil
	}
	if len(tables) == 1 && tables[0].typeSections == 0 {
		return tables[0].values, false, nil
	}
	return nil, false, fmt.Errorf("未找到 table type = %s 的表", tableType)
}

// UpgradeTableLayout defines column positions for upgrade/amplify tables.
// Columns holds [failureRate, failureOp, downgrade, itemID, itemCount].
// ColumnsT2 holds [armorFailureRate, armorFailureOp, armorDowngrade] when HasT2 is true.
type UpgradeTableLayout struct {
	Width       int
	Columns     []int
	ColumnsT2   []int
	RateColumn  int
	CountColumn int
	HasT2       bool
}

var (
	UpgradeLayout17 = UpgradeTableLayout{
		Width:       17,
		Columns:     []int{6, 7, 8, 9, 10},
		ColumnsT2:   nil,
		RateColumn:  6,
		CountColumn: 10,
		HasT2:       false,
	}
	UpgradeLayout19 = UpgradeTableLayout{
		Width:       19,
		Columns:     []int{7, 8, 9, 10, 11},
		ColumnsT2:   nil,
		RateColumn:  7,
		CountColumn: 11,
		HasT2:       false,
	}
	UpgradeLayout24 = UpgradeTableLayout{
		Width:       24,
		Columns:     []int{8, 10, 11, 14, 15},
		ColumnsT2:   []int{9, 12, 13},
		RateColumn:  8,
		CountColumn: 15,
		HasT2:       true,
	}
)

// DetectUpgradeTableLayout determines table geometry based on element count,
// typed metadata, and first-row semantic validation.
func DetectUpgradeTableLayout(tokens []ScriptElement, typed bool) (*UpgradeTableLayout, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("[table] 表格数据为空")
	}
	var candidates []UpgradeTableLayout
	if typed {
		candidates = []UpgradeTableLayout{UpgradeLayout24, UpgradeLayout19}
	} else {
		candidates = []UpgradeTableLayout{UpgradeLayout17}
	}

	for _, cand := range candidates {
		if len(tokens)%cand.Width != 0 {
			continue
		}
		if validateUpgradeLayoutFirstRow(tokens, cand) {
			return &cand, nil
		}
	}

	// Fallback: if only one candidate divides cleanly, accept it
	var divisible []UpgradeTableLayout
	for _, cand := range candidates {
		if len(tokens)%cand.Width == 0 {
			divisible = append(divisible, cand)
		}
	}
	if len(divisible) == 1 {
		return &divisible[0], nil
	}

	if typed {
		return nil, fmt.Errorf("[table] 每级需要 19 或 24 个数值，当前共 %d 项", len(tokens))
	}
	return nil, fmt.Errorf("[table] 每级需要 17 个数值，当前共 %d 项", len(tokens))
}

func validateUpgradeLayoutFirstRow(tokens []ScriptElement, layout UpgradeTableLayout) bool {
	if len(tokens) < layout.Width {
		return false
	}
	op, ok := parseUpgradeElementInt(tokens[layout.Columns[1]])
	if !ok || op < 1 || op > 3 {
		return false
	}
	rate, ok := parseUpgradeElementInt(tokens[layout.Columns[0]])
	if !ok || rate < 0 || rate > 100000 {
		return false
	}
	matID, ok := parseUpgradeElementInt(tokens[layout.Columns[3]])
	if !ok || matID <= 0 {
		return false
	}
	if layout.HasT2 {
		armorOp, ok := parseUpgradeElementInt(tokens[layout.ColumnsT2[1]])
		if !ok || armorOp < 1 || armorOp > 3 {
			return false
		}
		armorRate, ok := parseUpgradeElementInt(tokens[layout.ColumnsT2[0]])
		if !ok || armorRate < 0 || armorRate > 100000 {
			return false
		}
	}
	return true
}

func parseUpgradeElementInt(e ScriptElement) (int64, bool) {
	if e.TokenType != 0 && e.TokenType != 2 {
		return 0, false
	}
	if n, err := strconv.ParseInt(e.Value, 10, 64); err == nil {
		return n, true
	}
	if f, err := strconv.ParseFloat(e.Value, 64); err == nil {
		return int64(f), true
	}
	return 0, false
}

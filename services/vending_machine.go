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

const vendingMachineListPath = "etc/vendingmachine.lst"

type VendingReward struct {
	Fields []SkillNumber `json:"fields"`
	Name   string        `json:"name"`
}
type VendingGroup struct {
	Start        int             `json:"start"`
	End          int             `json:"end"`
	Number       SkillNumber     `json:"number"`
	Material     []SkillNumber   `json:"material"`
	MaterialName string          `json:"materialName"`
	Rewards      []VendingReward `json:"rewards"`
	OutputClose  int             `json:"outputClose"`
}
type VendingDocument struct {
	Groups []VendingGroup `json:"groups"`
}

// ListVendingMachines uses the registered LST, including relative and archive-root paths.
func (s *FileGUIService) ListVendingMachines() ([]SearchHit, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	index, ok := a.Find(vendingMachineListPath)
	if !ok {
		return nil, fmt.Errorf("找不到 %s", vendingMachineListPath)
	}
	pairs, err := a.ListPairs(index)
	if err != nil {
		return nil, err
	}
	result := []SearchHit{}
	for _, pair := range pairs {
		p, i, found := findListTargetInArchive(a, vendingMachineListPath, pair.Path)
		if !found || !strings.EqualFold(path.Ext(p), ".vm") {
			continue
		}
		name := path.Base(p)
		if meta, err := a.ScriptMetadata(i); err == nil && meta.HasName {
			name = resolvePreviewText(a, meta.Name)
		}
		result = append(result, SearchHit{FileIndex: i, Path: p, ID: pair.ID, Name: name, Category: "抽奖机", Rarity: pvf.RarityUnknown})
	}
	return result, nil
}

func (s *FileGUIService) ReadVendingMachine(fileIndex int32, text string) (*VendingDocument, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	if err := validateAnnotationIndex(a, fileIndex); err != nil {
		return nil, err
	}
	if !strings.EqualFold(path.Ext(a.Path(fileIndex)), ".vm") {
		return nil, fmt.Errorf("抽奖机 GUI 仅支持 .vm 文件")
	}
	if len(text) > int(s.c.editableByteLimit()) {
		return nil, fmt.Errorf("抽奖机文本过大")
	}
	doc, err := parseVendingMachine(text)
	if err != nil {
		return nil, err
	}
	names := map[int]string{}
	nameOf := func(value float64) string {
		id := int(value)
		if name, ok := names[id]; ok {
			return name
		}
		name := ""
		if s.c.annotationEngine != nil {
			if ref, ok := s.c.resolveAnnotationReferenceLocked("物品", strconv.Itoa(id)); ok {
				name = resolvePreviewText(a, ref.Name)
			}
		}
		names[id] = name
		return name
	}
	for i := range doc.Groups {
		g := &doc.Groups[i]
		g.MaterialName = nameOf(g.Material[0].Value)
		for j := range g.Rewards {
			g.Rewards[j].Name = nameOf(g.Rewards[j].Fields[0].Value)
		}
	}
	return doc, nil
}

// Source spans follow ParseScriptView's normalized UTF-16 offsets. Unknown sections
// remain in the draft; only explicitly selected spans are edited by the GUI.
func parseVendingMachine(text string) (*VendingDocument, error) {
	view := pvf.ParseScriptView(text)
	groupCloses := pvf.ScriptSectionClosingOffsets(text, "item group")
	outputCloses := pvf.ScriptSectionClosingOffsets(text, "output")
	units := utf16.Encode([]rune(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)))
	endTag := func(start int) int {
		for i := start; i < len(units); i++ {
			if units[i] == ']' {
				return i + 1
			}
		}
		return len(units)
	}
	doc := &VendingDocument{Groups: []VendingGroup{}}
	var group *VendingGroup
	var fields map[string][]SkillNumber
	seenIDs := map[int]bool{}
	finish := func() error {
		if group == nil {
			return nil
		}
		if len(fields["group num"]) != 1 || len(fields["material"]) != 2 || len(fields["output"])%4 != 0 || group.OutputClose < 0 {
			return fmt.Errorf("每个 [item group] 需要一个组编号、两个材料字段和已闭合的 [output]（每条 4 个整数）")
		}
		group.Number = fields["group num"][0]
		id := int(group.Number.Value)
		if seenIDs[id] {
			return fmt.Errorf("组编号 %d 重复", id)
		}
		seenIDs[id] = true
		group.Material = fields["material"]
		for i := 0; i < len(fields["output"]); i += 4 {
			row := fields["output"][i : i+4]
			if row[3].Value != 0 && row[3].Value != 1 {
				return fmt.Errorf("是否公告必须为 0 或 1")
			}
			group.Rewards = append(group.Rewards, VendingReward{Fields: row})
		}
		return nil
	}
	for _, e := range view.Elements {
		if e.Kind == pvf.ScriptElementSection && e.Section == "item group" {
			if len(e.SectionPath) != 1 {
				return nil, fmt.Errorf("[item group] 不能嵌套")
			}
			if err := finish(); err != nil {
				return nil, err
			}
			close, ok := groupCloses[e.Start]
			if !ok {
				return nil, fmt.Errorf("缺少 [/item group]，请先在文本模式修复")
			}
			doc.Groups = append(doc.Groups, VendingGroup{Start: e.Start, End: endTag(close), Rewards: []VendingReward{}, OutputClose: -1})
			group = &doc.Groups[len(doc.Groups)-1]
			fields = map[string][]SkillNumber{}
			continue
		}
		if e.Section != "group num" && e.Section != "material" && e.Section != "output" {
			continue
		}
		if group == nil || len(e.SectionPath) != 2 || e.SectionPath[0] != "item group" || e.Start >= group.End {
			return nil, fmt.Errorf("[%s] 必须直接位于 [item group] 内", e.Section)
		}
		if e.Kind == pvf.ScriptElementSection {
			if _, ok := fields[e.Section]; ok {
				return nil, fmt.Errorf("同一组内 [%s] 重复", e.Section)
			}
			fields[e.Section] = []SkillNumber{}
			if e.Section == "output" {
				close, ok := outputCloses[e.Start]
				if !ok || close >= group.End {
					return nil, fmt.Errorf("缺少 [/output]")
				}
				group.OutputClose = close
			}
			continue
		}
		if e.TokenType != 0 {
			return nil, fmt.Errorf("[%s] 必须使用非负整数", e.Section)
		}
		n, err := strconv.ParseInt(e.Value, 10, 32)
		if err != nil || n < 0 || n > math.MaxInt32 {
			return nil, fmt.Errorf("[%s] 包含无效整数", e.Section)
		}
		fields[e.Section] = append(fields[e.Section], SkillNumber{Value: float64(n), Start: e.Start, End: e.End, TokenType: 0})
	}
	if err := finish(); err != nil {
		return nil, err
	}
	return doc, nil
}

package services

import (
	"fmt"
	"strconv"
	"strings"

	"pvfine/internal/pvf"
)

const equipmentPartSetListPath = "etc/equipmentpartset.etc"

// readEquipmentSetPreviewLocked uses the editor text for the current equipment
// and the archive's in-memory edits for files referenced by the set list.
func (s *PreviewService) readEquipmentSetPreviewLocked(equipmentText string, issues *[]PreviewIssue) *EquipmentSetPreviewDocument {
	setID, start := equipmentPartSetID(equipmentText)
	if setID == "" {
		return nil
	}
	a := s.c.archive
	warn := func(message string) {
		addPreviewIssue(issues, equipmentText, start, "warning", "part set index", message)
	}
	listIndex, ok := a.Find(equipmentPartSetListPath)
	if !ok {
		warn("找不到套装列表: " + equipmentPartSetListPath)
		return nil
	}
	listText, err := s.previewRelatedTextLocked(listIndex)
	if err != nil {
		warn("读取套装列表失败: " + err.Error())
		return nil
	}
	values, ok := findEquipmentPartSet(listText, setID)
	if !ok {
		warn("套装列表中找不到 ID: " + setID)
		return nil
	}
	if len(values) < 7 || (len(values)-3)%4 != 0 {
		warn("套装记录缺少名称或完整的部位数据: " + setID)
		return nil
	}
	name := resolvePreviewText(a, values[2])
	parts := make([]string, 0, (len(values)-3)/4)
	for i := 3; i < len(values); i += 4 {
		parts = append(parts, resolvePreviewText(a, values[i]))
	}
	if name == "" {
		warn("套装名称为空: " + setID)
		return nil
	}
	for _, part := range parts {
		if part == "" {
			warn("套装部位名称为空: " + setID)
			return nil
		}
	}
	// Paths in equipmentpartset.etc are relative to equipment/, not etc/.
	effectPath, valid := resolveListPath("equipment/equipment.lst", values[1])
	if !valid {
		warn(fmt.Sprintf("套装效果文件路径无效: %s", values[1]))
		return nil
	}
	effectIndex, ok := a.Find(effectPath)
	if !ok {
		warn(fmt.Sprintf("找不到套装效果文件: %s", values[1]))
		return nil
	}
	effectText, err := s.previewRelatedTextLocked(effectIndex)
	if err != nil {
		warn("读取套装效果文件失败: " + err.Error())
		return nil
	}
	abilities, ok := parseEquipmentSetAbilities(a, effectText)
	if !ok {
		warn("套装效果文件缺少完整的件数或基础效果描述: " + values[1])
		return nil
	}
	return &EquipmentSetPreviewDocument{Name: name, Parts: parts, Abilities: abilities}
}

func (s *PreviewService) previewRelatedTextLocked(index int32) (string, error) {
	if text, ok := s.c.editorText[index]; ok {
		return text, nil
	}
	return s.c.archive.Text(index)
}

func equipmentPartSetID(text string) (string, int) {
	for _, element := range pvf.ParseScriptView(text).Elements {
		if element.Kind == pvf.ScriptElementToken && element.Index == 0 &&
			len(element.SectionPath) == 1 && strings.EqualFold(element.Section, "part set index") {
			return strings.TrimSpace(element.Value), element.Start
		}
	}
	return "", 0
}

func findEquipmentPartSet(text, setID string) ([]string, bool) {
	view := pvf.ParseScriptView(text)
	var values []string
	flush := func() ([]string, bool) {
		if len(values) > 0 && strings.TrimSpace(values[0]) == setID {
			return values, true
		}
		return nil, false
	}
	sectionID := 0
	for _, element := range view.Elements {
		if element.Kind == pvf.ScriptElementSection && len(element.SectionPath) == 1 &&
			strings.EqualFold(element.Section, "equipment part set") {
			if match, ok := flush(); ok {
				return match, true
			}
			sectionID = element.SectionID
			values = nil
			continue
		}
		if element.Kind == pvf.ScriptElementToken && element.SectionID == sectionID && sectionID != 0 {
			values = append(values, element.Value)
		}
	}
	return flush()
}

func parseEquipmentSetAbilities(a *pvf.Archive, text string) ([]EquipmentSetAbility, bool) {
	type abilityFields struct {
		count  []string
		basic  []string
		detail []string
	}
	view := pvf.ParseScriptView(text)
	fields := make([]abilityFields, 0)
	sectionKinds := make(map[int]string)
	current := -1
	for _, element := range view.Elements {
		if element.Kind == pvf.ScriptElementSection {
			if len(element.SectionPath) == 1 && strings.EqualFold(element.Section, "piece set ability") {
				fields = append(fields, abilityFields{})
				current = len(fields) - 1
				sectionKinds[element.SectionID] = "count"
			} else if current >= 0 && len(element.SectionPath) == 2 &&
				strings.EqualFold(element.SectionPath[0], "piece set ability") {
				switch {
				case strings.EqualFold(element.Section, "parameter basic explain"):
					sectionKinds[element.SectionID] = "basic"
				case strings.EqualFold(element.Section, "parameter detail explain"):
					sectionKinds[element.SectionID] = "detail"
				}
			}
			continue
		}
		if element.Kind != pvf.ScriptElementToken || current < 0 {
			continue
		}
		switch sectionKinds[element.SectionID] {
		case "count":
			fields[current].count = append(fields[current].count, element.Value)
		case "basic":
			fields[current].basic = append(fields[current].basic, element.Value)
		case "detail":
			fields[current].detail = append(fields[current].detail, element.Value)
		}
	}
	if len(fields) == 0 {
		return nil, false
	}
	abilities := make([]EquipmentSetAbility, 0, len(fields))
	for _, field := range fields {
		if len(field.count) != 1 {
			return nil, false
		}
		pieces, err := strconv.ParseInt(strings.TrimSpace(field.count[0]), 10, 32)
		if err != nil || pieces <= 0 {
			return nil, false
		}
		base := normalizeExplain(resolvePreviewText(a, joinFieldValues(field.basic)))
		if base == "" {
			return nil, false
		}
		abilities = append(abilities, EquipmentSetAbility{
			Pieces:        int32(pieces),
			BaseExplain:   base,
			DetailExplain: normalizeExplain(resolvePreviewText(a, joinFieldValues(field.detail))),
		})
	}
	return abilities, true
}

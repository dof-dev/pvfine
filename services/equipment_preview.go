package services

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	annotationrules "pvfine/internal/annotations"
	"pvfine/internal/pvf"
)

// EquipmentPreviewAttribute is one visible line in an equipment tooltip.
type EquipmentPreviewAttribute struct {
	Label    string `json:"label"`
	Value    string `json:"value"`
	Negative bool   `json:"negative"`
}

// EquipmentSkillLevelup is one profession-aware skill level bonus.
type EquipmentSkillLevelup struct {
	Job   string `json:"job"`
	Skill string `json:"skill"`
	Level int32  `json:"level"`
}

// EquipmentPreviewDocument is the game-style data model rendered by the
// frontend. Optional sections are represented by empty strings/slices.
type EquipmentPreviewDocument struct {
	Icon             *ImageReference             `json:"icon"`
	Name             string                      `json:"name"`
	Name2            string                      `json:"name2"`
	Rarity           int32                       `json:"rarity"`
	RarityLabel      string                      `json:"rarityLabel"`
	QualityText      string                      `json:"qualityText"`
	EquipmentType    string                      `json:"equipmentType"`
	ItemGroupName    string                      `json:"itemGroupName"`
	AttachType       string                      `json:"attachType"`
	MinimumLevelText string                      `json:"minimumLevelText"`
	UsableJobs       []string                    `json:"usableJobs"`
	BaseAttributes   []EquipmentPreviewAttribute `json:"baseAttributes"`
	FourDimensions   []EquipmentPreviewAttribute `json:"fourDimensions"`
	OtherAttributes  []EquipmentPreviewAttribute `json:"otherAttributes"`
	SkillLevelups    []EquipmentSkillLevelup     `json:"skillLevelups"`
	BaseExplain      string                      `json:"baseExplain"`
	DetailExplain    string                      `json:"detailExplain"`
	FlavorText       string                      `json:"flavorText"`
	DurabilityText   string                      `json:"durabilityText"`
	WeightText       string                      `json:"weightText"`
	PriceText        string                      `json:"priceText"`
	Issues           []PreviewIssue              `json:"issues"`
}

// ParseEQU parses the current editor text. Archive state is only used for the
// file path and relation lookups; text itself always comes from the caller so
// unsaved changes are reflected immediately.
func (s *PreviewService) ParseEQU(fileIndex int32, text string) (*EquipmentPreviewDocument, error) {
	if s == nil || s.c == nil {
		engine, err := annotationrules.LoadDefault()
		if err != nil {
			return nil, err
		}
		return buildEquipmentPreview("preview.equ", text, engine, nil), nil
	}

	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.archive == nil {
		engine, err := annotationrules.LoadDefault()
		if err != nil {
			return nil, err
		}
		return buildEquipmentPreview("preview.equ", text, engine, nil), nil
	}
	if err := validateAnnotationIndex(s.c.archive, fileIndex); err != nil {
		return nil, err
	}
	if s.c.annotationEngine == nil {
		if s.c.annotationErr != nil {
			return nil, s.c.annotationErr
		}
		return nil, fmt.Errorf("标注引擎未初始化")
	}
	return buildEquipmentPreview(
		s.c.archive.Path(fileIndex), text, s.c.annotationEngine,
		s.c.resolveAnnotationReferenceContextLocked,
	), nil
}

func buildEquipmentPreview(filePath, text string, engine *annotationrules.Engine, resolver annotationrules.ContextResolver) *EquipmentPreviewDocument {
	document := &EquipmentPreviewDocument{
		QualityText:     "最上级(100%)",
		UsableJobs:      make([]string, 0),
		BaseAttributes:  make([]EquipmentPreviewAttribute, 0),
		FourDimensions:  make([]EquipmentPreviewAttribute, 0),
		OtherAttributes: make([]EquipmentPreviewAttribute, 0),
		SkillLevelups:   make([]EquipmentSkillLevelup, 0),
		Issues:          make([]PreviewIssue, 0),
	}
	view := pvf.ParseScriptView(text)
	occurrences := engine.ExtractPreviewFields(filePath, view, "equ")

	roleValues := make(map[string][]annotationrules.PreviewFieldValue)
	for _, occurrence := range occurrences {
		role := ""
		if occurrence.Field.Preview != nil {
			role = occurrence.Field.Preview.Role
		}
		if role != "" {
			roleValues[role] = append(roleValues[role], occurrence)
		}
	}
	first := func(role string) (annotationrules.PreviewFieldValue, bool) {
		items := roleValues[role]
		if len(items) == 0 {
			return annotationrules.PreviewFieldValue{}, false
		}
		return items[0], true
	}

	if value, ok := first("name"); ok {
		document.Name = joinFieldValues(value.Values)
	}
	if value, ok := first("name2"); ok {
		document.Name2 = joinFieldValues(value.Values)
	}
	if value, ok := first("icon"); ok {
		readEquipmentIcon(document, value, text, &document.Issues)
	}
	if value, ok := first("rarity"); ok {
		raw := firstValue(value.Values)
		if parsed, ok := strconv.ParseInt(strings.TrimSpace(raw), 10, 32); ok == nil {
			document.Rarity = int32(parsed)
		} else {
			addPreviewIssue(&document.Issues, text, value.Start, "warning", "rarity", "稀有度不是有效整数: "+raw)
		}
		document.RarityLabel = enumValue(value.Field, raw, document, text, value.Start)
	}
	if value, ok := first("equipment-type"); ok {
		document.EquipmentType = enumValue(value.Field, firstValue(value.Values), document, text, value.Start)
	}
	if value, ok := first("item-group-name"); ok {
		document.ItemGroupName = enumValue(value.Field, firstValue(value.Values), document, text, value.Start)
	}
	if value, ok := first("attach-type"); ok {
		document.AttachType = enumValue(value.Field, firstValue(value.Values), document, text, value.Start)
	}
	if value, ok := first("minimum-level"); ok {
		document.MinimumLevelText = formatMinimumLevel(value, text, &document.Issues)
	}
	if values := roleValues["usable-jobs"]; len(values) > 0 {
		for _, occurrence := range values {
			for _, raw := range occurrence.Values {
				raw = strings.TrimSpace(raw)
				if raw == "" || strings.EqualFold(raw, "[all]") {
					continue
				}
				document.UsableJobs = append(document.UsableJobs, enumValue(occurrence.Field, raw, document, text, occurrence.Start))
			}
		}
	}

	for _, occurrence := range roleValues["base-attribute"] {
		if attribute, ok := parseEquipmentAttribute(occurrence, text, &document.Issues); ok {
			document.BaseAttributes = append(document.BaseAttributes, attribute)
		}
	}
	for _, occurrence := range roleValues["four-dimension"] {
		if attribute, ok := parseEquipmentAttribute(occurrence, text, &document.Issues); ok {
			document.FourDimensions = append(document.FourDimensions, attribute)
		}
	}
	for _, occurrence := range roleValues["other-attribute"] {
		if attribute, ok := parseEquipmentAttribute(occurrence, text, &document.Issues); ok {
			document.OtherAttributes = append(document.OtherAttributes, attribute)
		}
	}

	for _, occurrence := range roleValues["skill-levelup"] {
		if len(occurrence.Values) < 3 {
			addPreviewIssue(&document.Issues, text, occurrence.Start, "error", "skill levelup", "技能等级加成记录需要职业、技能 ID 和等级")
			continue
		}
		job := enumValue(occurrence.Field, occurrence.Values[0], document, text, occurrence.Start)
		skillID := strings.TrimSpace(occurrence.Values[1])
		skill := skillID
		if resolver != nil {
			if reference, ok := resolver("技能", skillID, occurrence.Context); ok {
				if strings.TrimSpace(reference.Name) != "" {
					skill = reference.Name
				}
			} else {
				addPreviewIssue(&document.Issues, text, occurrence.Start, "warning", "skill levelup", "未找到技能 ID: "+skillID)
			}
		} else if skillID != "" {
			addPreviewIssue(&document.Issues, text, occurrence.Start, "warning", "skill levelup", "未配置技能关联，显示原始 ID: "+skillID)
		}
		level, err := strconv.ParseInt(strings.TrimSpace(occurrence.Values[2]), 10, 32)
		if err != nil {
			addPreviewIssue(&document.Issues, text, occurrence.Start, "warning", "skill levelup", "技能等级不是有效整数: "+occurrence.Values[2])
			continue
		}
		document.SkillLevelups = append(document.SkillLevelups, EquipmentSkillLevelup{Job: job, Skill: skill, Level: int32(level)})
	}

	for _, occurrence := range roleValues["base-explain"] {
		if document.BaseExplain == "" {
			document.BaseExplain = normalizeExplain(joinFieldValues(occurrence.Values))
		}
	}
	if value, ok := first("detail-explain"); ok {
		document.DetailExplain = normalizeExplain(joinFieldValues(value.Values))
	}
	if value, ok := first("flavor-text"); ok {
		document.FlavorText = normalizeDisplayText(joinFieldValuesPreserve(value.Values))
	}
	if value, ok := first("durability"); ok {
		document.DurabilityText = formatDurability(value, text, &document.Issues)
	}
	if value, ok := first("weight"); ok {
		document.WeightText = formatWeight(value, text, &document.Issues)
	}
	if value, ok := first("price"); ok {
		document.PriceText = formatPrice(value, text, &document.Issues)
	}

	return document
}

func readEquipmentIcon(document *EquipmentPreviewDocument, occurrence annotationrules.PreviewFieldValue, text string, issues *[]PreviewIssue) {
	pathIndex := occurrence.Field.Target.ImagePathToken
	imageIndex := occurrence.Field.Target.Index
	if pathIndex == nil || imageIndex == nil || *pathIndex < 0 || *imageIndex < 0 ||
		*pathIndex >= len(occurrence.Values) || *imageIndex >= len(occurrence.Values) {
		addPreviewIssue(issues, text, occurrence.Start, "warning", "icon", "图标记录缺少图片路径或索引")
		return
	}
	imagePath := strings.TrimSpace(occurrence.Values[*pathIndex])
	parsedIndex, err := strconv.ParseInt(strings.TrimSpace(occurrence.Values[*imageIndex]), 10, 32)
	if imagePath == "" || err != nil || parsedIndex < 0 {
		// An empty icon is a valid, non-blocking state. Do not manufacture a
		// broken image reference for the frontend.
		return
	}
	document.Icon = &ImageReference{Path: imagePath, Index: int32(parsedIndex)}
}

func enumValue(field annotationrules.FieldDefinition, raw string, document *EquipmentPreviewDocument, text string, start int) string {
	value := strings.TrimSpace(raw)
	if label, ok := field.Annotation.Values[value]; ok {
		return label
	}
	if len(field.Annotation.Values) > 0 && value != "" {
		addPreviewIssue(&document.Issues, text, start, "warning", field.Annotation.Title, "未知枚举值: "+value)
	}
	return value
}

func parseEquipmentAttribute(occurrence annotationrules.PreviewFieldValue, text string, issues *[]PreviewIssue) (EquipmentPreviewAttribute, bool) {
	label := occurrence.Field.Annotation.Title
	if occurrence.Field.Preview != nil && strings.TrimSpace(occurrence.Field.Preview.Label) != "" {
		label = occurrence.Field.Preview.Label
	}
	if strings.TrimSpace(label) == "" {
		return EquipmentPreviewAttribute{}, false
	}
	values := occurrence.Values
	if len(values) == 0 {
		return EquipmentPreviewAttribute{}, false
	}
	format := "signed-number"
	if occurrence.Field.Preview != nil {
		format = occurrence.Field.Preview.Format
	}
	if format == "range" {
		if len(values) < 2 {
			addPreviewIssue(issues, text, occurrence.Start, "warning", label, "范围属性缺少最大值或最小值")
			return EquipmentPreviewAttribute{}, false
		}
		left, leftOK := parsePreviewNumber(values[0])
		right, rightOK := parsePreviewNumber(values[1])
		if !leftOK || !rightOK {
			addPreviewIssue(issues, text, occurrence.Start, "warning", label, "范围属性包含非法数值")
			return EquipmentPreviewAttribute{}, false
		}
		if left > right {
			left, right = right, left
		}
		negative := left < 0 || right < 0
		return EquipmentPreviewAttribute{Label: label, Value: signedNumber(left) + "-" + trimLeadingPlus(formatNumber(right)), Negative: negative}, true
	}

	number, ok := parsePreviewNumber(firstValue(values))
	if !ok {
		addPreviewIssue(issues, text, occurrence.Start, "warning", label, "属性值不是有效数值: "+firstValue(values))
		return EquipmentPreviewAttribute{}, false
	}
	value := signedNumber(number)
	if format == "percent" {
		value += "%"
	}
	return EquipmentPreviewAttribute{Label: label, Value: value, Negative: number < 0}, true
}

func formatMinimumLevel(occurrence annotationrules.PreviewFieldValue, text string, issues *[]PreviewIssue) string {
	raw := firstValue(occurrence.Values)
	level := strings.Fields(strings.TrimSpace(raw))
	if len(level) == 0 {
		return ""
	}
	if _, err := strconv.Atoi(strings.Trim(level[0], "()")); err != nil {
		addPreviewIssue(issues, text, occurrence.Start, "warning", "minimum level", "使用等级不是有效整数: "+raw)
		return ""
	}
	return "Lv" + strings.Trim(level[0], "()") + "以上可以使用"
}

func formatDurability(occurrence annotationrules.PreviewFieldValue, text string, issues *[]PreviewIssue) string {
	raw := firstValue(occurrence.Values)
	number, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
	if err != nil || number < 0 {
		addPreviewIssue(issues, text, occurrence.Start, "warning", "durability", "耐久度不是有效整数: "+raw)
		return ""
	}
	return fmt.Sprintf("%d/%d", number, number)
}

func formatWeight(occurrence annotationrules.PreviewFieldValue, text string, issues *[]PreviewIssue) string {
	number, ok := parsePreviewNumber(firstValue(occurrence.Values))
	if !ok || number < 0 {
		addPreviewIssue(issues, text, occurrence.Start, "warning", "weight", "重量不是有效数值: "+firstValue(occurrence.Values))
		return ""
	}
	if number < 1000 {
		return formatNumber(number) + "g"
	}
	return formatNumber(number/1000) + "kg"
}

func formatPrice(occurrence annotationrules.PreviewFieldValue, text string, issues *[]PreviewIssue) string {
	number, ok := parsePreviewNumber(firstValue(occurrence.Values))
	if !ok {
		addPreviewIssue(issues, text, occurrence.Start, "warning", "value", "售价不是有效数值: "+firstValue(occurrence.Values))
		return ""
	}
	return formatNumber(math.Trunc(number / 5))
}

func parsePreviewNumber(value string) (float64, bool) {
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
}

func signedNumber(value float64) string {
	formatted := formatNumber(value)
	if value >= 0 {
		return "+" + formatted
	}
	return formatted
}

func trimLeadingPlus(value string) string {
	return strings.TrimPrefix(value, "+")
}

func formatNumber(value float64) string {
	if math.Trunc(value) == value {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func joinFieldValues(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			parts = append(parts, strings.TrimSpace(value))
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func joinFieldValuesPreserve(values []string) string {
	return strings.Join(values, " ")
}

func firstValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func normalizeExplain(value string) string {
	return strings.TrimSpace(normalizeDisplayText(strings.ReplaceAll(value, "%%", "%")))
}

func normalizeDisplayText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	// Some exported text stores line breaks as the two-character escape `\\n`.
	value = strings.ReplaceAll(value, `\r\n`, "\n")
	value = strings.ReplaceAll(value, `\r`, "\n")
	value = strings.ReplaceAll(value, `\n`, "\n")
	return value
}

func addPreviewIssue(issues *[]PreviewIssue, text string, offset int, severity, section, message string) {
	if issues == nil {
		return
	}
	*issues = append(*issues, PreviewIssue{
		Severity: severity,
		Line:     int32(lineAtUTF16Offset(text, offset)),
		Section:  section,
		Message:  message,
	})
}

func lineAtUTF16Offset(text string, target int) int {
	if target < 0 {
		return 1
	}
	line := 1
	offset := 0
	runes := []rune(text)
	for i, value := range runes {
		if offset >= target {
			return line
		}
		if value == '\n' {
			line++
		}
		if value == '\r' && i+1 < len(runes) && runes[i+1] == '\n' {
			continue
		}
		offset += len(utf16.Encode([]rune{value}))
	}
	return line
}

package script

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dop251/goja"
	"pvfine/internal/pvf"
)

// editUpgrade evaluates every callback before staging one complete edit.
func (b *gojaBindings) editUpgrade(file *FileHandle, call goja.FunctionCall) (goja.Value, error) {
	options, err := objectArgument(call.Argument(0))
	if err != nil {
		return nil, err
	}
	if options == nil {
		return nil, fmt.Errorf("editUpgrade 需要 options")
	}
	for _, key := range options.Keys() {
		switch key {
		case "equipment", "fromLevel", "toLevel", "successRate", "costCount":
		default:
			return nil, fmt.Errorf("未知强化选项 %s", key)
		}
	}
	path := strings.ToLower(strings.ReplaceAll(file.Path(), "\\", "/"))
	kind := "normal"
	if path == "etc/amplifyupgrade.etc" {
		kind = "amplify"
	} else if path != "etc/upgrade.etc" {
		return nil, fmt.Errorf("editUpgrade 仅支持强化/增幅文件")
	}
	equipment := "weapon"
	if !isMissingValue(options.Get("equipment")) {
		equipment, err = requiredString(options.Get("equipment"), "装备类型")
		if err != nil {
			return nil, err
		}
	}
	if equipment != "weapon" && equipment != "armor" {
		return nil, fmt.Errorf("equipment 必须是 weapon 或 armor")
	}
	text, err := file.Text()
	if err != nil {
		return nil, err
	}
	tokens, typed, err := pvf.SelectUpgradeTableTokens(text, kind)
	if err != nil {
		return nil, err
	}
	layout, err := pvf.DetectUpgradeTableLayout(tokens, typed)
	if err != nil {
		return nil, err
	}
	if equipment == "armor" && !layout.HasT2 {
		return nil, fmt.Errorf("旧版表格没有独立防具数据")
	}
	rateColumn := layout.RateColumn
	if equipment == "armor" {
		rateColumn = layout.ColumnsT2[0]
	}
	countColumn := layout.CountColumn
	width := layout.Width
	for _, token := range tokens {
		if token.TokenType != 0 && token.TokenType != 2 {
			return nil, fmt.Errorf("表格包含非数值数据")
		}
	}
	maximum := len(tokens) / width
	if !typed && maximum > 31 {
		maximum = 31
	}
	from, err := optionInteger(options, "fromLevel", 1)
	if err != nil {
		return nil, err
	}
	to, err := optionInteger(options, "toLevel", int64(maximum))
	if err != nil {
		return nil, err
	}
	if from < 1 || to < from || to > int64(maximum) {
		return nil, fmt.Errorf("等级范围必须位于 1～%d", maximum)
	}
	type update struct {
		name   string
		column int
		value  goja.Value
	}
	updates := []update{}
	for _, item := range []update{{"successRate", rateColumn, options.Get("successRate")}, {"costCount", countColumn, options.Get("costCount")}} {
		if !isMissingValue(item.value) {
			updates = append(updates, item)
		}
	}
	if len(updates) == 0 {
		return nil, fmt.Errorf("请提供 successRate 或 costCount")
	}
	type patch struct {
		start, end int
		value      string
	}
	patches := []patch{}
	offsets := upgradeByteOffsets(text)
	for level := int(from); level <= int(to); level++ {
		if err := b.host.checkContext(); err != nil {
			return nil, err
		}
		for _, item := range updates {
			token := tokens[(level-1)*width+item.column]
			current, err := strconv.ParseFloat(token.Value, 64)
			if err != nil || token.TokenType != 0 {
				return nil, fmt.Errorf("第 %d 级 %s 必须是整数", level, item.name)
			}
			if item.name == "successRate" {
				if current < 0 || current > 100000 {
					return nil, fmt.Errorf("原始失败概率超出范围")
				}
				current = 100 - current/1000
			}
			value := item.value
			if fn, ok := goja.AssertFunction(value); ok {
				value, err = fn(goja.Undefined(), b.vm.ToValue(level), b.vm.ToValue(current))
				if err != nil {
					return nil, err
				}
			}
			switch value.Export().(type) {
			case int64, float64, int, int32, float32:
			default:
				return nil, fmt.Errorf("第 %d 级 %s 计算结果必须是数字", level, item.name)
			}
			number := value.ToFloat()
			if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
				return nil, fmt.Errorf("第 %d 级 %s 计算结果无效", level, item.name)
			}
			if item.name == "successRate" {
				if number > 100 {
					return nil, fmt.Errorf("成功率必须位于 0～100")
				}
				number = math.Round((100 - number) * 1000)
			} else if number > math.MaxInt32 || math.Trunc(number) != number {
				return nil, fmt.Errorf("消耗数量必须是非负 32 位整数")
			}
			if token.Start < 0 || token.End >= len(offsets) {
				return nil, fmt.Errorf("表格位置无效")
			}
			patches = append(patches, patch{offsets[token.Start], offsets[token.End], strconv.FormatInt(int64(number), 10)})
		}
	}
	sort.Slice(patches, func(i, j int) bool { return patches[i].start > patches[j].start })
	updated := text
	for _, patch := range patches {
		updated = updated[:patch.start] + patch.value + updated[patch.end:]
	}
	latest, err := file.Text()
	if err != nil {
		return nil, err
	}
	if latest != text {
		return nil, fmt.Errorf("计算期间文件发生变化，已拒绝覆盖")
	}
	if updated != text {
		if err := file.SetText(updated); err != nil {
			return nil, err
		}
	}
	return b.vm.ToValue(to - from + 1), nil
}

// Translate normalized UTF-16 token offsets to original UTF-8 byte offsets,
// retaining CRLF and lone CR line endings in source patches.
func upgradeByteOffsets(text string) []int {
	offsets := []int{0}
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == '\r' && i+size < len(text) && text[i+size] == '\n' {
			size++
		}
		if r > 0xffff {
			offsets = append(offsets, i)
		}
		i += size
		offsets = append(offsets, i)
	}
	return offsets
}

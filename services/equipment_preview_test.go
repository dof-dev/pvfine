package services

import (
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func TestPreviewServiceParseEQUSampleShape(t *testing.T) {
	text := `[name]
	` + "`名刀 - 观世正宗`" + `

[basic explain]
	` + "`暴击伤害 +32%%`" + `

[detail explain]
	` + "`暴击伤害 +32%% (暴击伤害加成效果取最高值， 且无法叠加)`" + `

[flavor text]
	` + "`    据说是某个岛国的十大名刀之一……嗯， 回头让小铁柱照着打一把更好的……  --西岚`" + `

[rarity]
	4

[usable job]
	` + "`[swordman]` `[demonic swordman]` `[at swordman]` `[knight]`" + `
[/usable job]

[attach type]
	` + "`[trade]`" + `

[minimum level]
	85 (some text)

[physical attack]
	65
[magical attack]
	97
[attack speed]
	80
[cast speed]
	40
[stuck]
	1
[anti evil]
	794
[value]
	131040
[equipment physical attack]
	912 783
[equipment magical attack]
	1008 865
[separate attack]
	589 382
[skill levelup]
	` + "`[swordman]` 38 2" + `
	` + "`[at swordman]` 14 2" + `
[/skill levelup]

[icon]
	` + "`item/new_equipment/01_weapon/swordman/katana/katana.img` 118" + `

[equipment type]
	` + "`[weapon]` 23" + `

[durability]
	45
[weight]
	2800
[item group name]
	` + "`katana`" + `
`
	result, err := NewPreviewService().ParseEQU(-1, text)
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "名刀 - 观世正宗" || result.Rarity != 4 || result.RarityLabel != "史诗" {
		t.Fatalf("header = %#v", result)
	}
	if result.Icon == nil || result.Icon.Index != 118 || result.EquipmentType != "武器" || result.ItemGroupName != "太刀" || result.AttachType != "不可交易" {
		t.Fatalf("references = %#v", result)
	}
	if result.MinimumLevelText != "Lv85以上可以使用" || result.WeightText != "2.8kg" || result.PriceText != "26208" || result.DurabilityText != "45/45" {
		t.Fatalf("converted fields = %#v", result)
	}
	if strings.Contains(result.BaseExplain, "%%") || !strings.Contains(result.DetailExplain, "%") || !strings.Contains(result.FlavorText, "……嗯， 回头") || len(result.UsableJobs) != 4 {
		t.Fatalf("text fields = %#v", result)
	}
	if len(result.BaseAttributes) != 3 || result.BaseAttributes[0].Value != "+783-912" {
		t.Fatalf("base attributes = %#v", result.BaseAttributes)
	}
	if len(result.FourDimensions) != 2 || result.FourDimensions[0].Value != "+65" || result.FourDimensions[1].Value != "+97" {
		t.Fatalf("four dimensions = %#v", result.FourDimensions)
	}
	if len(result.OtherAttributes) != 4 || result.OtherAttributes[0].Value != "+794" || result.OtherAttributes[1].Value != "+80%" {
		t.Fatalf("other attributes = %#v", result.OtherAttributes)
	}
	if len(result.SkillLevelups) != 2 || result.SkillLevelups[0].Job != "鬼剑士" || result.SkillLevelups[0].Skill != "38" || result.SkillLevelups[0].Level != 2 {
		t.Fatalf("skill levelups = %#v", result.SkillLevelups)
	}
}

func TestPreviewServiceParseEQUAllowsEmptyIconAndUnknownEnum(t *testing.T) {
	result, err := NewPreviewService().ParseEQU(-1, "[name]\n`测试`\n[rarity]\n99\n[icon]\n`` 0\n[item group name]\n`unknown-group`\n[value]\n10")
	if err != nil {
		t.Fatal(err)
	}
	if result.Icon != nil {
		t.Fatalf("empty icon = %#v", result.Icon)
	}
	if result.RarityLabel != "99" || result.ItemGroupName != "unknown-group" {
		t.Fatalf("unknown enum fallback = %#v", result)
	}
	foundWarning := false
	for _, issue := range result.Issues {
		if issue.Severity == "warning" && strings.Contains(issue.Message, "未知枚举值") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("issues = %#v", result.Issues)
	}
}

func TestPreviewServiceParseEQUNegativeAttribute(t *testing.T) {
	result, err := NewPreviewService().ParseEQU(-1, "[name]\n`负收益`\n[hit rate]\n-5")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.OtherAttributes) != 0 {
		t.Fatalf("unconfigured fields should be omitted: %#v", result.OtherAttributes)
	}
	result, err = NewPreviewService().ParseEQU(-1, "[name]\n`负收益`\n[stuck]\n-5")
	if err != nil || len(result.OtherAttributes) != 1 || !result.OtherAttributes[0].Negative || result.OtherAttributes[0].Value != "-5%" {
		t.Fatalf("negative attribute = %#v, err=%v", result.OtherAttributes, err)
	}
}

func TestPreviewServiceParseEQUOmitsAllUsableJob(t *testing.T) {
	result, err := NewPreviewService().ParseEQU(-1, "[name]\n`通用装备`\n[usable job]\n`[all]`\n[/usable job]")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.UsableJobs) != 0 {
		t.Fatalf("usable jobs = %#v", result.UsableJobs)
	}
}

func TestPreviewServiceParseEQUPreservesDisplayLineBreaks(t *testing.T) {
	result, err := NewPreviewService().ParseEQU(-1, "[basic explain]\n`第一行\\n第二行`\n[flavor text]\n`第一段\\n  第二段`")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.BaseExplain, "第一行\n第二行") || !strings.Contains(result.FlavorText, "第一段\n  第二段") {
		t.Fatalf("line breaks = base %q, flavor %q", result.BaseExplain, result.FlavorText)
	}
}

func TestPreviewServiceParseEQUResolvesContextualSkillName(t *testing.T) {
	a := pvf.New()
	if _, err := a.AddFileText("skill/swordmanskill.lst", "38 `swordman/38.skl`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("skill/swordman/38.skl", "[name]\n`升龙剑`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	equIndex, err := a.AddFileText("equipment/test.equ", "[name]\n`测试装备`\n[skill levelup]\n`[swordman]` 38 2\n[/skill levelup]", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	result, err := NewPreviewService(c).ParseEQU(equIndex, "[name]\n`测试装备`\n[skill levelup]\n`[swordman]` 38 2\n[/skill levelup]")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SkillLevelups) != 1 || result.SkillLevelups[0].Skill != "升龙剑" {
		t.Fatalf("skill levelups = %#v, issues=%#v", result.SkillLevelups, result.Issues)
	}
}

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

func TestPreviewServiceParseEQUResolvesNamePlaceholder(t *testing.T) {
	a := pvf.New()
	strTable := make([]byte, 0, 64)
	for _, r := range "equip_name_1>白色兽语腰带 [A款]\r\n" {
		strTable = append(strTable, byte(r), byte(r>>8))
	}
	if _, err := a.AddFileText("list/n_string.lst", "1 `String/Test.uv.str`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	a.AddFile("String/Test.uv.str", strTable, pvf.TypeScript)
	equIndex, err := a.AddFileText("equipment/test.equ",
		"[name]\n{8=`<1::equip_name_1>`}\n[name2]\n{8=`<1::equip_name_1>`}\n[rarity]\n4", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	result, err := NewPreviewService(c).ParseEQU(equIndex,
		"[name]\n{8=`<1::equip_name_1>`}\n[name2]\n{8=`<1::equip_name_1>`}\n[rarity]\n4")
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "白色兽语腰带 [A款]" {
		t.Fatalf("name = %q", result.Name)
	}
	if result.Name2 != "白色兽语腰带 [A款]" {
		t.Fatalf("name2 = %q", result.Name2)
	}
}

// TestPreviewServiceParseEQURealPaged110Name runs the whole display path on the
// retail Paged110 archive: the .equ stores a placeholder, the preview shows the
// string-table text, and a name the localization left empty is answered by the
// Korean overlay and flagged.
func TestPreviewServiceParseEQURealPaged110Name(t *testing.T) {
	a, _ := openRealPaged110(t)
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	for _, tc := range []struct{ path, want string }{
		{"character/demoniclancer/avatar/belt/514530375.equ", "白色兽语腰带 [A款]"},
		{"equipment/character/archer/avatar/belt/117530002.equ", "稀有克隆装扮腰部"},
		{"equipment/character/archer/avatar/belt/117530006.equ", "포니 비즈 뱅글[A타입]（未翻译）"},
	} {
		index, ok := a.Find(tc.path)
		if !ok {
			t.Errorf("%s not found", tc.path)
			continue
		}
		text, err := a.Text(index)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "::") {
			t.Errorf("%s carries no placeholder: %q", tc.path, text)
			continue
		}
		result, err := NewPreviewService(c).ParseEQU(index, text)
		if err != nil {
			t.Fatal(err)
		}
		if result.Name != tc.want {
			t.Errorf("%s -> name %q, want %q", tc.path, result.Name, tc.want)
		}
	}
}

// TestPreviewServiceParseEQUMarksOverlayFallback covers the untranslated case:
// the base localization lists the key with an empty value, so the name comes
// from the Korean overlay and is flagged for the reader.
func TestPreviewServiceParseEQUMarksOverlayFallback(t *testing.T) {
	a := pvf.New()
	encode := func(s string) []byte {
		out := make([]byte, 0, len(s)*2)
		for _, r := range s {
			out = append(out, byte(r), byte(r>>8))
		}
		return out
	}
	if _, err := a.AddFileText("list/n_string.lst", "3 `String/Equipment.uv.str`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("list/n_string_kor.lst", "3 `String/Equipment.kor.str`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	a.AddFile("String/Equipment.uv.str", encode("name_1=\r\n"), pvf.TypeScript)
	a.AddFile("String/Equipment.kor.str", encode("name_1>포니 비즈 뱅글[A타입]\r\n"), pvf.TypeScript)
	equIndex, err := a.AddFileText("equipment/test.equ", "[name]\n{8=`<3::name_1>`}", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	result, err := NewPreviewService(c).ParseEQU(equIndex, "[name]\n{8=`<3::name_1>`}")
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "포니 비즈 뱅글[A타입]（未翻译）" {
		t.Fatalf("name = %q", result.Name)
	}
}

func TestPreviewServiceParseEQUKeepsUnknownPlaceholder(t *testing.T) {
	a := pvf.New()
	equIndex, err := a.AddFileText("equipment/test.equ", "[name]\n{8=`<9::missing>`}", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	result, err := NewPreviewService(c).ParseEQU(equIndex, "[name]\n{8=`<9::missing>`}")
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "<9::missing>" {
		t.Fatalf("name = %q", result.Name)
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

func TestPreviewServiceParseEQUSetPreview(t *testing.T) {
	a := pvf.New()
	list := "[equipment part set]\n99 `missing.etc` `其它套装` `其它部位` 0 0 0\n[/equipment part set]\n" +
		"[equipment part set]\n42 `sets/effect.etc` `格拉西亚` `上衣` 1 2 3 `下装` 4 5 6\n[/equipment part set]"
	if _, err := a.AddFileText(equipmentPartSetListPath, list, pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	effects := "[piece set ability]\n3\n[parameter basic explain]\n`力量 +50\\n智力 +50%%`\n[/parameter basic explain]\n" +
		"[parameter detail explain]\n`力量 +50 (详细)`\n[/parameter detail explain]\n[/piece set ability]\n" +
		"[piece set ability]\n5\n[parameter basic explain]\n`光属性强化 +12`\n[/parameter basic explain]\n[/piece set ability]"
	if _, err := a.AddFileText("equipment/sets/effect.etc", effects, pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	// The same relative path under etc/ must not be used for set effects.
	if _, err := a.AddFileText("etc/sets/effect.etc", "[piece set ability]\n3", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	equipment, err := a.AddFileText("equipment/test.equ", "[name]\n`测试装备`\n[part set index]\n42", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	service := NewPreviewService(c)
	result, err := service.ParseEQU(equipment, "[name]\n`测试装备`\n[part set index]\n42")
	if err != nil {
		t.Fatal(err)
	}
	if result.PartSet == nil || result.PartSet.Name != "格拉西亚" || len(result.PartSet.Parts) != 2 || result.PartSet.Parts[1] != "下装" {
		t.Fatalf("set = %#v, issues = %#v", result.PartSet, result.Issues)
	}
	if len(result.PartSet.Abilities) != 2 || result.PartSet.Abilities[0].Pieces != 3 ||
		result.PartSet.Abilities[1].Pieces != 5 ||
		result.PartSet.Abilities[0].BaseExplain != "力量 +50\n智力 +50%" ||
		result.PartSet.Abilities[0].DetailExplain != "力量 +50 (详细)" {
		t.Fatalf("abilities = %#v", result.PartSet.Abilities)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("issues = %#v", result.Issues)
	}
	for _, test := range []struct {
		name string
		text string
		warn bool
	}{
		{"no set", "[name]\n`测试装备`", false},
		{"unmatched set", "[name]\n`测试装备`\n[part set index]\n43", true},
		{"missing effect file", "[name]\n`测试装备`\n[part set index]\n99", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := service.ParseEQU(equipment, test.text)
			if err != nil || got.PartSet != nil || len(got.Issues) > 0 != test.warn {
				t.Fatalf("partSet = %#v, issues = %#v, err = %v", got.PartSet, got.Issues, err)
			}
		})
	}
}

func TestPreviewServiceParseEQUSetPreviewRejectsIncompleteEffect(t *testing.T) {
	a := pvf.New()
	if _, err := a.AddFileText(equipmentPartSetListPath,
		"[equipment part set]\n42 `effect.etc` `测试套装` `部位` 1 2 3\n[/equipment part set]", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("equipment/effect.etc",
		"[piece set ability]\n3\n[parameter detail explain]\n`只有详细说明`\n[/parameter detail explain]\n[/piece set ability]", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	equipment, err := a.AddFileText("equipment/test.equ", "[part set index]\n42", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	got, err := NewPreviewService(c).ParseEQU(equipment, "[part set index]\n42")
	if err != nil || got.PartSet != nil || len(got.Issues) == 0 || got.Issues[0].Severity != "warning" {
		t.Fatalf("partSet = %#v, issues = %#v, err = %v", got.PartSet, got.Issues, err)
	}
}

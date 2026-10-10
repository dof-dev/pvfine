package services

import (
	"os"
	"pvfine/internal/pvf"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestSkillParseModesAndSource(t *testing.T) {
	text := "[name]\n`技能😀`\n[dungeon]\n[static data]\n10 2.5\n[/static data]\n[level info]\n2 100 200 300 400\n[/level info]\n[/dungeon]\n[pvp]\n[static data]\n20\n[/static data]\n[level info]\n1 50\n[/level info]\n[/pvp]\n[level property]\n1 99 `伤害<int>，减速<float2>%%\n次数<int>` -1 0 1.0 1 1 0.1 0 0 2.0\n[/level property]"
	doc := parseSkill(text)
	if doc.Name != "技能😀" || len(doc.Modes) != 2 {
		t.Fatalf("invalid document: %+v", doc)
	}
	dungeon, pvp := doc.Modes[0], doc.Modes[1]
	if dungeon.Width != 2 || len(dungeon.Levels) != 2 || dungeon.Levels[1][1].Value != 400 || len(pvp.Levels) != 1 {
		t.Fatal("incorrect dimensions")
	}
	if len(dungeon.Properties) != 1 || len(dungeon.Properties[0].Bindings) != 3 || !dungeon.Properties[0].Bindings[0].Dynamic || dungeon.Properties[0].Bindings[1].Multiplier != .1 {
		t.Fatal("incorrect properties")
	}
	if len(dungeon.Issues) != 0 || len(pvp.Issues) != 0 {
		t.Fatal("unexpected issues")
	}
	n := dungeon.Static[1]
	source := utf16.Encode([]rune(text))
	if string(utf16.Decode(source[n.Start:n.End])) != "2.5" || n.TokenType != 2 {
		t.Fatal("source position or token type lost")
	}
}

func TestSkillMalformedAndScopedProperties(t *testing.T) {
	doc := parseSkill("[dungeon]\n[static data]\n1\n[/static data]\n[static data]\n2\n[/static data]\n[level info]\n2 10 20 30\n[/level info]\n[level property]\n0 0 `私有<int>` 0 0 1.0\n[/level property]\n[/dungeon]\n[pvp]\n[static data]\n3\n[/static data]\n[/pvp]\n[level property]\n0 0 `通用<int>` 0 0 1.0\n[/level property]")
	if len(doc.Modes[0].Static) != 0 || len(doc.Modes[0].Levels) != 0 || len(doc.Modes[0].Issues) != 2 {
		t.Fatal("unsafe malformed data exposed")
	}
	if doc.Modes[0].Properties[0].Template != "私有<int>" || doc.Modes[1].Properties[0].Template != "通用<int>" {
		t.Fatal("properties leaked across modes")
	}
	bad := parseSkill("[dungeon]\n[level property]\n0 0 `伤害<int>`\n[/level property]\n[/dungeon]")
	if len(bad.Modes[0].Issues) == 0 {
		t.Fatal("missing bindings not reported")
	}
}

func TestSkillReadDraftAndCatalog(t *testing.T) {
	a := pvf.New()
	index, err := a.AddFileText("skill/atgunner/c4.skl", "[name]\n`旧名`\n[dungeon]\n[static data]\n10\n[/static data]\n[/dungeon]", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	for _, filePath := range []string{"monster/test.skl", "etc/test.skl", "skills/test.skl", "other/skill/test.skl"} {
		if _, err := a.AddFileText(filePath, "[name]\n`目录外技能`", pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	s := NewFileGUIService(c)
	entries, err := s.ListSkills()
	if err != nil || len(entries) != 1 || entries[0].Job != "atgunner" {
		t.Fatalf("catalog: %+v %v", entries, err)
	}
	doc, err := s.ReadSkill(index, "[name]\n`草稿`\n[dungeon]\n[static data]\n99\n[/static data]\n[/dungeon]")
	if err != nil || doc.Name != "草稿" || doc.Modes[0].Static[0].Value != 99 {
		t.Fatalf("draft: %+v %v", doc, err)
	}
	original, _ := a.Text(index)
	if strings.Contains(original, "99") {
		t.Fatal("read mutated archive")
	}
}

func TestSkillReal90CN(t *testing.T) {
	filename := os.Getenv("PVF_TESTFILE")
	if filename == "" {
		t.Skip("PVF_TESTFILE 未设置")
	}
	a, err := pvf.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	index, ok := a.Find("skill/atgunner/c4.skl")
	if !ok {
		t.Skip("无示例技能")
	}
	text, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	doc := parseSkill(text)
	if len(doc.Modes) != 2 || len(doc.Modes[0].Static) != 20 || doc.Modes[0].Width != 8 || len(doc.Modes[0].Levels) != 60 || len(doc.Modes[1].Levels) != 70 {
		t.Fatal("C4 数据结构不符")
	}
	if len(doc.Modes[0].Properties) != 1 || len(doc.Modes[0].Properties[0].Bindings) != 5 {
		t.Fatal("C4 描述解析失败")
	}
	count, issues := 0, 0
	issueKinds := map[string]int{}
	for i := int32(0); i < a.FileCount(); i++ {
		if !strings.HasPrefix(strings.ToLower(a.Path(i)), "skill/") || !strings.HasSuffix(strings.ToLower(a.Path(i)), ".skl") {
			continue
		}
		body, err := a.Text(i)
		if err != nil {
			t.Fatal(err)
		}
		skill := parseSkill(body)
		count++
		for _, m := range skill.Modes {
			issues += len(m.Issues)
			for _, issue := range m.Issues {
				issueKinds[issue]++
			}
		}
	}
	t.Logf("扫描 %d 个技能，结构提示 %d 项", count, issues)
	t.Logf("结构提示类型: %v", issueKinds)
	service := NewFileGUIService(&core{archive: a})
	entries, err := service.ListSkills()
	if err != nil || len(entries) != count {
		t.Fatalf("catalog count: %d, want %d: %v", len(entries), count, err)
	}
	found := false
	for _, entry := range entries {
		if entry.FileIndex == index {
			found = entry.Job == "atgunner" && entry.Name == doc.Name
		}
	}
	if !found {
		t.Fatal("C4 missing from catalog")
	}

	// Exercise the same positional draft patches through the actual PVF encoder.
	patches := append([]SkillNumber{doc.Modes[0].Static[1]}, doc.Modes[0].Levels[0][0])
	for _, row := range doc.Modes[0].Levels[1:] {
		patches = append(patches, row[0])
	}
	sort.Slice(patches, func(i, j int) bool { return patches[i].Start > patches[j].Start })
	units := utf16.Encode([]rune(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)))
	for _, n := range patches {
		value := n.Value + 100
		if n.Start == doc.Modes[0].Static[1].Start {
			value = 12000
		}
		replacement := utf16.Encode([]rune(strconv.FormatFloat(value, 'f', 0, 64)))
		updated := append([]uint16{}, units[:n.Start]...)
		updated = append(updated, replacement...)
		units = append(updated, units[n.End:]...)
	}
	if err := a.SetText(index, string(utf16.Decode(units))); err != nil {
		t.Fatal(err)
	}
	saved, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip := parseSkill(saved)
	if roundTrip.Modes[0].Static[1].Value != 12000 || roundTrip.Modes[1].Static[1].Value != doc.Modes[1].Static[1].Value {
		t.Fatal("static edit leaked to pvp")
	}
	for i, row := range roundTrip.Modes[0].Levels {
		if row[0].Value != doc.Modes[0].Levels[i][0].Value+100 || row[1].Value != doc.Modes[0].Levels[i][1].Value {
			t.Fatalf("level %d changed incorrectly", i+1)
		}
	}
	if roundTrip.Modes[0].Properties[0].Template != doc.Modes[0].Properties[0].Template {
		t.Fatal("description template changed")
	}
}

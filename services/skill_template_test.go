package services

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"pvfine/internal/pvf"
)

func TestSkillTemplateNormalization(t *testing.T) {
	for _, test := range []struct{ name, input, want string }{
		{"legacy", "伤害<int>\n减速<float2>%%", "伤害<int>\n减速<float2>%%"},
		{"aliases-and-escaped-newlines", `伤害<quorum>\n减速<decimal1>%%\r\n数量<int>\r倍率<float2>`, "伤害<int>\n减速<float1>%%\n数量<int>\n倍率<float2>"},
		{"mixed-and-precision", ` <quorum> <int> <decimal0> <decimal3> <float1> <decimal> `, ` <int> <int> <float0> <float3> <float1> <float> `},
		{"literal-lines-and-unknown", "  首行\r\n次行 <unknown> <12::missing>  ", "  首行\n次行 <unknown> <12::missing>  "},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeSkillTemplate(test.input); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestSkillReadReferencedTemplateBeforeBindings(t *testing.T) {
	a := pvf.New()
	defer a.Release()
	if _, err := a.AddFileText("list/n_string.lst", "1 `String/Skills.uv.str`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	const template = `伤害<quorum>，数量<int>\n减速<decimal1>%% / 倍率<float2>`
	table := []byte{}
	for _, unit := range utf16.Encode([]rune("property>" + template + "\r\n")) {
		table = append(table, byte(unit), byte(unit>>8))
	}
	a.AddFile("String/Skills.uv.str", table, pvf.TypeScript)
	index, err := a.AddFileText("skill/test/test.skl", "[name]\n`技能😀`", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	s := NewFileGUIService(&core{archive: a})
	for _, tokenType := range []string{"8", "10"} {
		t.Run("token="+tokenType, func(t *testing.T) {
			text := "[name]\n`技能😀`\n[dungeon]\n[static data]\n10\n[/static data]\n[level info]\n2 100 123\n[/level info]\n[/dungeon]\n[level property]\n1 99 {" + tokenType + "=`<1::property>`} -1 0 2.0 0 0 1.0 -1 1 0.1 -1 1 0.01\n[/level property]"
			doc, err := s.ReadSkill(index, text)
			if err != nil {
				t.Fatal(err)
			}
			mode := doc.Modes[0]
			if len(mode.Issues) != 0 || len(mode.Properties) != 1 || len(mode.Properties[0].Bindings) != 4 {
				t.Fatalf("reference bindings: %+v", mode)
			}
			p := mode.Properties[0]
			if p.Template != "伤害<int>，数量<int>\n减速<float1>%% / 倍率<float2>" {
				t.Fatalf("template: %q", p.Template)
			}
			if !p.Bindings[0].Dynamic || p.Bindings[1].Dynamic || p.Bindings[2].Index != 1 || p.Bindings[2].Multiplier != .1 {
				t.Fatalf("bindings: %+v", p.Bindings)
			}
			n := mode.Levels[0][0]
			units := utf16.Encode([]rune(text))
			if string(utf16.Decode(units[n.Start:n.End])) != "100" {
				t.Fatal("template resolution changed numeric positions")
			}
			if value, ok := a.LookupStringTable(1, "property"); !ok || value != template {
				t.Fatal("display normalization changed string table")
			}
		})
	}
}

func TestSkillReal110US(t *testing.T) {
	filename := os.Getenv("PVF_TESTFILE")
	if filename == "" {
		t.Skip("PVF_TESTFILE 未设置")
	}
	a, err := pvf.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	if a.ClientVersion() != "110US" {
		t.Skip("使用 110US 真实样例验证")
	}
	s := NewFileGUIService(&core{archive: a})
	for _, filePath := range []string{"skill/atgunner/c4.skl", "skill/gunner/c4.skl"} {
		t.Run(filePath, func(t *testing.T) {
			index, ok := a.Find(filePath)
			if !ok {
				t.Fatal("缺少 C4 示例")
			}
			text, err := a.Text(index)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := s.ReadSkill(index, text)
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Modes) != 2 {
				t.Fatal("缺少技能模式")
			}
			for _, mode := range doc.Modes {
				if mode.Width != 10 || len(mode.Levels) != 70 || len(mode.Properties) != 1 || len(mode.Properties[0].Bindings) != 6 || len(mode.Issues) != 0 {
					t.Fatalf("C4 structure: mode=%s width=%d levels=%d issues=%v", mode.ID, mode.Width, len(mode.Levels), mode.Issues)
				}
				p := mode.Properties[0]
				if strings.Contains(p.Template, `\n`) || strings.Count(p.Template, "\n") != 5 || !strings.Contains(p.Template, "<float1>%%") || strings.Contains(p.Template, "<quorum>") {
					t.Fatalf("template normalization: %q", p.Template)
				}
				for _, b := range p.Bindings {
					if b.Dynamic && b.Index >= mode.Width || !b.Dynamic && b.Index >= len(mode.Static) {
						t.Fatal("invalid binding index")
					}
				}
			}
			// A GUI parameter edit must retain the string reference, localized template,
			// other columns, and the other mode through the Paged110 binary encoder.
			n := doc.Modes[0].Levels[0][0]
			units := utf16.Encode([]rune(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)))
			replacement := utf16.Encode([]rune(strconv.FormatFloat(n.Value+100, 'f', 0, 64)))
			modified := append([]uint16{}, units[:n.Start]...)
			modified = append(modified, replacement...)
			modified = append(modified, units[n.End:]...)
			if err := a.SetText(index, string(utf16.Decode(modified))); err != nil {
				t.Fatal(err)
			}
			saved, err := a.Text(index)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(saved, "{8=`<12::C4") {
				t.Fatal("description reference was rewritten")
			}
			after, err := s.ReadSkill(index, saved)
			if err != nil {
				t.Fatal(err)
			}
			if after.Modes[0].Levels[0][0].Value != n.Value+100 || after.Modes[0].Levels[0][1].Value != doc.Modes[0].Levels[0][1].Value || after.Modes[1].Levels[0][0].Value != doc.Modes[1].Levels[0][0].Value || after.Modes[0].Properties[0].Template != doc.Modes[0].Properties[0].Template {
				t.Fatal("parameter round trip changed unrelated data")
			}
		})
	}
}

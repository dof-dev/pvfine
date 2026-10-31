package pvf

import (
	"pvfine/internal/rendering"
	"strings"
	"testing"
)

func TestDefaultUpgradeAndForgeRendering(t *testing.T) {
	engine, err := rendering.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	a := New()
	a.SetScriptRenderer(engine)
	decode := func(text, path string) string {
		t.Helper()
		raw, err := a.encodeScript(text)
		if err != nil {
			t.Fatal(err)
		}
		return a.decodeScriptForPath(raw, path)
	}
	for _, tc := range []struct{ path, kind string }{{"etc/upgrade.etc", "normal"}, {"etc/amplifyupgrade.etc", "amplify"}} {
		row := "1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24"
		input := "[table]\n[table type]\n`" + tc.kind + "` " + row + " " + row + "\n[/table]\n[table]\n[table type]\n`" + tc.kind + "` " + row + "\n[/table]\n[safe upgrade]\n0 1 94000 1000 2 10327281 40 124448 1 1 20000 5000 1 10327281 41 124448\n[/safe upgrade]"
		got := decode(input, tc.path)
		dataLines := 0
		types := 0
		safeLines := 0
		for _, line := range strings.Split(got, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 24 {
				dataLines++
			}
			if strings.TrimSpace(line) == "`"+tc.kind+"`" {
				types++
			}
			if len(fields) == 8 {
				safeLines++
			}
		}
		if dataLines != 3 || types != 2 || safeLines != 2 {
			t.Fatalf("typed layout: %q", got)
		}
		// The same section names in unrelated files retain ordinary rendering.
		if engine.SectionFormat("etc/unrelated.etc", "table type").TokensPerLine != 0 {
			t.Fatal("format leaked")
		}
	}
	legacy := "1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17"
	got := decode("[table]\n"+legacy+" "+legacy+"\n[/table]", "etc/upgrade.etc")
	if strings.Count(got, "\t"+strings.ReplaceAll(legacy, " ", "\t")+"\n") != 2 {
		t.Fatalf("legacy layout: %q", got)
	}
	got = decode("[table]\n2 1 10000 1 3 2 5000 1.11\n[/table]\n[item weights by grade]\n1 3326 1 2 3326 1\n[/item weights by grade]\n[item weights by grade 100lv]\n96 3326 51 97 3326 51\n[/item weights by grade 100lv]\n[separate upgrade effect]\n255 255 255 0 50 2000 255 255 255 0 80 2000\n[/separate upgrade effect]", "etc/upgrade_separate.etc")
	counts := map[int]int{}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "\t") {
			counts[len(strings.Fields(line))]++
		}
	}
	if counts[4] != 2 || counts[3] != 4 || counts[6] != 2 {
		t.Fatalf("forge layout: %q", got)
	}
	// Text edits still round-trip every token with default rendering active.
	encoded, err := a.encodeScript(got)
	if err != nil {
		t.Fatal(err)
	}
	if next := a.decodeScriptForPath(encoded, "etc/upgrade_separate.etc"); next != got {
		t.Fatalf("unstable render: %q", next)
	}
}

func Test115CNForgeRendering(t *testing.T) {
	a := New()
	a.format = paged115CNProfile
	row := "2 1 2.6 1 10000 1"
	raw, err := a.encodeScript("[table]\n" + row + " " + row + "\n[/table]")
	if err != nil {
		t.Fatal(err)
	}
	text := a.decodeScriptForPath(raw, "etc/upgrade_separate.etc")
	if strings.Count(text, "\t"+strings.ReplaceAll(row, " ", "\t")+"\n") != 2 {
		t.Fatal("115CN forge table did not render six values per row")
	}
}

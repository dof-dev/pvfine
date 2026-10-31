package pvf

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"

	"pvfine/internal/rendering"
)

func TestScriptRenderingRuleCanTargetSectionByExtension(t *testing.T) {
	a := New()
	input := "[records]\n1 2 3 4 5\n[/records]"
	encoded, err := a.encodeScript(input)
	if err != nil {
		t.Fatalf("encodeScript() error = %v", err)
	}
	engine, err := rendering.Parse([]byte(`{
  "version": 1,
  "rules": [
    {
      "id": "skl.file",
      "match": {"extensions": [".skl"]},
      "target": {"kind": "file"},
      "format": {"tokensPerLine": 1}
    },
    {
      "id": "skl.records",
      "match": {"extensions": [".skl"]},
      "target": {"kind": "section", "section": "records"},
      "format": {"tokensPerLine": 2}
    }
  ]
}`))
	if err != nil {
		t.Fatalf("rendering.Parse() error = %v", err)
	}
	a.SetScriptRenderer(engine)

	want := "[records]\n\t1\t2\n\t3\t4\n\t5\n[/records]\n"
	if got := a.decodeScriptForPath(encoded, "SKILLS/TEST.SKL"); got != want {
		t.Fatalf("scoped rendering = %q, want %q", got, want)
	}
	if got := a.decodeScriptForPath(encoded, "skills/test.equ"); got != "[records]\n\t1\t2\t3\t4\t5\n[/records]\n" {
		t.Fatalf("unmatched extension rendering = %q", got)
	}
}

func TestScriptRenderingSupportsLeadingTokenOffset(t *testing.T) {
	a := New()
	encoded, err := a.encodeScript("[records]\n10 20 1 2 3 4 5\n[/records]")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := rendering.Parse([]byte(`{
  "version": 1,
  "rules": [{
    "id": "records.offset",
    "target": {"kind": "section", "section": "records"},
    "format": {"offset": 2, "tokensPerLine": 2}
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	a.SetScriptRenderer(engine)
	want := "[records]\n\t10\n\t20\n\t1\t2\n\t3\t4\n\t5\n[/records]\n"
	if got := a.decodeScriptForPath(encoded, "item.equ"); got != want {
		t.Fatalf("offset rendering = %q, want %q", got, want)
	}
}

func TestScriptRenderingSupportsLegacyUnpairedSection(t *testing.T) {
	a := New()
	encoded, err := a.encodeScript("[basis of rarity dicision]\n99 1 2 3 4 5 6 7 8\n[next]")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := rendering.Parse([]byte(`{
  "version": 1,
  "rules": [{
    "id": "etc.basis-of-rarity-dicision",
    "match": {},
    "target": {"kind": "section", "section": "basis of rarity dicision"},
    "format": {"tokensPerLine": 7, "offset": 1}
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	a.SetScriptRenderer(engine)
	want := "[basis of rarity dicision]\n\t99\n\t1\t2\t3\t4\t5\t6\t7\n\t8\n\n[next]\n"
	if got := a.decodeScriptForPath(encoded, "etc/itemdropinfo_monster_hell.etc"); got != want {
		t.Fatalf("unpaired section rendering = %q, want %q", got, want)
	}
}

func TestScriptRenderingUsesTokenValueForLegacyUnpairedSection(t *testing.T) {
	a := New()
	encoded, err := a.encodeScript("[basis of rarity dicision]\n3 100 200 300 400 500\n[next]")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := rendering.Parse([]byte(`{
  "version": 1,
  "rules": [{
    "id": "etc.basis-of-rarity-dicision-dynamic",
    "match": {},
    "target": {"kind": "section", "section": "basis of rarity dicision"},
    "format": {"offset": 1, "tokensPerLine": 1, "tokensPerLineIndex": 0}
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	a.SetScriptRenderer(engine)
	want := "[basis of rarity dicision]\n\t3\n\t100\t200\t300\n\t400\t500\n\n[next]\n"
	if got := a.decodeScriptForPath(encoded, "etc/itemdropinfo_monster_hell.etc"); got != want {
		t.Fatalf("dynamic unpaired section rendering = %q, want %q", got, want)
	}
}

func TestScriptRenderingUsesTokenValueForTokensPerLine(t *testing.T) {
	a := New()
	encoded, err := a.encodeScript("[records]\n2 100 200 300 400 500\n[/records]")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := rendering.Parse([]byte(`{
  "version": 1,
  "rules": [{
    "id": "records.dynamic",
    "target": {"kind": "section", "section": "records"},
    "format": {
      "offset": 1,
      "tokensPerLine": 1,
      "tokensPerLineIndex": 0
    }
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	a.SetScriptRenderer(engine)
	want := "[records]\n\t2\n\t100\t200\n\t300\t400\n\t500\n[/records]\n"
	if got := a.decodeScriptForPath(encoded, "item.equ"); got != want {
		t.Fatalf("dynamic tokensPerLine rendering = %q, want %q", got, want)
	}
}

func TestScriptRenderingStandaloneValuesResetGrouping(t *testing.T) {
	a := New()
	engine, err := rendering.Parse([]byte(`{
  "version": 1,
  "rules": [{
    "id": "world.drop",
    "target": {"kind": "section", "section": "world drop"},
    "format": {"tokensPerLine": 2, "standaloneValues": [-1]}
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	a.SetScriptRenderer(engine)
	for _, tt := range []struct {
		name, input, want string
	}{
		{"partial row", "[world drop]\n100 200 300 -1 400 500 -1 600 700\n[next]", "[world drop]\n\t100\t200\n\t300\n\t-1\n\t400\t500\n\t-1\n\t600\t700\n\n[next]\n"},
		{"leading and consecutive", "[world drop]\n-1 -1 1 2 -1 3\n[next]", "[world drop]\n\t-1\n\t-1\n\t1\t2\n\t-1\n\t3\n\n[next]\n"},
		{"no sentinel", "[world drop]\n1 2 3\n[next]", "[world drop]\n\t1\t2\n\t3\n\n[next]\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := a.encodeScript(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got := a.decodeScriptForPath(encoded, "item.equ"); got != tt.want {
				t.Fatalf("rendering = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScriptRenderingIndependentDropWithOptionalList(t *testing.T) {
	a := New()
	engine, err := rendering.Parse([]byte(`{
  "version": 1,
  "rules": [
    {
      "id": "independent.drop",
      "target": {"kind": "section", "section": "independent drop"},
      "format": {"tokensPerLine": 17, "nestedSections": ["list"]}
    },
    {
      "id": "list",
      "target": {"kind": "section", "section": "list"},
      "format": {"tokensPerLine": 2}
    }
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	a.SetScriptRenderer(engine)

	input := "[independent drop]\n" +
		"1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17\n" +
		"[list]\n100 200 300 400\n[/list]\n" +
		"18 19 20 21 22 23 24 25 26 27 28 29 30 31 32 33 34\n" +
		"35 36 37 38 39 40 41 42 43 44 45 46 47 48 49 50 51\n" +
		"[next]"
	want := "[independent drop]\n" +
		"\t1\t2\t3\t4\t5\t6\t7\t8\t9\t10\t11\t12\t13\t14\t15\t16\t17\n" +
		"\t[list]\n\t\t100\t200\n\t\t300\t400\n\t[/list]\n" +
		"\t18\t19\t20\t21\t22\t23\t24\t25\t26\t27\t28\t29\t30\t31\t32\t33\t34\n" +
		"\t35\t36\t37\t38\t39\t40\t41\t42\t43\t44\t45\t46\t47\t48\t49\t50\t51\n\n" +
		"[next]\n"
	encoded, err := a.encodeScript(input)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.decodeScriptForPath(encoded, "item.equ"); got != want {
		t.Fatalf("rendering = %q, want %q", got, want)
	}
}

func TestCanonicalTextIgnoresUserRenderingOverride(t *testing.T) {
	a := New()
	index, err := a.AddFileText("skills/test.skl", "[records]\n1 2 3 4 5\n[/records]", TypeScript)
	if err != nil {
		t.Fatalf("AddFileText() error = %v", err)
	}
	canonical, err := a.CanonicalText(index)
	if err != nil {
		t.Fatalf("CanonicalText() error = %v", err)
	}
	engine, err := rendering.Parse([]byte(`{
  "version": 1,
  "rules": [{
    "id": "skl.records",
    "match": {"extensions": [".skl"]},
    "target": {"kind": "section", "section": "records"},
    "format": {"tokensPerLine": 2}
  }]
}`))
	if err != nil {
		t.Fatalf("rendering.Parse() error = %v", err)
	}
	a.SetScriptRenderer(engine)
	active, err := a.Text(index)
	if err != nil {
		t.Fatalf("Text() error = %v", err)
	}
	if active == canonical {
		t.Fatalf("custom renderer did not change active text: %q", active)
	}
	stable, err := a.CanonicalText(index)
	if err != nil {
		t.Fatalf("CanonicalText() after override error = %v", err)
	}
	if stable != canonical {
		t.Fatalf("canonical text changed after renderer override: got %q, want %q", stable, canonical)
	}

	reencoded, err := a.encodeScript(active)
	if err != nil {
		t.Fatalf("encodeScript(active) error = %v", err)
	}
	if !bytes.Equal(reencoded, encodedPayloadForTest(t, a, index)) {
		t.Fatalf("rendered text changed token payload")
	}
}

func encodedPayloadForTest(t *testing.T, a *Archive, index int32) []byte {
	t.Helper()
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), raw...)
}

func TestDropRateRenderingUsesClientGroupWidth(t *testing.T) {
	for _, tc := range []struct {
		profile formatProfile
		width   int
	}{
		{standardProfile, 7}, {alternateProfile, 7}, {twProfile, 6}, {paged100CNProfile, 8}, {paged110Profile, 9}, {paged115CNProfile, 9}, {recoveredProfile, 7},
	} {
		t.Run(tc.profile.id, func(t *testing.T) {
			a := New()
			a.format = tc.profile
			for _, path := range []string{"etc/itemdropinfo_monster_hell.etc", "etc/itemdropinfo_monseter.etc"} {
				hell := strings.Contains(path, "monster_hell")
				var values []string
				for i := 1; i <= tc.width*2; i++ {
					values = append(values, strconv.Itoa(i))
				}
				input := "[basis of rarity dicision]\n"
				if hell {
					input += "2\n"
				}
				input += strings.Join(values, " ") + "\n[next]\n99"
				raw, err := a.encodeScript(input)
				if err != nil {
					t.Fatal(err)
				}
				rendered := a.decodeScriptForPath(raw, path)
				want := "[basis of rarity dicision]\n"
				if hell {
					want += "\t2\n"
				}
				for group := 0; group < 2; group++ {
					want += "\t" + strings.Join(values[group*tc.width:(group+1)*tc.width], "\t") + "\n"
				}
				want += "\n[next]\n\t99"
				if rendered != want {
					t.Fatalf("%s: rendered %q, want %q", path, rendered, want)
				}
				back, err := a.encodeScript(rendered)
				if err != nil || !bytes.Equal(raw, back) {
					t.Fatal("rendering changed tokens")
				}
			}
		})
	}
}

func TestDropRateRenderingRealArchiveRows(t *testing.T) {
	path := os.Getenv("PVF_TESTFILE")
	if path == "" {
		t.Skip("PVF_TESTFILE not set")
	}
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	widths := map[string]int{"90US": 7, "90CN": 7, "TW": 6, "100CN": 8, "110US": 9}
	width, ok := widths[a.ClientVersion()]
	if !ok {
		t.Skip("known drop-rate client required")
	}
	for _, spec := range []struct {
		path           string
		groups, offset int
	}{
		{"etc/itemdropinfo_monster_hell.etc", 2, 1}, {"etc/itemdropinfo_clearreward.etc", 1, 0},
		{"etc/itemdropinfo_monseter.etc", 4, 0}, {"etc/itemdropinfo_monseter_extra.etc", 4, 0},
	} {
		index, ok := a.Find(spec.path)
		if !ok {
			t.Fatal("drop file missing")
		}
		text, err := a.Text(index)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(text, "\n")
		inside := false
		rows := 0
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "[basis of rarity dicision]" {
				inside = true
				continue
			}
			if !inside {
				continue
			}
			if strings.HasPrefix(line, "[") {
				break
			}
			if line == "" {
				continue
			}
			want := width
			if rows < spec.offset {
				want = 1
			}
			if count := len(strings.Fields(line)); count != want {
				t.Fatalf("%s %s row %d width=%d want=%d", a.ClientVersion(), spec.path, rows, count, want)
			}
			rows++
		}
		if rows != spec.groups+spec.offset {
			t.Fatalf("%s unexpected row count=%d", spec.path, rows)
		}
		raw, err := a.RawBytes(index)
		if err != nil {
			t.Fatal(err)
		}
		back, err := a.encodeScript(text)
		if err != nil {
			t.Fatal(err)
		}
		// Legacy archives can have duplicate string-table entries: re-encoding
		// may choose an equivalent offset, so compare the numeric drop section.
		originalDoc, err := a.ParseScriptDocument(raw)
		if err != nil {
			t.Fatal(err)
		}
		renderedDoc, err := a.ParseScriptDocument(back)
		if err != nil {
			t.Fatal(err)
		}
		originalSection, ok := originalDoc.Section([]string{"basis of rarity dicision"}, 0)
		if !ok {
			t.Fatal("original drop section missing")
		}
		renderedSection, ok := renderedDoc.Section([]string{"basis of rarity dicision"}, 0)
		if !ok {
			t.Fatal("rendered drop section missing")
		}
		originalValues, renderedValues := originalSection.Values(), renderedSection.Values()
		if len(originalValues) != len(renderedValues) {
			t.Fatal("rendered drop section length changed")
		}
		for i, value := range originalValues {
			if value.Type != renderedValues[i].Type || value.Value != renderedValues[i].Value {
				t.Fatalf("%s drop value %d changed after rendering", spec.path, i)
			}
		}
	}
}

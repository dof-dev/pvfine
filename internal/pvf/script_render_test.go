package pvf

import (
	"bytes"
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

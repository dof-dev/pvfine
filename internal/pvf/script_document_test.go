package pvf

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestScriptDocumentRoundTripPreservesRawTokens(t *testing.T) {
	a := New()
	index, err := a.AddFileText("test/item.equ", "[parent]\n1\n[child]\n`hello`\n[/child]\n2\n[/parent]\n[flat]\n{5=`block`}\n{7=9}", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	document, err := a.ParseScriptDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := a.EncodeScriptDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded, raw) {
		t.Fatalf("parse -> encode changed raw payload:\n got: % X\nwant: % X", reencoded, raw)
	}

	children := document.Sections([]string{"parent", "child"})
	if len(children) != 1 {
		t.Fatalf("nested sections = %d, want 1", len(children))
	}
	value, ok := children[0].GetValue(0)
	if !ok || value.Type != ScriptTokenQuoted || value.Value != "hello" {
		t.Fatalf("nested value = %#v, ok=%v", value, ok)
	}
	flat, ok := document.Section([]string{"flat"}, 0)
	if !ok || !flatHasSection(flat) {
		t.Fatal("unpaired section was not parsed")
	}
	flatValues := flat.Values()
	if len(flatValues) != 2 || flatValues[0].Type != ScriptTokenBlock5 || flatValues[1].Type != ScriptTokenBlock7 {
		t.Fatalf("block token types = %#v", flatValues)
	}
	if flatValues[1].Value != int64(9) {
		t.Fatalf("numeric block value = %#v", flatValues[1].Value)
	}
}

func TestScriptDocumentRecoversOrphanClosingTag(t *testing.T) {
	a := New()
	index, err := a.AddFileText("test/malformed.equ", "[parent]\n1\n[/orphan]\n2\n[/parent]", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	document, err := a.ParseScriptDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	warnings := document.Warnings()
	if len(warnings) != 1 || warnings[0].Code != ScriptWarningOrphanClose {
		t.Fatalf("parse warnings = %#v", warnings)
	}
	if warnings[0].TokenIndex != 2 || len(warnings[0].SectionPath) != 1 || warnings[0].SectionPath[0] != "parent" {
		t.Fatalf("orphan warning location = %#v", warnings[0])
	}
	parent, ok := document.Section([]string{"parent"}, 0)
	if !ok {
		t.Fatal("parent section is missing")
	}
	values := parent.Values()
	if len(values) != 2 || values[0].Value != int64(1) || values[1].Value != int64(2) {
		t.Fatalf("parent values = %#v", values)
	}
	reencoded, err := a.EncodeScriptDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded, raw) {
		t.Fatalf("recovered parse -> encode changed raw payload:\n got: % X\nwant: % X", reencoded, raw)
	}
}

func TestScriptDocumentRecoversMissingAndMismatchedClosingTags(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		code       ScriptParseWarningCode
		path       []string
		wantHasEnd bool
	}{
		{
			name:       "missing close at eof",
			text:       "[outer]\n1\n[outer]\n2\n[/outer]",
			code:       ScriptWarningMissingClose,
			path:       []string{"outer"},
			wantHasEnd: false,
		},
		{
			name:       "inner close is skipped",
			text:       "[outer]\n[inner]\n1\n[/outer]\n[inner]\n2\n[/inner]",
			code:       ScriptWarningMismatchedClose,
			path:       []string{"outer", "inner"},
			wantHasEnd: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := New()
			index, err := a.AddFileText("test/malformed.equ", test.text, TypeScript)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := a.RawBytes(index)
			if err != nil {
				t.Fatal(err)
			}
			document, err := a.ParseScriptDocument(raw)
			if err != nil {
				t.Fatal(err)
			}
			warnings := document.Warnings()
			if len(warnings) != 1 || warnings[0].Code != test.code {
				t.Fatalf("parse warnings = %#v", warnings)
			}
			if !scriptPathEqual(warnings[0].SectionPath, test.path) {
				t.Fatalf("warning path = %#v, want %#v", warnings[0].SectionPath, test.path)
			}
			section, ok := document.Section(test.path, 0)
			if !ok {
				t.Fatalf("section %v is missing", test.path)
			}
			if section.HasEndTag() != test.wantHasEnd {
				t.Fatalf("section hasEndTag = %v, want %v", section.HasEndTag(), test.wantHasEnd)
			}
			reencoded, err := a.EncodeScriptDocument(document)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(reencoded, raw) {
				t.Fatalf("recovered parse -> encode changed raw payload:\n got: % X\nwant: % X", reencoded, raw)
			}
		})
	}
}

func TestScriptDocumentExactStringPoolEncoding(t *testing.T) {
	a := New()
	// Seed the UTF-8 pool with the same value so choosing utf16 must not
	// silently reuse the other pool's offset.
	_ = a.StringOffset("same")
	value, err := NewScriptValue(ScriptTokenQuoted, "same", ScriptPoolUTF16)
	if err != nil {
		t.Fatal(err)
	}
	document := &ScriptDocument{}
	if _, err := document.AppendSection("value", []ScriptValue{value}, true); err != nil {
		t.Fatal(err)
	}
	raw, err := a.EncodeScriptDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 15 {
		t.Fatalf("encoded token count bytes = %d, want 15", len(raw))
	}
	valueOffset := int32(binary.LittleEndian.Uint32(raw[6:]))
	if valueOffset&1 != 1 {
		t.Fatalf("quoted token did not use utf16 pool: offset=%d", valueOffset)
	}
}

func TestScriptDocumentAllTokenTypes(t *testing.T) {
	a := New()
	index, err := a.AddFileText("test/all.equ", "[all]\n1 1.5 bare `quoted` {5=`block`} {7=7}", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	document, err := a.ParseScriptDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	section, ok := document.Section([]string{"all"}, 0)
	if !ok {
		t.Fatal("all section is missing")
	}
	values := section.Values()
	want := []ScriptTokenType{
		ScriptTokenInteger, ScriptTokenFloat, ScriptTokenString,
		ScriptTokenQuoted, ScriptTokenBlock5, ScriptTokenBlock7,
	}
	if len(values) != len(want) {
		t.Fatalf("values = %#v", values)
	}
	for index, kind := range want {
		if values[index].Type != kind {
			t.Fatalf("value %d type = %s, want %s", index, values[index].Type, kind)
		}
	}
	if values[1].Value != float64(1.5) || values[4].Value != "block" || values[5].Value != int64(7) {
		t.Fatalf("token values = %#v", values)
	}
}

func flatHasSection(section *ScriptSection, want ...ScriptTokenType) bool {
	if section == nil {
		return false
	}
	values := section.Values()
	if len(want) > 0 && len(values) != len(want) {
		return false
	}
	for index, kind := range want {
		if values[index].Type != kind {
			return false
		}
	}
	return true
}

func TestScriptDocumentDuplicateSelectionAndMutation(t *testing.T) {
	a := New()
	index, err := a.AddFileText("test/item.equ", "[rarity]\n3\n[rarity]\n4\n[parent]\n[child]\n1\n[/child]\n[/parent]", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	document, err := a.ParseScriptDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	rarities := document.Sections([]string{"rarity"})
	if len(rarities) != 2 || rarities[0].Occurrence() != 0 || rarities[1].Occurrence() != 1 {
		t.Fatalf("rarity sections = %#v", rarities)
	}

	integer, err := NewScriptValue(ScriptTokenInteger, int64(9), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := rarities[1].Set(integer, 0); err != nil {
		t.Fatal(err)
	}
	if err := rarities[0].Append(integer); err != nil {
		t.Fatal(err)
	}
	if changed, err := document.Delete([]string{"parent", "child"}, 0); err != nil || !changed {
		t.Fatalf("delete child changed=%v err=%v", changed, err)
	}
	if len(document.Sections([]string{"parent", "child"})) != 0 {
		t.Fatal("deleted child is still selectable")
	}
	if _, err := document.AppendSection("created", []ScriptValue{integer}, true); err != nil {
		t.Fatal(err)
	}

	encoded, err := a.EncodeScriptDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetRawBytes(index, encoded); err != nil {
		t.Fatal(err)
	}
	text, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"[rarity]", "9", "[created]", "[/created]"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("mutated text %q does not contain %q", text, fragment)
		}
	}
}

func TestScriptDocumentSetCanCreatePath(t *testing.T) {
	document := &ScriptDocument{}
	value, err := NewScriptValue(ScriptTokenQuoted, "created", ScriptPoolUTF8)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := document.Set([]string{"parent", "child"}, 0, 0, value, true, true)
	if err != nil || !changed {
		t.Fatalf("set create changed=%v err=%v", changed, err)
	}
	section, ok := document.Section([]string{"parent", "child"}, 0)
	if !ok || !section.HasEndTag() {
		t.Fatalf("created section = %#v ok=%v", section, ok)
	}
	created, ok := section.GetValue(0)
	if !ok || created.Value != "created" {
		t.Fatalf("created value = %#v ok=%v", created, ok)
	}
}

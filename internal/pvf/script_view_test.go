package pvf

import (
	"strings"
	"testing"
)

func TestParseScriptViewFlatAndDuplicateSections(t *testing.T) {
	view := ParseScriptView("[rarity]\n3\n[name]\n`first`\n[rarity]\n4")
	var sections, rarityTokens int
	for _, element := range view.Elements {
		if element.Kind == ScriptElementSection {
			sections++
		}
		if element.Kind == ScriptElementToken && element.Section == "rarity" {
			rarityTokens++
			if element.Index != 0 {
				t.Fatalf("rarity token index = %d, want 0", element.Index)
			}
		}
	}
	if sections != 3 || rarityTokens != 2 {
		t.Fatalf("sections=%d rarityTokens=%d", sections, rarityTokens)
	}
}

func TestParseScriptViewNestedDirectTokens(t *testing.T) {
	view := ParseScriptView("[parent]\n1\n[child]\n2\n[/child]\n3\n[/parent]")
	got := make([]string, 0)
	for _, element := range view.Elements {
		if element.Kind == ScriptElementToken {
			got = append(got, element.Section+":"+element.Value)
		}
	}
	want := []string{"parent:1", "child:2", "parent:3"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tokens = %#v, want %#v", got, want)
		}
	}
}

func TestParseScriptViewUsesUTF16Offsets(t *testing.T) {
	view := ParseScriptView("[name]\n`😀中`")
	for _, element := range view.Elements {
		if element.Kind != ScriptElementToken {
			continue
		}
		if element.Start != 7 || element.End != 12 {
			t.Fatalf("range = [%d,%d), want [7,12)", element.Start, element.End)
		}
		return
	}
	t.Fatal("name token not found")
}

func TestParseScriptViewNormalizesLineEndingOffsets(t *testing.T) {
	text := "[name]\n`line1\r\nline2`\r[rarity]\r\n3"
	normalized := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
	view := ParseScriptView(text)

	var section, token *ScriptElement
	for i := range view.Elements {
		element := &view.Elements[i]
		if element.Kind == ScriptElementSection && element.Section == "rarity" {
			section = element
		}
		if element.Kind == ScriptElementToken && element.Section == "rarity" {
			token = element
		}
	}
	if section == nil || token == nil {
		t.Fatalf("rarity elements not found: %#v", view.Elements)
	}

	wantSectionStart := strings.Index(normalized, "[rarity]")
	wantSectionEnd := wantSectionStart + len("[rarity]")
	wantTokenStart := strings.LastIndex(normalized, "3")
	if section.Start != wantSectionStart || section.End != wantSectionEnd {
		t.Fatalf("section range = [%d,%d), want [%d,%d)", section.Start, section.End, wantSectionStart, wantSectionEnd)
	}
	if token.Start != wantTokenStart || token.End != wantTokenStart+1 {
		t.Fatalf("token range = [%d,%d), want [%d,%d)", token.Start, token.End, wantTokenStart, wantTokenStart+1)
	}
}

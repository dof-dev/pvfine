package pvf

import "testing"

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

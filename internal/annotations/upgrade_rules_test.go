package annotations

import (
	"pvfine/internal/pvf"
	"strings"
	"testing"
)

const upgradeRuleRow = "2.0 0 0 0 0 0.6 0 0.828 20000 30000 2 3 1 0 3037 50 0.6 0.09 0.5 0.169 0.83 0.684 0 0.7866"

func TestDefaultUpgradeRulePositions(t *testing.T) {
	engine, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, prefix, kind string }{{"etc/upgrade.etc", "etc.upgrade", "normal"}, {"etc/amplifyupgrade.etc", "etc.amplify", "amplify"}} {
		text := "[table]\n[table type]\n`" + tc.kind + "` " + upgradeRuleRow + " " + upgradeRuleRow + " 999\n[/table]\n[table]\n[table type]\n`" + tc.kind + "` " + upgradeRuleRow + "\n[/table]\n[safe upgrade]\n0 1 94000 1000 2 10327281 40 124448 1 1 20000 5000 1 10327281 41 124448\n[/safe upgrade]"
		view := pvf.ParseScriptView(text)
		results := engine.Annotate(tc.path, view, func(relation, id string) (Reference, bool) {
			return Reference{ID: id, Name: "材料 " + id, FileIndex: 9}, relation == "物品"
		})
		checks := map[string]struct {
			value, title string
			count        int
		}{
			tc.prefix + ".table-type.8":   {"20000", "武器失败率", 3},
			tc.prefix + ".table-type.9":   {"30000", "防具失败率", 3},
			tc.prefix + ".table-type.10":  {"2", "降级", 3},
			tc.prefix + ".table-type.14":  {"3037", "材料 3037", 3},
			tc.prefix + ".table-type.15":  {"50", "消耗数量", 3},
			tc.prefix + ".safe-upgrade.5": {"10327281", "材料 10327281", 2},
		}
		for id, check := range checks {
			count := 0
			for _, r := range results {
				for _, ruleID := range r.RuleIDs {
					if ruleID == id {
						count++
						if text[r.Start:r.End] != check.value || r.Title != check.title {
							t.Fatalf("%s = %+v", id, r)
						}
					}
				}
			}
			if count != check.count {
				t.Fatalf("%s matches %d, want %d", id, count, check.count)
			}
		}
		for _, r := range engine.Annotate("etc/unrelated.etc", view, nil) {
			for _, id := range r.RuleIDs {
				if strings.HasPrefix(id, "etc.upgrade.") || strings.HasPrefix(id, "etc.amplify.") {
					t.Fatal("rule leaked to unrelated file")
				}
			}
		}
	}
}

func TestDefaultForgeAndLegacyRules(t *testing.T) {
	engine, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	text := "[table]\n2 1 10000 1 3 2 5000 1.11 999\n[/table]\n[item weights by grade]\n1 3326 1 2 3326 1 3 3326\n[/item weights by grade]\n[item weights by grade 100lv]\n96 3326 51\n[/item weights by grade 100lv]"
	results := engine.Annotate("etc/upgrade_separate.etc", pvf.ParseScriptView(text), nil)
	counts := map[string]int{}
	for _, r := range results {
		for _, id := range r.RuleIDs {
			counts[id]++
			if id == "etc.forge.table.2" && r.Title != "锻造成功率" {
				t.Fatal("wrong forge probability meaning")
			}
		}
	}
	for id, want := range map[string]int{"etc.forge.table.2": 2, "etc.forge.item-weights-by-grade.1": 2, "etc.forge.item-weights-by-grade-100lv.1": 1} {
		if counts[id] != want {
			t.Fatalf("%s matches %d, want %d", id, counts[id], want)
		}
	}
	legacy := "[table]\n2 0 0 0 0 0 12500 2 3 3037 10 1 0 0 0 0 0\n[/table]"
	results = engine.Annotate("etc/upgrade.etc", pvf.ParseScriptView(legacy), nil)
	found := false
	for _, r := range results {
		for _, id := range r.RuleIDs {
			if id == "etc.upgrade.table.6" {
				found = true
				if legacy[r.Start:r.End] != "12500" {
					t.Fatal("legacy position changed")
				}
			}
		}
	}
	if !found {
		t.Fatal("missing legacy annotation")
	}
}

func Test115CNForgeRulePositions(t *testing.T) {
	engine, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	text := "[table]\n2 1 2.6 1 10000 1 3 2 3.6 2 5000 1.11\n[/table]"
	results := engine.ForVersion("115CN").Annotate("etc/upgrade_separate.etc", pvf.ParseScriptView(text), nil)
	probabilities := 0
	for _, result := range results {
		for _, id := range result.RuleIDs {
			if strings.HasPrefix(id, "etc.forge.table.") {
				t.Fatal("legacy four-column annotation leaked into 115CN")
			}
			if id == "etc.forge.115cn.table.4" {
				value := text[result.Start:result.End]
				if value != "10000" && value != "5000" {
					t.Fatal("probability annotation points at a coefficient")
				}
				probabilities++
			}
		}
	}
	if probabilities != 2 {
		t.Fatal("six-column probability annotations missing")
	}
	for _, result := range engine.ForVersion("115US").Annotate("etc/upgrade_separate.etc", pvf.ParseScriptView(text), nil) {
		for _, id := range result.RuleIDs {
			if strings.HasPrefix(id, "etc.forge.115cn.") {
				t.Fatal("115CN annotations leaked into 115US")
			}
		}
	}
}

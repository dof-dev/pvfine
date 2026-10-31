package script

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

const scriptUpgradeRow = "2.0 0 0 0 0 0.6 0 0.828 90000 88000 2 3 3 0 3037 10 0.3 0.018 0.1 0.033 0.17 0.228 0 0.2622"

func TestScriptUpgradeFunctionsAndAtomicValidation(t *testing.T) {
	for _, test := range []struct {
		name, source string
		failed       bool
	}{
		{"range", `const f=pvf.find("etc/upgrade.etc"); if(f.editUpgrade({equipment:"armor",fromLevel:2,toLevel:3,successRate:level=>100-level*5,costCount:(level,current)=>current+level*2})!==2)throw Error("count");`, false},
		{"invalid late callback", `const f=pvf.find("etc/upgrade.etc");const before=f.text();let rejected=false;try{f.editUpgrade({successRate:level=>level===3?101:50,costCount:level=>level*20});}catch(e){rejected=true;}if(!rejected||f.text()!==before)throw Error("atomic validation failed");`, false},
		{"fraction count", `pvf.find("etc/upgrade.etc").editUpgrade({costCount:level=>level/2});`, true},
		{"string result", `pvf.find("etc/upgrade.etc").editUpgrade({successRate:()=>"50"});`, true},
		{"bad range", `pvf.find("etc/upgrade.etc").editUpgrade({fromLevel:0,successRate:50});`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := pvf.New()
			defer a.Release()
			text := "[name]\n`😀`\n[table]\n[table type]\n`event` 1 2 3\n[/table]\n[table]\n[table type]\n`normal` " + strings.Repeat(scriptUpgradeRow+"\n", 3) + "[/table]\n[table]\n[table type]\n`amplify` " + scriptUpgradeRow + "\n[/table]"
			index, err := a.AddFileText("etc/upgrade.etc", text, pvf.TypeScript)
			if err != nil {
				t.Fatal(err)
			}
			tx := NewTransaction(a)
			defer tx.Rollback()
			host := NewBatchAPI(context.Background(), tx, nil, nil)
			before, err := tx.Stage().Text(index)
			if err != nil {
				t.Fatal(err)
			}
			result, err := NewGojaRuntime().Run(context.Background(), test.source, host)
			if err != nil {
				t.Fatal(err)
			}
			if test.failed {
				if result.Status != RunStatusFailed {
					t.Fatalf("expected failure: %+v", result)
				}
			} else if result.Status != RunStatusCompleted {
				t.Fatalf("run: %+v", result)
			}
			after, _ := tx.Stage().Text(index)
			if test.name != "range" {
				if after != before {
					t.Fatal("invalid batch changed file")
				}
				return
			}
			tokens, _, err := pvf.SelectUpgradeTableTokens(after, "normal")
			if err != nil {
				t.Fatal(err)
			}
			for level := 1; level <= 3; level++ {
				row := tokens[(level-1)*24 : level*24]
				if row[8].Value != "90000" {
					t.Fatal("weapon modified")
				}
				wantRate, wantCount := 88000, 10
				if level >= 2 {
					wantRate, wantCount = level*5000, 10+level*2
				}
				if row[9].Value != strconv.Itoa(wantRate) || row[15].Value != strconv.Itoa(wantCount) {
					t.Fatalf("level %d: %+v", level, row)
				}
			}
			original, _ := a.Text(index)
			if original != before {
				t.Fatal("script changed base archive")
			}
			unchanged, _, _ := pvf.SelectUpgradeTableTokens(after, "amplify")
			if unchanged[9].Value != "88000" || unchanged[15].Value != "10" {
				t.Fatal("wrong table changed")
			}
		})
	}
}

func TestUpgradeSourceOffsets(t *testing.T) {
	text := "`😀`\r\n123\r456"
	offsets := upgradeByteOffsets(text)
	for _, e := range pvf.ParseScriptView(text).Elements {
		if e.TokenType == 0 {
			if text[offsets[e.Start]:offsets[e.End]] != e.Value {
				t.Fatal("invalid CRLF/UTF-16 positions")
			}
		}
	}
}

const scriptUpgrade19Row = "2.2 0.018 0.1 0.033 0.2 0.6 0 10000 2 1 3037 10 1 0.018 0.1 0.033 0.17 0.228 0"

func TestScriptUpgrade19Columns(t *testing.T) {
	a := pvf.New()
	defer a.Release()
	text := "[table]\n[table type]\n`normal`\n" + strings.Repeat(scriptUpgrade19Row+"\n", 5) + "[/table]"
	index, err := a.AddFileText("etc/upgrade.etc", text, pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	tx := NewTransaction(a)
	defer tx.Rollback()
	host := NewBatchAPI(context.Background(), tx, nil, nil)

	// equipment: armor 应报错
	res, err := NewGojaRuntime().Run(context.Background(), `pvf.find("etc/upgrade.etc").editUpgrade({equipment:"armor",successRate:100});`, host)
	if err != nil || res.Status != RunStatusFailed {
		t.Fatalf("expected armor edit to fail on 19 column table: %+v", res)
	}

	// 成功率与数量修改
	res, err = NewGojaRuntime().Run(context.Background(), `pvf.find("etc/upgrade.etc").editUpgrade({fromLevel:1,toLevel:2,successRate:100,costCount:88});`, host)
	if err != nil || res.Status != RunStatusCompleted {
		t.Fatalf("run failed: %+v %v", res, err)
	}
	after, err := tx.Stage().Text(index)
	if err != nil {
		t.Fatal(err)
	}
	tokens, _, err := pvf.SelectUpgradeTableTokens(after, "normal")
	if err != nil {
		t.Fatal(err)
	}
	// Level 1: 失败概率在 offset+7 应为 0 (100% 成功率), 数量在 offset+11 应为 88
	if tokens[7].Value != "0" || tokens[11].Value != "88" {
		t.Fatalf("level 1 not updated correctly: tokens[7]=%s, tokens[11]=%s", tokens[7].Value, tokens[11].Value)
	}
	// Level 2: 同样被更新
	if tokens[19+7].Value != "0" || tokens[19+11].Value != "88" {
		t.Fatalf("level 2 not updated correctly: tokens[26]=%s, tokens[30]=%s", tokens[19+7].Value, tokens[19+11].Value)
	}
	// Level 3: 保持未变 (原本失败率 10000, 数量 10)
	if tokens[38+7].Value != "10000" || tokens[38+11].Value != "10" {
		t.Fatalf("level 3 should be unchanged: tokens[45]=%s, tokens[49]=%s", tokens[38+7].Value, tokens[38+11].Value)
	}
}

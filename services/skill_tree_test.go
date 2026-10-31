package services

import (
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"pvfine/internal/pvf"
)

func TestSkillTreeMixedCoordinatesAndEdges(t *testing.T) {
	doc := parseSkillTree("[character job]\n`[swordman]` `none`\n[skill info]\n[index]\n5\n[cell pos]\n2 3\n[next skill]\n7\n[next skill]\n8\n[/skill info]\n[skill info]\n[index]\n7\n[icon pos]\n23 67\n[/skill info]\n[/character job]\n[character job]\n`[swordman]` `weaponmaster`\n[skill info]\n[index]\n8\n[cell pos]\n0 0\n[/skill info]\n[/character job]")
	if len(doc.Branches) != 2 || len(doc.Branches[0].Nodes) != 2 || len(doc.Branches[1].Nodes) != 1 {
		t.Fatalf("分支解析错误：%+v", doc)
	}
	n := doc.Branches[0].Nodes[0]
	if n.X != 94 || n.Y != 201 || !reflect.DeepEqual(n.Next, []int{7, 8}) {
		t.Fatalf("坐标或顺序错误：%+v", n)
	}
	half := doc.Branches[0].Nodes[1]
	if len(n.PositionTokens) != 2 || n.PositionTokens[0].Value != 2 || n.PositionTokens[1].Value != 3 || n.SourceStart <= 0 {
		t.Fatalf("缺少可编辑坐标位置：%+v", n)
	}
	if half.X != 23 || half.Y != 67 || half.PositionKind != "icon pos" {
		t.Fatalf("半格坐标错误：%+v", half)
	}
}

func TestCommonSkillsJobExclusions(t *testing.T) {
	text := "[common skill]\n169 190 197\n[/common skill]\n[except skill info]\n[info]\n`[creator mage]` 197\n[/info]\n[info]\n`[swordman]` 169 190\n[/info]\n[info]\n`[creator mage]` 190\n[/info]\n[/except skill info]"
	if got := parseCommonSkillIDs(text, "[creator mage]"); !reflect.DeepEqual(got, []int{169}) {
		t.Fatal(got)
	}
	if got := parseCommonSkillIDs(text, "[swordman]"); !reflect.DeepEqual(got, []int{197}) {
		t.Fatal(got)
	}
	if got := parseCommonSkillIDs(text, "[mage]"); !reflect.DeepEqual(got, []int{169, 190, 197}) {
		t.Fatal(got)
	}
}

func TestSkillTreeInvalidPositions(t *testing.T) {
	doc := parseSkillTree("[character job]\n`[mage]` `none`\n[skill info]\n[index]\n1\n[/skill info]\n[skill info]\n[index]\n2\n[cell pos]\n-1 2\n[/skill info]\n[/character job]")
	if len(doc.Issues) != 2 || len(doc.Branches[0].Nodes) != 0 {
		t.Fatalf("无效坐标未提示：%+v", doc)
	}
}

func TestSkillTreeReadJobScopedAndOverlay(t *testing.T) {
	a := pvf.New()
	defer a.Release()
	files := map[string]string{
		"clientonly/skillshoptreespindex.co": "[skill tree]\n`[swordman]` `SkillTree/Test.co` `[mage]` `SkillTree/Test.co`\n[/skill tree]",
		"clientonly/skilltree/test.co":       "[character job]\n`[swordman]` `none`\n[skill info]\n[index]\n169\n[cell pos]\n1 2\n[/skill info]\n[/character job]\n[character job]\n`[mage]` `none`\n[skill info]\n[index]\n169\n[icon pos]\n23 0\n[/skill info]\n[/character job]",
		"clientonly/commonskilllist.co":      "[common skill]\n169\n[/common skill]",
		"skill/skilllist.lst":                "0 `SwordmanSkill.lst` 3 `MageSkill.lst`",
		"skill/swordmanskill.lst":            "169 `Swordman/BackStep.skl` 7 `Swordman/New.skl`",
		"skill/swordman/new.skl":             "[name]\n`新增技能`",
		"skill/swordman/ignored.skl":         "[name]\n`未注册技能`",
		"skill/mageskill.lst":                "169 `Mage/BackStep.skl`",
		"skill/swordman/backstep.skl":        "[name]\n`剑士后跳`\n[required level]\n0\n[explain]\n`向后跳跃\\n回避攻击，成功率100%%`",
		"skill/mage/backstep.skl":            "[name]\n`法师后跳`\n[explain]\n{8=`<1::description>`}",
		"list/n_string.lst":                  "1 `String/SkillTree.uv.str`",
	}
	for p, text := range files {
		if _, e := a.AddFileText(p, text, pvf.TypeScript); e != nil {
			t.Fatal(e)
		}
	}
	table := []byte{}
	for _, unit := range utf16.Encode([]rune("description>法师技能描述\\n第二行\r\n")) {
		table = append(table, byte(unit), byte(unit>>8))
	}
	a.AddFile("String/SkillTree.uv.str", table, pvf.TypeScript)
	s := NewFileGUIService(&core{archive: a})
	for job, count := range map[string]int{"[swordman]": 2, "[mage]": 1} {
		catalog, err := s.ListSkillTreeSkills(job)
		if err != nil || len(catalog) != count {
			t.Fatalf("职业技能列表错误：%s %+v %v", job, catalog, err)
		}
		expectedName := "剑士后跳"
		if job == "[mage]" {
			expectedName = "法师后跳"
		}
		for _, skill := range catalog {
			if skill.ID == 169 && skill.Name != expectedName {
				t.Fatal("同ID应按职业解析")
			}
		}
	}
	for job, name := range map[string]string{"[swordman]": "剑士后跳", "[mage]": "法师后跳"} {
		doc, e := s.ReadSkillTree(job, "sp")
		if e != nil {
			t.Fatal(e)
		}
		treeIndex, found := a.Find("clientonly/skilltree/test.co")
		if !found || doc.FileIndex != treeIndex || doc.Path != a.Path(treeIndex) {
			t.Fatalf("混合大小写注册路径应返回归档的实际文件身份：%d %s", doc.FileIndex, doc.Path)
		}
		if len(doc.Common) != 1 || doc.Common[0].Name != name || len(doc.Branches) != 1 || doc.Branches[0].Nodes[0].Name != name {
			t.Fatalf("技能必须按职业解析：%+v", doc)
		}
	}
	for job, want := range map[string]string{"[swordman]": "向后跳跃\n回避攻击，成功率100%", "[mage]": "法师技能描述\n第二行"} {
		doc, err := s.ReadSkillTree(job, "sp")
		if err != nil {
			t.Fatal(err)
		}
		if doc.Common[0].Explain != want || doc.Branches[0].Nodes[0].Explain != want {
			t.Fatalf("%s 描述应解析引用并保留换行：%q", job, doc.Common[0].Explain)
		}
	}
	treeIndex, _ := a.Find("clientonly/skilltree/test.co")
	before, err := a.Text(treeIndex)
	if err != nil {
		t.Fatal(err)
	}
	// Match the renderer's whitespace without assuming one particular style.
	view := pvf.ParseScriptView(before)
	var coordinates []pvf.ScriptElement
	for _, element := range view.Elements {
		if element.Kind == pvf.ScriptElementToken && element.Section == "cell pos" {
			coordinates = append(coordinates, element)
		}
	}
	if len(coordinates) != 2 {
		t.Fatal("缺少合成坐标")
	}
	units := utf16.Encode([]rune(before))
	units = append(append(append([]uint16{}, units[:coordinates[1].Start]...), uint16('1')), units[coordinates[1].End:]...)
	units = append(append(append([]uint16{}, units[:coordinates[0].Start]...), uint16('3')), units[coordinates[0].End:]...)
	draft := string(utf16.Decode(units))
	preview, err := s.ReadSkillTreeDraft("[swordman]", "sp", draft)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Text != draft || preview.Branches[0].Nodes[0].X != 141 || preview.Branches[0].Nodes[0].Y != 67 {
		t.Fatal("未读取拖拽后的草稿坐标")
	}
	after, err := a.Text(treeIndex)
	if err != nil || after != before {
		t.Fatal("读取草稿不应提前修改归档")
	}
	if err := a.SetText(treeIndex, draft); err != nil {
		t.Fatal(err)
	}
	saved, err := s.ReadSkillTree("[swordman]", "sp")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Branches[0].Nodes[0].X != 141 || saved.Branches[0].Nodes[0].Y != 67 {
		t.Fatal("应用草稿后坐标未持久化到覆盖层")
	}
	mageTree, err := s.ReadSkillTree("[mage]", "sp")
	if err != nil || mageTree.Branches[0].Nodes[0].X != 23 {
		t.Fatal("应用坐标不应影响其他职业分支")
	}
	swordman, err := s.ReadSkillTree("[swordman]", "sp")
	if err != nil {
		t.Fatal(err)
	}
	if swordman.Common[0].RequiredLevel == nil || *swordman.Common[0].RequiredLevel != 0 || swordman.Branches[0].Nodes[0].RequiredLevel == nil {
		t.Fatal("应保留等级0，不与缺失等级混淆")
	}
	mage, err := s.ReadSkillTree("[mage]", "sp")
	if err != nil {
		t.Fatal(err)
	}
	if mage.Common[0].RequiredLevel != nil {
		t.Fatal("缺失等级不应虚构为0")
	}
	if _, e := s.ReadSkillTree("[swordman]", "unknown"); e == nil {
		t.Fatal("不应读取未注册类型")
	}
	// Changes to skill names and learning levels must both follow the overlay.
	idx, _ := a.Find("skill/swordman/backstep.skl")
	if e := a.SetText(idx, "[name]\n`已更新`\n[required level]\n20\n[explain]\n`更新后的描述`"); e != nil {
		t.Fatal(e)
	}
	doc, e := s.ReadSkillTree("[swordman]", "sp")
	if e != nil {
		t.Fatal(e)
	}
	if doc.Common[0].Name != "已更新" || doc.Common[0].Explain != "更新后的描述" || doc.Common[0].RequiredLevel == nil || *doc.Common[0].RequiredLevel != 20 {
		t.Fatalf("未读取当前覆盖层：%s", doc.Common[0].Name)
	}
	branch := doc.Branches[0]
	if branch.InsertOffset < 0 {
		t.Fatal("缺少新增技能插入位置")
	}
	source := utf16.Encode([]rune(doc.Text))
	entry := utf16.Encode([]rune("\n[skill info]\n[index]\n7\n[cell pos]\n4 3\n[/skill info]\n"))
	inserted := string(utf16.Decode(append(append(append([]uint16{}, source[:branch.InsertOffset]...), entry...), source[branch.InsertOffset:]...)))
	added, err := s.ReadSkillTreeDraft("[swordman]", "sp", inserted)
	if err != nil || len(added.Branches[0].Nodes) != 2 {
		t.Fatal("新增技能草稿未读回", err)
	}
	if added.Branches[0].Nodes[1].ID != 7 || added.Branches[0].Nodes[1].Name != "新增技能" || added.Branches[0].Nodes[1].X != 188 {
		t.Fatal("新增技能解析错误")
	}
	if err := a.SetText(treeIndex, inserted); err != nil {
		t.Fatal(err)
	}
	added, err = s.ReadSkillTree("[swordman]", "sp")
	if err != nil || len(added.Branches[0].Nodes) != 2 {
		t.Fatal("新增技能未保存到覆盖层", err)
	}
}

func TestSkillTreeReal110US(t *testing.T) {
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
	registrations, err := s.ListSkillTrees()
	if err != nil {
		t.Fatal(err)
	}
	if len(registrations) != 34 {
		t.Fatalf("注册数：%d", len(registrations))
	}
	for _, r := range registrations {
		t.Run(r.Job+"/"+r.Kind, func(t *testing.T) {
			doc, e := s.ReadSkillTree(r.Job, r.Kind)
			if e != nil {
				t.Fatal(e)
			}
			index, found := a.Find(r.Path)
			if !found || doc.FileIndex != index || doc.Path != a.Path(index) {
				t.Fatalf("注册路径 %s 应关联实际文件索引和路径：%d %s", r.Path, doc.FileIndex, doc.Path)
			}
			if len(doc.Branches) == 0 {
				t.Fatal("缺少转职分支")
			}
			if r.Job == "[creator mage]" {
				for _, n := range doc.Common {
					if n.ID == 197 {
						t.Fatal("缔造者应排除197")
					}
				}
			}
			for _, b := range doc.Branches {
				if b.InsertOffset < 0 {
					t.Errorf("分支 %s 缺少新增技能插入位置", b.Name)
				}
				for _, n := range b.Nodes {
					if r.Job == "[swordman]" && r.Kind == "sp" && n.ID == 1 && (n.RequiredLevel == nil || *n.RequiredLevel != 5) {
						t.Errorf("格挡学习等级应为5：%v", n.RequiredLevel)
					}
					if len(n.PositionTokens) != 2 || n.SourceStart < 0 {
						t.Errorf("技能 #%d 缺少编辑位置", n.ID)
					}
					if n.SourceClose <= n.SourceStart || len(n.NextTokens) != len(n.Next) {
						t.Errorf("技能 #%d 缺少删除记录或关联的编辑位置", n.ID)
					}
					if n.FileIndex < 0 {
						t.Errorf("%s 技能 #%d 无法解析", b.Name, n.ID)
					}
				}
			}
			t.Logf("%d 分支，%d 通用技能，%d 条提示", len(doc.Branches), len(doc.Common), len(doc.Issues))
		})
	}
}

func TestSkillTreeDeletionOffsets(t *testing.T) {
	text := "// 😀\r\n[character job]\r\n`[swordman]` `none`\r\n[skill info]\r\n[index]\r\n7\r\n[cell pos]\r\n0 0\r\n// [/skill info]\r\n[/skill info]\r\n[skill info]\r\n[index]\r\n8\r\n[cell pos]\r\n0 0\r\n[next skill]\r\n7 9 7\r\n[/skill info]\r\n[/character job]"
	doc := parseSkillTree(text)
	nodes := doc.Branches[0].Nodes
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	units := utf16.Encode([]rune(normalized))
	for _, n := range nodes {
		if n.SourceClose <= n.SourceStart || string(utf16.Decode(units[n.SourceClose:n.SourceClose+13])) != "[/skill info]" {
			t.Fatalf("incorrect skill record range: %#v", n)
		}
	}
	if len(nodes[1].NextTokens) != 3 {
		t.Fatal("missing incoming link offsets")
	}
	for i, token := range nodes[1].NextTokens {
		if string(utf16.Decode(units[token.Start:token.End])) != strconv.Itoa(nodes[1].Next[i]) {
			t.Fatalf("incorrect next skill token: %#v", token)
		}
	}
	broken := parseSkillTree("[character job]\n`[swordman]` `none`\n[skill info]\n[index]\n7\n[cell pos]\n0 0")
	if broken.Branches[0].Nodes[0].SourceClose != -1 {
		t.Fatal("unclosed skill record should not be deletable")
	}
}

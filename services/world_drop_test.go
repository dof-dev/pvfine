package services

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func TestWorldDropParseAndRender(t *testing.T) {
	input := "[world drop]\n\t1\t0\n\t3176\t0\n\t3030\t17\n\t-1\n\t2\t0\n\t-1\n"
	levels, err := parseWorldDrop(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []WorldDropLevel{
		{Level: 1, Items: []WorldDropItem{{ID: 3176, Weight: 0}, {ID: 3030, Weight: 17}}},
		{Level: 2, Items: []WorldDropItem{}},
	}
	if !reflect.DeepEqual(levels, want) {
		t.Fatalf("levels = %#v", levels)
	}
	if got := renderWorldDrop(levels); got != input {
		t.Fatalf("rendered = %q", got)
	}
	if _, err := parseWorldDrop(renderWorldDrop([]WorldDropLevel{})); err != nil {
		t.Fatal(err)
	}
}

func TestWorldDropMalformed(t *testing.T) {
	for _, test := range []struct{ text, want string }{
		{"[world drop]\n1 0 100 2", "缺少结束标记"},
		{"[world drop]\n1 0 100", "缺少道具权重"},
		{"[world drop]\n1 1 -1", "固定值必须是 0"},
		{"[world drop]\n1 0 7 -1", "道具权重不能为负数"},
		{"[world drop]\n1 0 -1 1 0 -1", "重复"},
		{"[other]\n1 0 -1", "只允许"},
		{"[world drop]\n1 0 -1\n[other]", "只允许"},
		{"[world drop]\n1 0 -1\n[world drop]", "只能包含一个"},
	} {
		_, err := parseWorldDrop(test.text)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("parse %q: got %v, want %q", test.text, err, test.want)
		}
	}
}

func TestWorldDropUnknownLegacyIDs(t *testing.T) {
	c, service, index, _ := worldDropFixture(t)
	text := "[world drop]\n1 0 0 5 -2 7 -1\n2 0 -1\n"
	doc, err := service.ReadWorldDrop(index, text)
	if err != nil {
		t.Fatal(err)
	}
	items := doc.Levels[0].Items
	if len(items) != 2 || items[0].Name != "未知物品 #0" || items[1].Name != "未知物品 #-2" {
		t.Fatalf("legacy items = %#v", items)
	}
	doc.Levels[0].Items[0].Weight = 9
	request := WorldDropEditRequest{FileIndex: index, Path: worldDropPath, Text: text, Revision: c.batchRevision, Levels: doc.Levels}
	result, err := service.ApplyWorldDropEdit(request)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.ReadWorldDrop(index, result.Files[0].Text)
	if err != nil || got.Levels[0].Items[0].ID != 0 || got.Levels[0].Items[0].Weight != 9 || got.Levels[0].Items[1].ID != -2 {
		t.Fatalf("legacy round trip = %#v, %v", got, err)
	}
	request.Revision = result.Revision
	request.Text = result.Files[0].Text
	request.Levels = got.Levels
	request.Levels[0].Items = append(request.Levels[0].Items, WorldDropItem{ID: 0, Weight: 1})
	if _, err := service.ApplyWorldDropEdit(request); err == nil || !strings.Contains(err.Error(), "不能新增") {
		t.Fatalf("adding nonpositive ID: %v", err)
	}
}

func worldDropFixture(t *testing.T) (*core, *FileGUIService, int32, string) {
	return worldDropFixtureAtPath(t, worldDropPath)
}

func worldDropFixtureAtPath(t *testing.T, filePath string) (*core, *FileGUIService, int32, string) {
	t.Helper()
	a := pvf.New()
	for path, text := range map[string]string{
		"stackable/stackable.lst": "3176 `item.stk`",
		"stackable/item.stk":      "[name]\n`测试物品`",
	} {
		if _, err := a.AddFileText(path, text, pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	text := "[world drop]\n1 0 3176 0 -1\n2 0 -1\n"
	index, err := a.AddFileText(filePath, text, pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	return c, NewFileGUIService(c), index, text
}

func TestRegionalWorldDropReadAndApply(t *testing.T) {
	c, service, index, text := worldDropFixtureAtPath(t, regionalWorldDropPath)
	doc, err := service.ReadWorldDrop(index, text)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Levels) != 2 || doc.Levels[0].Items[0].Name != "测试物品" {
		t.Fatalf("document = %#v", doc)
	}
	doc.Levels[0].Items[0].Weight = 5
	result, err := service.ApplyWorldDropEdit(WorldDropEditRequest{
		FileIndex: index, Path: regionalWorldDropPath, Text: text, Revision: c.batchRevision, Levels: doc.Levels,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Files[0].Path != regionalWorldDropPath {
		t.Fatalf("result = %#v", result)
	}
	if _, err := service.ReadWorldDrop(index, result.Files[0].Text); err != nil {
		t.Fatal(err)
	}
}

func TestRegionalWorldDropReal90US(t *testing.T) {
	filename := os.Getenv("PVF_TESTFILE")
	if filename == "" {
		t.Skip("PVF_TESTFILE 未设置")
	}
	a, err := pvf.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	if a.ClientVersion() != "90US" {
		t.Skip("仅验证 90US 归档")
	}
	index, ok := a.Find(regionalWorldDropPath)
	if !ok {
		t.Fatal("缺少区域全局掉率文件")
	}
	text, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWorldDropFile(a, index, regionalWorldDropPath); err != nil {
		t.Fatal(err)
	}
	levels, err := parseWorldDrop(text)
	if err != nil || len(levels) == 0 {
		t.Fatalf("读取 90US 全局掉率失败：levels=%d, err=%v", len(levels), err)
	}
}

func TestWorldDropReadAndApply(t *testing.T) {
	c, service, index, text := worldDropFixture(t)
	doc, err := service.ReadWorldDrop(index, text)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Revision != c.batchRevision || len(doc.Levels) != 2 || doc.Levels[0].Items[0].Name != "测试物品" {
		t.Fatalf("document = %#v", doc)
	}
	before, err := c.archive.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	request := WorldDropEditRequest{FileIndex: index, Path: worldDropPath, Text: text, Revision: doc.Revision, Levels: []WorldDropLevel{
		{Level: 3, Items: []WorldDropItem{{ID: 3176, Weight: 20}, {ID: 9999, Weight: 0}}},
		{Level: 2, Items: []WorldDropItem{}},
	}}
	result, err := service.ApplyWorldDropEdit(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Revision <= doc.Revision || result.ModifiedCount == 0 {
		t.Fatalf("result = %#v", result)
	}
	after, err := c.archive.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(before, after) {
		t.Fatal("archive bytes unchanged")
	}
	got, err := service.ReadWorldDrop(index, result.Files[0].Text)
	if err != nil {
		t.Fatal(err)
	}
	if got.Levels[0].Level != 3 || got.Levels[0].Items[1].Weight != 0 || got.Levels[1].Level != 2 || len(got.Levels[1].Items) != 0 {
		t.Fatalf("round trip = %#v", got.Levels)
	}
	if _, err := service.ApplyWorldDropEdit(request); err == nil || !strings.Contains(err.Error(), "已变化") {
		t.Fatalf("stale request: %v", err)
	}
}

func TestWorldDropRejectedEditIsAtomic(t *testing.T) {
	c, service, index, text := worldDropFixture(t)
	before, _ := c.archive.RawBytes(index)
	modified := c.archive.ModifiedCount()
	base := WorldDropEditRequest{FileIndex: index, Path: worldDropPath, Text: text, Revision: c.batchRevision}
	for _, request := range []WorldDropEditRequest{
		{FileIndex: index, Path: "etc/other.etc", Text: text, Revision: base.Revision},
		{FileIndex: index, Path: worldDropPath, Text: "[world drop]\n1 0 123", Revision: base.Revision},
		{FileIndex: index, Path: worldDropPath, Text: text, Revision: base.Revision, Levels: []WorldDropLevel{{Level: 1}, {Level: 1}}},
		{FileIndex: index, Path: worldDropPath, Text: text, Revision: base.Revision, Levels: []WorldDropLevel{{Level: 1, Items: []WorldDropItem{{ID: 7, Weight: -1}}}}},
	} {
		if _, err := service.ApplyWorldDropEdit(request); err == nil {
			t.Fatalf("unexpected success: %#v", request)
		}
		after, _ := c.archive.RawBytes(index)
		if !reflect.DeepEqual(before, after) || c.archive.ModifiedCount() != modified {
			t.Fatal("rejected edit modified archive")
		}
	}
}

func TestWorldDropNoOpAndEditorDraft(t *testing.T) {
	c, service, index, text := worldDropFixture(t)
	before, _ := c.archive.RawBytes(index)
	doc, err := service.ReadWorldDrop(index, text)
	if err != nil {
		t.Fatal(err)
	}
	request := WorldDropEditRequest{FileIndex: index, Path: worldDropPath, Text: text, Revision: doc.Revision, Levels: doc.Levels}
	result, err := service.ApplyWorldDropEdit(request)
	if err != nil || len(result.Files) != 0 || result.Revision != doc.Revision {
		t.Fatalf("no-op = %#v, %v", result, err)
	}
	after, _ := c.archive.RawBytes(index)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("no-op changed source formatting")
	}
	// An unsaved DSL draft is the source of truth for a GUI edit.
	draft := "[world drop]\n1 0 3176 8 -1\n2 0 -1\n"
	request.Text = draft
	request.Levels = []WorldDropLevel{{Level: 1, Items: []WorldDropItem{{ID: 3176, Weight: 9}}}, {Level: 2, Items: []WorldDropItem{}}}
	result, err = service.ApplyWorldDropEdit(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Files[0].BeforeText != draft {
		t.Fatalf("draft not used: %#v", result.Files[0])
	}
}

package services

import (
	"bytes"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func itemCreationFixture(t *testing.T) (*core, *FileGUIService, ItemCreateRequest) {
	t.Helper()
	c, _ := shopFixture(t)
	if err := c.setArchive(c.archive); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	req := ItemCreateRequest{Path: "stackable/custom/new.stk", ID: "999", Name: "新物品😀", Text: "[name]\n`参考名称`\n[rarity]\n3\n[custom]\n42"}
	return c, NewFileGUIService(c), req
}
func checkedCreate(t *testing.T, s *FileGUIService, req ItemCreateRequest) *ItemCreateResult {
	t.Helper()
	check, err := s.CheckItemCreation(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Conflicts) > 0 {
		t.Fatal(check.Conflicts)
	}
	req.Revision = check.Revision
	result, err := s.CreateItem(req)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestItemCreationCopiesAndRegistersAtomically(t *testing.T) {
	c, s, req := itemCreationFixture(t)
	li, _ := c.archive.Find("stackable/stackable.lst")
	original, _ := c.archive.Text(li)
	ei, _ := c.archive.Find("equipment/equipment.lst")
	equBefore, _ := c.archive.RawBytes(ei)
	equBefore = append([]byte(nil), equBefore...)
	req.Drafts = []ShopDraft{{FileIndex: li, Path: c.archive.Path(li), Text: original + "\n998 `draft-only.stk`"}, {FileIndex: ei, Path: c.archive.Path(ei), Text: "4 `different.equ`"}}
	result := checkedCreate(t, s, req)
	text, err := c.archive.Text(result.FileIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "新物品😀") || !strings.Contains(text, "42") || strings.Contains(text, "参考名称") {
		t.Fatalf("new item: %s", text)
	}
	pairs, _ := c.archive.ListPairs(li)
	if len(pairs) != 5 || pairs[3].ID != "998" || pairs[4].Path != "custom/new.stk" {
		t.Fatalf("registrations: %#v", pairs)
	}
	raw, _ := c.archive.RawBytes(ei)
	if !bytes.Equal(raw, equBefore) {
		t.Fatal("unrelated list draft was saved")
	}
	if len(result.Files) != 1 || result.Files[0].BeforeText != req.Drafts[0].Text {
		t.Fatalf("draft baselines: %#v", result.Files)
	}
	if _, err := s.CreateItem(req); err == nil {
		t.Fatal("stale create accepted")
	}
}
func TestItemCreationChecksBothListsAndDrafts(t *testing.T) {
	c, s, req := itemCreationFixture(t)
	for _, id := range []string{"1", "4"} {
		req.ID = id
		check, err := s.CheckItemCreation(req)
		if err != nil || len(check.Conflicts) == 0 {
			t.Fatalf("id %s: %#v %v", id, check, err)
		}
	}
	req.ID = "999"
	li, _ := c.archive.Find("equipment/equipment.lst")
	req.Drafts = []ShopDraft{{FileIndex: li, Path: c.archive.Path(li), Text: "999 `reserved.equ`"}}
	check, err := s.CheckItemCreation(req)
	if err != nil || len(check.Conflicts) == 0 {
		t.Fatalf("draft collision: %#v %v", check, err)
	}
	// Dangling registrations still reserve a path, even when the file is absent.
	si, _ := c.archive.Find("stackable/stackable.lst")
	req.Drafts = []ShopDraft{{FileIndex: si, Path: c.archive.Path(si), Text: "998 `custom/new.stk`"}}
	check, err = s.CheckItemCreation(req)
	if err != nil || len(check.Conflicts) == 0 {
		t.Fatalf("dangling registration: %#v %v", check, err)
	}
}
func TestItemCreationRejectsInvalidRequestsWithoutMutation(t *testing.T) {
	c, s, req := itemCreationFixture(t)
	before := c.archive
	count := before.FileCount()
	revision := c.batchRevision
	for _, id := range []string{"0", "01", "-1", "1.2", "2147483648"} {
		r := req
		r.ID = id
		if _, err := s.CheckItemCreation(r); err == nil {
			t.Fatalf("accepted id %s", id)
		}
	}
	for _, path := range []string{"equipment/../new.equ", "equipment/new.stk", "stackable/new.equ", "else/new.stk", "stackable/a\n.stk"} {
		r := req
		r.Path = path
		if _, err := s.CheckItemCreation(r); err == nil {
			t.Fatalf("accepted path %s", path)
		}
	}
	check, err := s.CheckItemCreation(req)
	if err != nil {
		t.Fatal(err)
	}
	req.Revision = check.Revision
	req.Text = "[name]\n`a`\n[name]\n`b`"
	if _, err := s.CreateItem(req); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if c.archive != before || before.FileCount() != count || c.batchRevision != revision {
		t.Fatal("failed creation partially published")
	}
	req.Text = "[name]\n`x`"
	req.Revision--
	if _, err := s.CreateItem(req); err == nil {
		t.Fatal("stale revision accepted")
	}
}
func TestItemCreationRootListsAndIndependentLocalizedName(t *testing.T) {
	c, s, req := itemCreationFixture(t)
	a := pvf.New()
	mustAddText(t, a, "list/equipment.lst", "1 `equipment/original.equ`", pvf.TypeScript)
	mustAddText(t, a, "equipment/original.equ", "[name]\n{8=`<3::old_name>`}", pvf.TypeScript)
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	req.Path = "equipment/custom/new.equ"
	req.Text = "[name]\n{8=`<3::old_name>`}\n[detail explain]\n{10=`<3::shared_description>`}"
	result := checkedCreate(t, s, req)
	text, _ := c.archive.Text(result.FileIndex)
	if !strings.Contains(text, "新物品😀") || !strings.Contains(text, "shared_description") || strings.Contains(text, "old_name") {
		t.Fatal(text)
	}
	li, _ := c.archive.Find("list/equipment.lst")
	pairs, _ := c.archive.ListPairs(li)
	if pairs[1].Path != req.Path {
		t.Fatalf("root relative registration: %#v", pairs)
	}
}
func TestItemCreationVersionUndo(t *testing.T) {
	c, _, i := versionServiceFixture(t)
	versions := NewVersionService(c)
	if _, err := versions.Initialize(); err != nil {
		t.Fatal(err)
	}
	text, _ := c.archive.Text(i)
	req := ItemCreateRequest{Path: "equipment/custom/copied.equ", ID: "1999999999", Name: "复制物品", Text: text}
	result := checkedCreate(t, NewFileGUIService(c), req)
	if versions.Status().PendingChangeSets != 1 {
		t.Fatalf("creation should be one operation: %#v", versions.Status())
	}
	if _, err := versions.Undo(); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.archive.Find(result.Path); ok {
		t.Fatal("undo left copied item")
	}
	li, _ := c.archive.FindList("equipment/equipment.lst")
	pairs, _ := c.archive.ListPairs(li)
	for _, pair := range pairs {
		if pair.ID == req.ID {
			t.Fatal("undo left registration")
		}
	}
}
func TestItemCreationSQLiteProjection(t *testing.T) {
	c, s, req := itemCreationFixture(t)
	c.diskIndex = deltaIndexFixture(t, c.archive)
	t.Cleanup(c.closeArchive)
	result := checkedCreate(t, s, req)
	if _, ok := c.archive.Find(result.Path); !ok {
		t.Fatal("created file missing")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	assertDeltaProjection(t, c.diskIndex, c.archive)
}

func TestItemCreationSQLiteFailureLeavesArchiveUntouched(t *testing.T) {
	c, s, req := itemCreationFixture(t)
	c.diskIndex = deltaIndexFixture(t, c.archive)
	t.Cleanup(c.closeArchive)
	if _, err := c.diskIndex.db.Exec(`CREATE TRIGGER reject_item_creation BEFORE INSERT ON files BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	before := c.archive
	li, _ := before.Find("stackable/stackable.lst")
	original, _ := before.RawBytes(li)
	original = append([]byte(nil), original...)
	check, err := s.CheckItemCreation(req)
	if err != nil {
		t.Fatal(err)
	}
	req.Revision = check.Revision
	if _, err := s.CreateItem(req); err == nil {
		t.Fatal("injected projection failure accepted")
	}
	if c.archive != before {
		t.Fatal("failed creation published its archive")
	}
	if _, ok := before.Find(req.Path); ok {
		t.Fatal("failed creation left a file")
	}
	raw, _ := before.RawBytes(li)
	if !bytes.Equal(raw, original) {
		t.Fatal("failed creation changed list")
	}
}

func TestItemCreationSourceValidation(t *testing.T) {
	for _, source := range []string{"[name]\n`unterminated", "[if]\n[physical attack]\n10", "[name", "{8=`x`", "/* unterminated", "[if]\n[/then]"} {
		if _, err := itemCreationSource(source); err == nil {
			t.Fatalf("accepted malformed source %q", source)
		}
	}
	c, s, req := itemCreationFixture(t)
	req.Text = "[name]\n`old` // [bad] `unterminated\n[price]\n10 /* [another] */\n[detail explain]\n{10=`preserve /* text */ [tags]`}"
	result := checkedCreate(t, s, req)
	text, _ := c.archive.Text(result.FileIndex)
	if strings.Contains(text, "[bad]") || strings.Contains(text, "[another]") || !strings.Contains(text, "preserve /* text */ [tags]") {
		t.Fatal(text)
	}
}

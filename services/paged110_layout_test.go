package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pvfine/internal/pvf"
)

// paged110LayoutFixture builds the 110US list layout: every list lives under
// `list/`, entries are archive-root-relative, and item names are stored as
// `<table::key>` placeholders resolved through `list/n_string.lst`.
func paged110LayoutFixture(t *testing.T) (*core, *pvf.Archive, int32, int32) {
	t.Helper()
	a := pvf.New()
	encode := func(s string) []byte {
		out := make([]byte, 0, len(s)*2)
		for _, r := range s {
			out = append(out, byte(r), byte(r>>8))
		}
		return out
	}
	if _, err := a.AddFileText("list/n_string.lst", "3 `String/Equipment.uv.str`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("list/n_string_kor.lst", "3 `String/Equipment.kor.str`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	// name_514530375 is localized; name_514530376 is blank in the base table and
	// only the Korean overlay has it.
	a.AddFile("String/Equipment.uv.str",
		encode("name_514530375>白色兽语腰带 [A款]\r\nname_514530376=\r\n"), pvf.TypeScript)
	a.AddFile("String/Equipment.kor.str", encode("name_514530376>포니 비즈 뱅글[A타입]\r\n"), pvf.TypeScript)
	if _, err := a.AddFileText("list/equipment.lst",
		"514530375 `equipment/character/x/514530375.equ` 514530376 `equipment/character/x/514530376.equ`",
		pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	itemIndex, err := a.AddFileText("equipment/character/x/514530375.equ",
		"[name]\n{8=`<3::name_514530375>`}\n[rarity]\n4", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("equipment/character/x/514530376.equ",
		"[name]\n{8=`<3::name_514530376>`}", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	stringTableIndex, ok := a.Find("String/Equipment.uv.str")
	if !ok {
		t.Fatal("string table missing")
	}

	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	return c, a, itemIndex, stringTableIndex
}

// TestSearchIndexResolvesPaged110Names searches the display text of an item
// whose name is a placeholder in the 110US list layout.
func TestSearchIndexResolvesPaged110Names(t *testing.T) {
	c, _, itemIndex, _ := paged110LayoutFixture(t)
	c.startSearchIndex()
	waitForSearchIndex(t, c)

	result, err := NewArchiveService(c).Search("白色兽语腰带", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("hits = %#v", result.Hits)
	}
	hit := result.Hits[0]
	if hit.Category != SearchCategoryEquipment || hit.FileIndex != itemIndex || hit.Name != "白色兽语腰带 [A款]" {
		t.Fatalf("hit = %#v", hit)
	}
}

// TestSearchIndexMarksOverlayFallbackNames covers the marker the explorer, the
// search results and the editor tags add when a name only the language overlay
// could answer.
func TestSearchIndexMarksOverlayFallbackNames(t *testing.T) {
	c, a, _, _ := paged110LayoutFixture(t)
	c.startSearchIndex()
	waitForSearchIndex(t, c)

	const want = "포니 비즈 뱅글[A타입]（未翻译）"
	result, err := NewArchiveService(c).Search("포니 비즈 뱅글", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("hits = %#v", result.Hits)
	}
	if result.Hits[0].Name != want {
		t.Errorf("search name = %q, want %q", result.Hits[0].Name, want)
	}

	fallbackIndex, ok := a.Find("equipment/character/x/514530376.equ")
	if !ok {
		t.Fatal("fallback item missing")
	}
	file, err := NewEditorService(c).GetFile(fallbackIndex)
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range file.Tags {
		if tag.Name == want {
			return
		}
	}
	t.Fatalf("tree tags = %#v", file.Tags)
}

// TestEditorAnnotationsShowResolvedPlaceholder checks the editor keeps the
// placeholder in the document while showing the resolved text as a tag.
func TestEditorAnnotationsShowResolvedPlaceholder(t *testing.T) {
	c, _, itemIndex, stringTableIndex := paged110LayoutFixture(t)
	service := NewEditorService(c)

	file, err := service.GetFile(itemIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(file.Text, "<3::name_514530375>") {
		t.Fatalf("editor text lost the placeholder: %q", file.Text)
	}

	var resolved *EditorAnnotation
	for _, annotation := range file.Annotations {
		if annotation.Type == "placeholder" {
			copy := annotation
			resolved = &copy
			break
		}
	}
	if resolved == nil {
		t.Fatalf("no placeholder annotation in %#v", file.Annotations)
	}
	if resolved.Title != "白色兽语腰带 [A款]" {
		t.Errorf("title = %q", resolved.Title)
	}
	if resolved.TargetFileIndex != stringTableIndex {
		t.Errorf("target = %d, want %d", resolved.TargetFileIndex, stringTableIndex)
	}
	if !strings.Contains(resolved.Content, "String/Equipment.uv.str") {
		t.Errorf("content = %q", resolved.Content)
	}
}

// TestEditorAnnotationsMarkOverlayFallback covers the marker the preview and
// the editor add when only a language overlay can answer the placeholder.
func TestEditorAnnotationsMarkOverlayFallback(t *testing.T) {
	a := pvf.New()
	encode := func(s string) []byte {
		out := make([]byte, 0, len(s)*2)
		for _, r := range s {
			out = append(out, byte(r), byte(r>>8))
		}
		return out
	}
	if _, err := a.AddFileText("list/n_string.lst", "3 `String/Equipment.uv.str`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("list/n_string_kor.lst", "3 `String/Equipment.kor.str`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	a.AddFile("String/Equipment.uv.str", encode("name_1=\r\n"), pvf.TypeScript)
	a.AddFile("String/Equipment.kor.str", encode("name_1>포니 비즈 뱅글[A타입]\r\n"), pvf.TypeScript)
	itemIndex, err := a.AddFileText("equipment/a.equ", "[name]\n{8=`<3::name_1>`}", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	annotations, err := NewEditorService(c).GetAnnotations(itemIndex)
	if err != nil {
		t.Fatal(err)
	}
	for _, annotation := range annotations {
		if annotation.Type != "placeholder" {
			continue
		}
		if annotation.Title != "포니 비즈 뱅글[A타입]（未翻译）" {
			t.Fatalf("title = %q", annotation.Title)
		}
		return
	}
	t.Fatalf("no placeholder annotation in %#v", annotations)
}

// TestListTargetCandidates covers both list entry layouts.
// TestSetPlaceholderTextEditsStringTableOnly rewrites the text behind a
// placeholder: the script keeps its placeholder, the `.str` entry changes, and
// the editor tag, tree name and search index all show the new text.
func TestSetPlaceholderTextEditsStringTableOnly(t *testing.T) {
	c, a, itemIndex, _ := paged110LayoutFixture(t)
	c.startSearchIndex()
	waitForSearchIndex(t, c)
	service := NewEditorService(c)

	before, err := service.GetFile(itemIndex)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetPlaceholderText(itemIndex, 3, "name_514530375", "改过的腰带"); err != nil {
		t.Fatal(err)
	}

	// The stored script is untouched: only the string table changed.
	if got, ok := a.LookupStringTable(3, "name_514530375"); !ok || got != "改过的腰带" {
		t.Errorf("string table = %q, %v", got, ok)
	}
	after, err := service.GetFile(itemIndex)
	if err != nil {
		t.Fatal(err)
	}
	if after.Text != before.Text || !strings.Contains(after.Text, "<3::name_514530375>") {
		t.Errorf("script text changed: %q", after.Text)
	}

	found := false
	for _, annotation := range after.Annotations {
		if annotation.Type != "placeholder" || annotation.Placeholder == nil {
			continue
		}
		if annotation.Placeholder.TableIndex != 3 || annotation.Placeholder.Key != "name_514530375" {
			continue
		}
		found = true
		if annotation.Title != "改过的腰带" {
			t.Errorf("annotation title = %q", annotation.Title)
		}
	}
	if !found {
		t.Fatalf("placeholder annotation missing: %#v", after.Annotations)
	}

	tagFound := false
	for _, tag := range after.Tags {
		if tag.Name == "改过的腰带" {
			tagFound = true
		}
	}
	if !tagFound {
		t.Errorf("tree tags = %#v", after.Tags)
	}

	result, err := NewArchiveService(c).Search("改过的腰带", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range result.Hits {
		if hit.FileIndex == itemIndex && hit.Name == "改过的腰带" {
			return
		}
	}
	t.Fatalf("search hits = %#v", result.Hits)
}

// TestSetPlaceholderTextEditsOverlayEntry covers the fallback case: the base
// table has the key with an empty value, so the edit lands in the Korean overlay
// and the name stays flagged as untranslated.
func TestSetPlaceholderTextEditsOverlayEntry(t *testing.T) {
	c, a, _, _ := paged110LayoutFixture(t)
	fallbackIndex, ok := a.Find("equipment/character/x/514530376.equ")
	if !ok {
		t.Fatal("fallback item missing")
	}
	service := NewEditorService(c)
	if err := service.SetPlaceholderText(fallbackIndex, 3, "name_514530376", "改过的韩文"); err != nil {
		t.Fatal(err)
	}
	if got, ok := a.LookupStringTable(3, "name_514530376"); !ok || got != "改过的韩文" {
		t.Errorf("overlay entry = %q, %v", got, ok)
	}
	// The base table must stay empty, so the value is still a fallback.
	resolution, ok := a.ResolveStringTable(3, "name_514530376")
	if !ok || resolution.Source == "" || !resolution.Fallback {
		t.Errorf("resolution = %#v, %v", resolution, ok)
	}
	if !strings.Contains(strings.ToLower(resolution.Source), "kor") {
		t.Errorf("edit did not land in the overlay: %s", resolution.Source)
	}
	annotations, err := service.GetAnnotations(fallbackIndex)
	if err != nil {
		t.Fatal(err)
	}
	for _, annotation := range annotations {
		if annotation.Type != "placeholder" || annotation.Placeholder == nil {
			continue
		}
		if !annotation.Placeholder.Fallback {
			t.Error("annotation is no longer marked as a fallback")
		}
		if annotation.Title != "改过的韩文（未翻译）" {
			t.Errorf("title = %q", annotation.Title)
		}
		return
	}
	t.Fatalf("placeholder annotation missing: %#v", annotations)
}

// TestPlaceholderCreationForNewFile covers the new-file workflow: a file that
// references a key no table has yet gets a "create" annotation, and filling it
// in appends the entry to the base table.
func TestPlaceholderCreationForNewFile(t *testing.T) {
	a := pvf.New()
	encode := func(s string) []byte {
		out := make([]byte, 0, len(s)*2)
		for _, r := range s {
			out = append(out, byte(r), byte(r>>8))
		}
		return out
	}
	if _, err := a.AddFileText("list/n_string.lst", "3 `String/Equipment.uv.str`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	a.AddFile("String/Equipment.uv.str", encode("name_514530375>白色兽语腰带 [A款]\r\n"), pvf.TypeScript)
	// A brand-new entry registered in the equipment list, referencing a key the
	// tables do not know.
	newIndex, err := a.AddFileText("equipment/character/x/new_item.equ",
		"[name]\n{8=`<3::name_new_item>`}\n[rarity]\n4", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("list/equipment.lst",
		"900000001 `equipment/character/x/new_item.equ`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	c.startSearchIndex()
	waitForSearchIndex(t, c)
	service := NewEditorService(c)

	file, err := service.GetFile(newIndex)
	if err != nil {
		t.Fatal(err)
	}
	var missing *EditorAnnotation
	for _, annotation := range file.Annotations {
		if annotation.Type == "placeholder-missing" {
			copy := annotation
			missing = &copy
		}
	}
	if missing == nil {
		t.Fatalf("no create annotation in %#v", file.Annotations)
	}
	if missing.Placeholder == nil || !missing.Placeholder.Missing || missing.Placeholder.Key != "name_new_item" {
		t.Fatalf("create annotation = %#v", missing)
	}
	if missing.Title != missingPlaceholderLabel {
		t.Errorf("title = %q", missing.Title)
	}

	// Create the entry the way the dialog does.
	if err := service.SetPlaceholderText(newIndex, 3, "name_new_item", "新装备名"); err != nil {
		t.Fatal(err)
	}
	if got, ok := a.LookupStringTable(3, "name_new_item"); !ok || got != "新装备名" {
		t.Errorf("created entry = %q, %v", got, ok)
	}
	after, err := service.GetFile(newIndex)
	if err != nil {
		t.Fatal(err)
	}
	resolved := false
	for _, annotation := range after.Annotations {
		if annotation.Type != "placeholder" || annotation.Placeholder == nil {
			continue
		}
		resolved = true
		if annotation.Title != "新装备名" {
			t.Errorf("title after creation = %q", annotation.Title)
		}
	}
	if !resolved {
		t.Errorf("annotation stayed unresolved: %#v", after.Annotations)
	}
	tagged := false
	for _, tag := range after.Tags {
		if tag.Name == "新装备名" {
			tagged = true
		}
	}
	if !tagged {
		t.Errorf("tree tags = %#v", after.Tags)
	}
	result, err := NewArchiveService(c).Search("新装备名", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range result.Hits {
		if hit.FileIndex == newIndex && hit.Name == "新装备名" {
			return
		}
	}
	t.Fatalf("new entry not searchable: %#v", result.Hits)
}

func TestListTargetCandidates(t *testing.T) {
	candidates, ok := listPathCandidates("equipment/equipment.lst", "character/a.equ")
	if !ok {
		t.Fatal("list-dir-relative entry rejected")
	}
	if candidates[0] != "equipment/character/a.equ" {
		t.Errorf("first candidate = %q", candidates[0])
	}
	found := false
	for _, candidate := range candidates {
		if candidate == "character/a.equ" {
			found = true
		}
	}
	if !found {
		t.Errorf("root-relative candidate missing: %v", candidates)
	}

	// 110US: a configured 90US list path must still resolve root-relative
	// entries stored under `list/`.
	candidates, ok = listPathCandidates("list/equipment.lst", "equipment/character/a.equ")
	if !ok {
		t.Fatal("root-relative entry rejected")
	}
	found = false
	for _, candidate := range candidates {
		if candidate == "equipment/character/a.equ" {
			found = true
		}
	}
	if !found {
		t.Errorf("root-relative candidate missing for list/ layout: %v", candidates)
	}
}

// TestSearchIndexRealPaged110 names the retail archive end to end: the 110US
// list layout resolves, item names become display text, and searching the
// Chinese name finds the item. Opening the 543MB archive plus indexing its
// 638k list entries takes roughly half a minute, so the test skips when the
// fixture is absent.
func TestSearchIndexRealPaged110(t *testing.T) {
	archive := filepath.Join("..", "testdata", "110US.pvf")
	if _, err := os.Stat(archive); err != nil {
		t.Skip("testdata/110US.pvf not present")
	}
	if _, err := os.Stat(filepath.Join("..", "testdata", "sk.dat")); err != nil {
		t.Skip("testdata/sk.dat not present")
	}
	a, err := pvf.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	listIndex, ok := a.FindList("equipment/equipment.lst")
	if !ok {
		t.Fatal("equipment list not found")
	}
	if got := a.Path(listIndex); got != "list/equipment.lst" {
		t.Fatalf("equipment list = %s", got)
	}
	const itemPath = "character/demoniclancer/avatar/belt/514530375.equ"
	itemIndex, ok := a.Find(itemPath)
	if !ok {
		t.Fatalf("%s not found", itemPath)
	}
	metadata, err := a.ScriptMetadata(itemIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.HasName || metadata.Name != "白色兽语腰带 [A款]" {
		t.Fatalf("%s metadata = %#v", itemPath, metadata)
	}

	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	c.startSearchIndex()
	waitForSearchIndexWithin(t, c, 15*time.Minute)

	result, err := NewArchiveService(c).Search("白色兽语腰带", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, hit := range result.Hits {
		if hit.Category != SearchCategoryEquipment || !strings.HasPrefix(hit.Name, "白色兽语腰带") {
			continue
		}
		// The indexed name is the display text, not the placeholder.
		metadata, err := a.ScriptMetadata(hit.FileIndex)
		if err != nil || metadata.Name != hit.Name {
			t.Fatalf("hit %s name %q, metadata %#v err=%v", hit.Path, hit.Name, metadata, err)
		}
		found = true
		break
	}
	if !found {
		for _, hit := range result.Hits {
			t.Logf("hit [%s] %s | %s", hit.Category, hit.Name, hit.Path)
		}
		t.Fatalf("no equipment hit for the Chinese name among %d hits", len(result.Hits))
	}

	// A name this localization left blank is answered by the Korean overlay and
	// is flagged in the explorer and the search results.
	fallbackIndex, ok := a.Find("equipment/character/archer/avatar/belt/117530006.equ")
	if !ok {
		t.Fatal("fallback item not found")
	}
	metadata, err = a.ScriptMetadata(fallbackIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.NameFallback {
		t.Fatalf("%s is not an overlay fallback: %#v", a.Path(fallbackIndex), metadata)
	}
	result, err = NewArchiveService(c).Search("포니 비즈 뱅글", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	flagged := false
	for _, hit := range result.Hits {
		if hit.FileIndex == fallbackIndex && hit.Name == metadata.Name+untranslatedMark {
			flagged = true
		}
	}
	if !flagged {
		for _, hit := range result.Hits {
			t.Logf("hit [%s] %s | %s", hit.Category, hit.Name, hit.Path)
		}
		t.Fatalf("fallback name not flagged in the search results")
	}

	// Editing the text behind the placeholder changes the `.str` entry only and
	// is visible through the editor tag, the tree and the search index. This
	// item is registered in the equipment list, so it has index records.
	editIndex, ok := a.Find("equipment/character/archer/avatar/belt/117530002.equ")
	if !ok {
		t.Fatal("editable item not found")
	}
	const (
		editKey   = "name_117530002"
		editValue = "改过的稀有腰带"
	)
	editorService := NewEditorService(c)
	before, err := editorService.GetFile(editIndex)
	if err != nil {
		t.Fatal(err)
	}
	if err := editorService.SetPlaceholderText(editIndex, 3, editKey, editValue); err != nil {
		t.Fatal(err)
	}
	after, err := editorService.GetFile(editIndex)
	if err != nil {
		t.Fatal(err)
	}
	if after.Text != before.Text || !strings.Contains(after.Text, "<3::"+editKey+">") {
		t.Errorf("script text changed: %q", after.Text)
	}
	if got, ok := a.LookupStringTable(3, editKey); !ok || got != editValue {
		t.Errorf("string table = %q, %v", got, ok)
	}
	tagged := false
	for _, tag := range after.Tags {
		if tag.Name == editValue {
			tagged = true
		}
	}
	if !tagged {
		t.Errorf("tree tags = %#v", after.Tags)
	}
	result, err = NewArchiveService(c).Search(editValue, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range result.Hits {
		if hit.FileIndex == editIndex && hit.Name == editValue {
			return
		}
	}
	t.Fatalf("edited name not searchable: %#v", result.Hits)
}

func waitForSearchIndexWithin(t *testing.T, c *core, limit time.Duration) IndexStatus {
	t.Helper()
	deadline := time.Now().Add(limit)
	svc := NewArchiveService(c)
	for time.Now().Before(deadline) {
		status := svc.IndexStatus()
		if status.State == IndexStateReady || status.State == IndexStateError {
			if status.State == IndexStateError {
				t.Fatalf("search index failed: %s", status.Error)
			}
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("search index did not finish: %#v", svc.IndexStatus())
	return IndexStatus{}
}

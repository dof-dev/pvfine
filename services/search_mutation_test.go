package services

import (
	"testing"
	"time"

	"pvfine/internal/pvf"
)

func TestSearchIndexRefreshRequestsCoalesceWhileBuilding(t *testing.T) {
	c := NewCore()
	c.archive = pvf.New()
	c.indexStatus = IndexStatus{State: IndexStateBuilding}
	cancelled := false
	c.indexCancel = func() { cancelled = true }
	t.Cleanup(func() {
		c.mu.Lock()
		c.indexCancel = nil
		c.mu.Unlock()
		c.closeArchive()
	})

	c.startSearchIndex()
	c.startSearchIndexForced()

	c.mu.RLock()
	queued := c.indexRefreshPending
	queuedForce := c.indexRefreshPendingForce
	generation := c.indexGen
	c.mu.RUnlock()
	if cancelled {
		t.Fatal("queued refresh cancelled the active build")
	}
	if !queued || !queuedForce {
		t.Fatalf("refresh queue = pending:%t force:%t, want both true", queued, queuedForce)
	}
	if generation != 0 {
		t.Fatalf("queued refresh advanced generation: %d", generation)
	}
}

func TestSearchIndexSkipsUnrelatedPayloadEdits(t *testing.T) {
	path := writeSearchFixture(t, "unrelated-edit.pvf")
	c := NewCore()
	service := NewArchiveService(c)
	if _, err := service.Open(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	waitForSearchIndex(t, c)

	target, ok := c.archive.Find("equipment/character/common/amulet/1008.equ")
	if !ok {
		t.Fatal("registered target missing")
	}
	beforeGeneration := c.indexGen
	beforeStatus := service.IndexStatus()
	text := "[name]\n`烈火之心项链`\n[icon]\n`Item/test.img`\n3\n[field image]\n`Item/field.img`\n4\n[grade]\n99"
	if err := NewEditorService(c).SetText(target, text); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if c.indexGen != beforeGeneration {
		t.Fatalf("unrelated registered payload restarted index: before=%d after=%d", beforeGeneration, c.indexGen)
	}
	status := service.IndexStatus()
	if status.State != IndexStateReady || status.Refreshing || status.BuildDurationMs != beforeStatus.BuildDurationMs {
		t.Fatalf("index status changed for unrelated payload: before=%#v after=%#v", beforeStatus, status)
	}

	unregistered, ok := c.archive.Find("misc/readme.txt")
	if !ok {
		t.Fatal("unregistered file missing")
	}
	if err := NewEditorService(c).SetText(unregistered, "changed ordinary content"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if c.indexGen != beforeGeneration {
		t.Fatalf("unregistered payload restarted index: before=%d after=%d", beforeGeneration, c.indexGen)
	}
}

func TestSearchIndexListEditTriggersSemanticRefresh(t *testing.T) {
	path := writeSearchFixture(t, "list-edit.pvf")
	c := NewCore()
	service := NewArchiveService(c)
	if _, err := service.Open(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	waitForSearchIndex(t, c)

	listIndex, ok := c.archive.Find("equipment/equipment.lst")
	if !ok {
		t.Fatal("equipment list missing")
	}
	listText := "1008 `character/common/amulet/1008.equ` 1010 `character/common/amulet/1008.equ` 1011 `character/common/amulet/missing.equ` 2000 `misc/readme.txt`"
	if err := NewEditorService(c).SetText(listIndex, listText); err != nil {
		t.Fatal(err)
	}
	waitForSearchRefresh(t, c)
	result, err := service.Search("2000", 0, 10)
	if err != nil || len(result.Hits) != 1 || result.Hits[0].Path != "misc/readme.txt" {
		t.Fatalf("list edit search = %#v, err = %v", result.Hits, err)
	}
}

func TestSearchIndexKeepsLatestRapidNameEdit(t *testing.T) {
	path := writeSearchFixture(t, "rapid-name-edit.pvf")
	c := NewCore()
	service := NewArchiveService(c)
	if _, err := service.Open(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	waitForSearchIndex(t, c)

	target, ok := c.archive.Find("equipment/character/common/amulet/1008.equ")
	if !ok {
		t.Fatal("registered target missing")
	}
	editor := NewEditorService(c)
	for _, name := range []string{"连续改名-1", "连续改名-2", "连续改名-3"} {
		if err := editor.SetText(target, "[name]\n`"+name+"`\n[grade]\n1"); err != nil {
			t.Fatal(err)
		}
	}

	waitForSearchRefresh(t, c)
	latest, err := service.Search("连续改名-3", 0, 20)
	if err != nil || len(latest.Hits) != 2 {
		t.Fatalf("latest rapid name search = %#v, err = %v", latest.Hits, err)
	}
	for _, oldName := range []string{"烈火之心项链", "连续改名-1", "连续改名-2"} {
		old, err := service.Search(oldName, 0, 20)
		if err != nil || len(old.Hits) != 0 {
			t.Fatalf("stale rapid name search %q = %#v, err = %v", oldName, old.Hits, err)
		}
	}
}

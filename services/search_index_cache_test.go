package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"pvfine/internal/pvf"
)

func waitForSearchCacheFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("search index cache was not written: %s", path)
}

func TestSearchIndexCacheHit(t *testing.T) {
	archivePath := writeSearchFixture(t, "cached-search.pvf")
	cachePath := filepath.Join(t.TempDir(), "search-index.json.gz")

	first := NewCore()
	first.searchIndexCachePath = cachePath
	firstService := NewArchiveService(first)
	if _, err := firstService.Open(archivePath); err != nil {
		t.Fatal(err)
	}
	waitForSearchIndex(t, first)
	waitForSearchCacheFile(t, cachePath)
	first.closeArchive()

	second := NewCore()
	second.searchIndexCachePath = cachePath
	secondService := NewArchiveService(second)
	if _, err := secondService.Open(archivePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.closeArchive)
	status := waitForSearchIndex(t, second)
	if !status.CacheHit || status.Stage != "ready-cache" {
		t.Fatalf("cache status = %#v", status)
	}
	result, err := secondService.Search("烈火之心项链", 0, 10)
	if err != nil || len(result.Hits) != 2 {
		t.Fatalf("cached search = %d hits, err = %v", len(result.Hits), err)
	}
}

func TestSearchIndexCacheInvalidatesOnSourceChange(t *testing.T) {
	archivePath := writeSearchFixture(t, "invalidated-search.pvf")
	cachePath := filepath.Join(t.TempDir(), "search-index.json.gz")

	first := NewCore()
	first.searchIndexCachePath = cachePath
	if _, err := NewArchiveService(first).Open(archivePath); err != nil {
		t.Fatal(err)
	}
	waitForSearchIndex(t, first)
	waitForSearchCacheFile(t, cachePath)
	first.closeArchive()

	file, err := os.OpenFile(archivePath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("x"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	second := NewCore()
	second.searchIndexCachePath = cachePath
	if _, err := NewArchiveService(second).Open(archivePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.closeArchive)
	status := waitForSearchIndex(t, second)
	if status.CacheHit {
		t.Fatalf("stale cache was used: %#v", status)
	}
}

func TestSearchIndexCacheIgnoresUnsavedEdits(t *testing.T) {
	archivePath := writeSearchFixture(t, "unsaved-search.pvf")
	cachePath := filepath.Join(t.TempDir(), "search-index.json.gz")

	seed := NewCore()
	seed.searchIndexCachePath = cachePath
	seedService := NewArchiveService(seed)
	if _, err := seedService.Open(archivePath); err != nil {
		t.Fatal(err)
	}
	waitForSearchIndex(t, seed)
	waitForSearchCacheFile(t, cachePath)
	seed.closeArchive()

	edited := NewCore()
	edited.searchIndexCachePath = cachePath
	editedService := NewArchiveService(edited)
	if _, err := editedService.Open(archivePath); err != nil {
		t.Fatal(err)
	}
	waitForSearchIndex(t, edited)
	hits, err := editedService.Search("烈火之心项链", 0, 10)
	if err != nil || len(hits.Hits) == 0 {
		t.Fatalf("seed search = %d hits, err = %v", len(hits.Hits), err)
	}
	if err := NewEditorService(edited).SetText(hits.Hits[0].FileIndex, "[name]\n`未保存名称`"); err != nil {
		t.Fatal(err)
	}
	waitForSearchRefresh(t, edited)
	edited.closeArchive()

	reopened := NewCore()
	reopened.searchIndexCachePath = cachePath
	reopenedService := NewArchiveService(reopened)
	if _, err := reopenedService.Open(archivePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.closeArchive)
	status := waitForSearchIndex(t, reopened)
	if !status.CacheHit {
		t.Fatalf("cache was unexpectedly replaced by unsaved edit: %#v", status)
	}
	old, err := reopenedService.Search("烈火之心项链", 0, 10)
	if err != nil || len(old.Hits) == 0 {
		t.Fatalf("reopened old search = %d hits, err = %v", len(old.Hits), err)
	}
}

func TestSearchIndexNewFileUsesAsyncDelta(t *testing.T) {
	archivePath := writeSearchFixture(t, "delta-search.pvf")
	a, err := pvf.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	c.startSearchIndex()
	waitForSearchIndex(t, c)

	service := NewArchiveService(c)
	if _, err := service.CreateFile("misc/generated.txt", pvf.TypeUnicode); err != nil {
		t.Fatal(err)
	}
	status := service.IndexStatus()
	if status.State != IndexStateReady {
		t.Fatalf("index became unavailable during delta: %#v", status)
	}
	old, err := service.Search("烈火之心项链", 0, 10)
	if err != nil || len(old.Hits) != 2 {
		t.Fatalf("old search during delta = %d hits, err = %v", len(old.Hits), err)
	}
	waitForSearchRefresh(t, c)
	newHits, err := service.Search("generated.txt", 0, 10)
	if err != nil || len(newHits.Hits) != 1 {
		t.Fatalf("new path search = %d hits, err = %v", len(newHits.Hits), err)
	}
}

func TestSearchIndexListRegistrationUsesAsyncDelta(t *testing.T) {
	archivePath := writeSearchFixture(t, "registration-delta.pvf")
	a, err := pvf.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	c.startSearchIndex()
	waitForSearchIndex(t, c)
	fileIndex, ok := a.Find("misc/readme.txt")
	if !ok {
		t.Fatal("unregistered file missing")
	}

	service := NewArchiveService(c)
	if _, err := service.RegisterFileToList(fileIndex, "equipment/equipment.lst", "2000"); err != nil {
		t.Fatal(err)
	}
	if status := service.IndexStatus(); status.State != IndexStateReady {
		t.Fatalf("index became unavailable during registration: %#v", status)
	}
	waitForSearchRefresh(t, c)
	result, err := service.Search("2000", 0, 10)
	if err != nil || len(result.Hits) != 1 || result.Hits[0].FileIndex != fileIndex {
		t.Fatalf("registered search = %#v, err = %v", result.Hits, err)
	}
}

func TestSearchIndexDeleteUsesAsyncDelta(t *testing.T) {
	archivePath := writeSearchFixture(t, "delete-delta.pvf")
	a, err := pvf.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	c.startSearchIndex()
	waitForSearchIndex(t, c)
	fileIndex, ok := a.Find("misc/readme.txt")
	if !ok {
		t.Fatal("file to delete missing")
	}

	service := NewArchiveService(c)
	if _, err := service.DeleteFiles([]int32{fileIndex}); err != nil {
		t.Fatal(err)
	}
	if status := service.IndexStatus(); status.State != IndexStateReady {
		t.Fatalf("index became unavailable during delete: %#v", status)
	}
	waitForSearchRefresh(t, c)
	deleted, err := service.Search("misc/readme.txt", 0, 10)
	if err != nil || len(deleted.Hits) != 0 {
		t.Fatalf("deleted path search = %#v, err = %v", deleted.Hits, err)
	}
	remaining, err := service.Search("烈火之心项链", 0, 10)
	if err != nil || len(remaining.Hits) != 2 {
		t.Fatalf("remaining search = %d hits, err = %v", len(remaining.Hits), err)
	}
}

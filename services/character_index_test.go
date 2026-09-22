package services

import (
	"path/filepath"
	"testing"
)

func TestCharacterNamesInMemorySQLiteAndIncrementalIndex(t *testing.T) {
	for _, disk := range []bool{false, true} {
		name := "memory"
		if disk {
			name = "sqlite"
		}
		t.Run(name, func(t *testing.T) {
			c, _ := shopFixture(t)
			a := c.archive
			if err := c.setArchive(a); err != nil {
				t.Fatal(err)
			}
			if disk {
				index, _, err := openSQLiteArchiveIndex(a)
				if err != nil {
					t.Fatal(err)
				}
				c.mu.Lock()
				c.diskIndex = index
				c.installDiskArchiveIndexesLocked(a)
				c.mu.Unlock()
			}
			t.Cleanup(c.closeArchive)
			c.startSearchIndex()
			waitForSearchIndex(t, c)
			service := NewArchiveService(c)
			result, err := service.Search("鬼剑士", 0, 10)
			if err != nil || len(result.Hits) != 1 || result.Hits[0].Name != "鬼剑士" {
				t.Fatalf("search=%#v err=%v", result, err)
			}
			i, _ := a.Find("character/sword.chr")
			if err := NewEditorService(c).SetText(i, "[name]\n`generic`\n[growtype name]\n`新职业` `转职`"); err != nil {
				t.Fatal(err)
			}
			waitForSearchRefresh(t, c)
			waitForSearchIndex(t, c)
			result, err = service.Search("新职业", 0, 10)
			if err != nil || len(result.Hits) != 1 || result.Hits[0].Name != "新职业" {
				t.Fatalf("updated search=%#v err=%v", result, err)
			}
			c.mu.Lock()
			ref, ok := c.resolveAnnotationReferenceLocked("职业", "0")
			c.mu.Unlock()
			if !ok || ref.Name != "新职业" {
				t.Fatalf("relation=%#v found=%v", ref, ok)
			}
		})
	}
}

func TestCharacterNamesSurviveSearchCache(t *testing.T) {
	fixture, _ := shopFixture(t)
	archivePath := filepath.Join(t.TempDir(), "characters.pvf")
	if err := fixture.archive.SaveAs(archivePath); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "index.json.gz")
	for pass := 0; pass < 2; pass++ {
		c := NewCore()
		c.searchIndexCachePath = cache
		service := NewArchiveService(c)
		if _, err := service.Open(archivePath); err != nil {
			t.Fatal(err)
		}
		status := waitForSearchIndex(t, c)
		if pass == 1 && !status.CacheHit {
			t.Fatal("cache not restored")
		}
		result, err := service.Search("鬼剑士", 0, 10)
		if err != nil || len(result.Hits) != 1 || result.Hits[0].Name != "鬼剑士" {
			t.Fatalf("cached search=%#v err=%v", result, err)
		}
		waitForSearchCacheFile(t, cache)
		c.closeArchive()
	}
}

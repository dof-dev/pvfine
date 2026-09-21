package services

import (
	"os"
	"runtime"
	"testing"
	"time"
)

// TestLargeArchiveMemory is an opt-in regression probe for the real large
// archive. It intentionally logs measurements instead of asserting a machine
// specific RSS limit. Run with PVF_MEMORY_TEST=1 and PVF_TESTFILE set.
func TestLargeArchiveMemory(t *testing.T) {
	if os.Getenv("PVF_MEMORY_TEST") != "1" {
		t.Skip("PVF_MEMORY_TEST=1 required")
	}
	path := os.Getenv("PVF_TESTFILE")
	if path == "" {
		t.Fatal("PVF_TESTFILE is required")
	}
	c := NewCore()
	service := NewArchiveService(c)
	started := time.Now()
	if _, err := service.Open(path); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for deadline := time.Now().Add(10 * time.Minute); time.Now().Before(deadline); {
		status := service.IndexStatus()
		if status.State == IndexStateReady {
			break
		}
		if status.State == IndexStateError {
			t.Fatal(status.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	status := service.IndexStatus()
	if status.State != IndexStateReady {
		t.Fatalf("index did not become ready: %#v", status)
	}
	if _, err := service.ListChildren(""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Search("a", 0, 5); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PVF_ADVANCED_TEST") == "1" {
		if _, err := service.AdvancedSearch(AdvancedSearchModeString, "npc", "", false, 0, 1); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("PVF_REBUILD_TEST") == "1" {
		rebuildStarted := time.Now()
		if _, err := service.RebuildSearchIndex(); err != nil {
			t.Fatal(err)
		}
		for deadline := time.Now().Add(10 * time.Minute); time.Now().Before(deadline); {
			status = service.IndexStatus()
			if status.State == IndexStateReady {
				break
			}
			if status.State == IndexStateError {
				t.Fatal(status.Error)
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Logf("forced_rebuild=%s stage=%s", time.Since(rebuildStarted), status.Stage)
	}
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	t.Logf("files=%d index=%s cache_hit=%t elapsed=%s live_heap=%.1fMiB heap_sys=%.1fMiB total_alloc=%.1fMiB", c.archive.FileCount(), status.Stage, status.CacheHit, time.Since(started), float64(stats.HeapAlloc)/1048576, float64(stats.HeapSys)/1048576, float64(stats.TotalAlloc)/1048576)
}

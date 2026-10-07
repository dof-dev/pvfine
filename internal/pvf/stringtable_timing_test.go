package pvf

import (
	"sync"
	"testing"
)

func stringTableTimingFixture(t *testing.T) *Archive {
	t.Helper()
	a := New()
	if _, err := a.AddFileText("list/n_string.lst", "3 `String/Equipment.uv.str`", TypeScript); err != nil {
		t.Fatal(err)
	}
	a.AddFile("String/Equipment.uv.str", utf16LEBytes("first>First\r\nsecond>Second\r\n"), TypeScript)
	a.SetSourcePath("fixture.pvf")
	return a
}

func TestStringTableIndexTimingsAndCachedLookups(t *testing.T) {
	a := stringTableTimingFixture(t)
	if stats := a.StringTableIndexStats(); stats.State != "idle" || stats.Revision != 0 {
		t.Fatalf("initial stats = %#v", stats)
	}
	var events []StringTableIndexEvent
	a.SetStringTableIndexObserver(func(event StringTableIndexEvent) {
		// Inspecting stats here also verifies observers are called outside the lock.
		a.StringTableIndexStats()
		events = append(events, event)
	})
	if value, ok := a.LookupStringTable(3, "first"); !ok || value != "First" {
		t.Fatalf("lookup = %q, %t", value, ok)
	}
	stats := a.StringTableIndexStats()
	if stats.State != "ready" || stats.ActiveBuilds != 0 || stats.MappingBuilds != 1 ||
		stats.MappingCount != 1 || stats.TableBuilds != 1 || stats.Entries != 2 || stats.Bytes <= 0 ||
		stats.FailedTables != 0 || stats.Revision != 4 {
		t.Fatalf("built stats = %#v", stats)
	}
	if stats.MappingDurationMs < 0 || stats.TableDurationMs < 0 ||
		stats.BuildDurationMs != stats.MappingDurationMs+stats.TableDurationMs {
		t.Fatalf("invalid durations = %#v", stats)
	}
	if len(events) != 5 || events[0].Kind != "snapshot" || events[1].Kind != "mapping" || events[1].State != "building" ||
		events[4].Path != "String/Equipment.uv.str" || events[4].ArchivePath != "fixture.pvf" {
		t.Fatalf("build events = %#v", events)
	}
	a.LookupStringTable(3, "second")
	a.LookupStringTable(3, "missing")
	if next := a.StringTableIndexStats(); next != stats || len(events) != 5 {
		t.Fatalf("cached lookups changed timing: %#v", next)
	}
	a.InvalidateStringTables()
	a.LookupStringTable(3, "first")
	if next := a.StringTableIndexStats(); next.TableBuilds != 2 || next.MappingBuilds != 2 ||
		next.Entries != 4 || next.BuildDurationMs < stats.BuildDurationMs {
		t.Fatalf("rebuild stats = %#v", next)
	}
	a.Release()
	if next := a.StringTableIndexStats(); next.State != "idle" || next.TableBuilds != 0 || next.Revision != 0 {
		t.Fatalf("released stats = %#v", next)
	}
}

func TestStringTableIndexFailureTimings(t *testing.T) {
	a := New()
	if _, err := a.AddFileText("list/n_string.lst", "3 `String/Missing.uv.str`", TypeScript); err != nil {
		t.Fatal(err)
	}
	var last StringTableIndexEvent
	a.SetStringTableIndexObserver(func(event StringTableIndexEvent) { last = event })
	if _, ok := a.LookupStringTable(3, "missing"); ok {
		t.Fatal("missing table resolved")
	}
	stats := a.StringTableIndexStats()
	if stats.FailedTables != 1 || stats.TableBuilds != 1 || stats.ActiveBuilds != 0 ||
		last.State != "error" || last.Error == "" || last.DurationMs < 0 {
		t.Fatalf("failure stats = %#v, event = %#v", stats, last)
	}
	a.LookupStringTable(3, "another")
	if next := a.StringTableIndexStats(); next != stats {
		t.Fatalf("cached failure changed stats: %#v", next)
	}
}

func TestStringTableIndexConcurrentBuildOnce(t *testing.T) {
	a := stringTableTimingFixture(t)
	a.StringTablePaths(3)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Go(func() {
			a.LookupStringTable(3, "first")
			a.StringTableIndexStats()
		})
	}
	workers.Wait()
	if stats := a.StringTableIndexStats(); stats.TableBuilds != 1 || stats.Entries != 2 || stats.ActiveBuilds != 0 {
		t.Fatalf("concurrent stats = %#v", stats)
	}
}

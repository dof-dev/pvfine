package services

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"pvfine/internal/pvf"
)

func advancedFixture(t *testing.T) (*core, *ArchiveService) {
	t.Helper()
	a := pvf.New()
	for i, text := range []string{"[name]\n`共同 Alpha`\n{3=`共同 Alpha`}\n{5=`重复`}\n{7=`重复`}", "[name]\n`共同 beta`", "[name]\n`共同 Gamma`", "[name]\n`中文 标点.*?`"} {
		if _, err := a.AddFileText(fmt.Sprintf("dir/%d.equ", i), text, pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.AddFileText("other/4.equ", "[name]\n`共同 outside`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.mu.RLock()
		d := c.advancedDisk
		c.mu.RUnlock()
		c.closeArchive()
		if d != nil {
			select {
			case <-d.closed:
			case <-time.After(5 * time.Second):
				t.Error("session cleanup timed out")
			}
		}
	})
	return c, NewArchiveService(c)
}

func TestSQLiteFileURIUsesLocalFileForm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	u, err := url.Parse(sqliteFileURI(path, "mode=ro"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "file" || u.Host != "" {
		t.Fatalf("unexpected local file URI: %q", u.String())
	}
	if u.Query().Get("mode") != "ro" {
		t.Fatalf("missing read-only query: %q", u.String())
	}
}

func TestAdvancedSQLiteMatchesMemoryOracle(t *testing.T) {
	c, svc := advancedFixture(t)
	oracle, err := c.archive.BuildStringPoolIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query, scope string
		regex        bool
	}{
		{"共同", "", false}, {"ALPHA", "DIR", false}, {"中文", "", false}, {".*?", "", false},
		{"^共同.*[aA]$", "dir", true}, {"重复", "", false}, {"dir", "", false}, {"0.equ", "", false}, {"absent", "", false},
	} {
		t.Run(tc.query+tc.scope, func(t *testing.T) {
			want, err := oracle.MatchDetails(tc.query, tc.regex)
			if err != nil {
				t.Fatal(err)
			}
			filtered := want[:0]
			for _, m := range want {
				if advancedPathInScope(c.archive.Path(m.FileIndex), normalizeAdvancedScope(tc.scope)) {
					filtered = append(filtered, m)
				}
			}
			want = filtered
			var got []pvf.StringPoolMatch
			cursor := 0
			seen := map[int32]bool{}
			for {
				result, err := svc.AdvancedSearch("string", tc.query, tc.scope, tc.regex, cursor, 1)
				if err != nil {
					t.Fatal(err)
				}
				for _, hit := range result.Hits {
					if seen[hit.FileIndex] {
						t.Fatal("duplicate file across pages")
					}
					seen[hit.FileIndex] = true
					for _, d := range hit.Details {
						got = append(got, pvf.StringPoolMatch{FileIndex: hit.FileIndex, Pool: d.Pool, Offset: d.PoolOffset, Value: d.Value, Occurrences: d.Occurrences, TokenTypes: d.TokenTypes, FileFields: d.FileFields})
					}
				}
				if result.NextCursor < 0 {
					break
				}
				if result.NextCursor == cursor {
					t.Fatal("cursor did not advance")
				}
				cursor = result.NextCursor
			}
			sort.Slice(got, func(i, j int) bool {
				if got[i].FileIndex != got[j].FileIndex {
					return got[i].FileIndex < got[j].FileIndex
				}
				return got[i].Offset < got[j].Offset
			})
			if len(got) != len(want) {
				t.Fatalf("got %d details, want %d", len(got), len(want))
			}
			for i := range got {
				if !reflect.DeepEqual(got[i], want[i]) {
					t.Fatalf("detail mismatch: got %#v want %#v", got[i], want[i])
				}
			}
		})
	}
}

func TestAdvancedSQLiteCursorCacheAndInvalidation(t *testing.T) {
	c, svc := advancedFixture(t)
	first, err := svc.AdvancedSearch("string", "共同", "", false, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := c.advancedDisk
	if len(d.queries) != 1 {
		t.Fatal("query not cached")
	}
	_, err = svc.AdvancedSearch("string", "共同", "dir", false, first.NextCursor, 1)
	if !errors.Is(err, ErrAdvancedCursorStale) {
		t.Fatalf("changed query accepted cursor: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err = svc.AdvancedSearch("string", "共同", "", false, first.NextCursor, 1); err != nil {
			t.Fatal(err)
		}
	}
	if len(d.queries) != 1 {
		t.Fatal("paging built a new query")
	}
	// Modify the raw archive under the core lock without invalidating on purpose:
	// an existing result page must be served exclusively from the SQL snapshot.
	c.mu.Lock()
	err = c.archive.SetText(1, "[name]\n`replaced`")
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	next, err := svc.AdvancedSearch("string", "共同", "", false, first.NextCursor, 1)
	if err != nil || len(next.Hits) != 1 || next.Hits[0].Details[0].Value != "共同 beta" {
		t.Fatalf("page rescanned archive: %#v %v", next, err)
	}
	c.mu.Lock()
	c.invalidateAdvancedSearchLocked()
	c.mu.Unlock()
	if _, err = svc.AdvancedSearch("string", "共同", "", false, first.NextCursor, 1); !errors.Is(err, ErrAdvancedCursorStale) {
		t.Fatalf("stale cursor accepted: %v", err)
	}
	select {
	case <-d.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup blocked")
	}
	if _, err = os.Stat(d.dir); !os.IsNotExist(err) {
		t.Fatalf("session leaked: %v", err)
	}
	result, err := svc.AdvancedSearch("string", "replaced", "", false, 0, 20)
	if err != nil || len(result.Hits) != 1 {
		t.Fatalf("rebuild after edit: %#v %v", result, err)
	}
}

func TestAdvancedSQLiteEvictsQueries(t *testing.T) {
	c, svc := advancedFixture(t)
	first, err := svc.AdvancedSearch("string", "共同", "", false, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	var oldPath string
	for _, q := range c.advancedDisk.queries {
		oldPath = q.path
	}
	for i := 0; i < advancedQueryLimit; i++ {
		if _, err = svc.AdvancedSearch("string", fmt.Sprintf("missing-%d", i), "", false, 0, 1); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.advancedDisk.queries) != advancedQueryLimit {
		t.Fatal("wrong query cache size")
	}
	if _, err = os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("evicted result file still exists")
	}
	if _, err = svc.AdvancedSearch("string", "共同", "", false, first.NextCursor, 1); !errors.Is(err, ErrAdvancedCursorStale) {
		t.Fatalf("evicted cursor accepted: %v", err)
	}
}

func TestAdvancedSQLiteCancelWhileBuilding(t *testing.T) {
	c, svc := advancedFixture(t)
	// Hold the operation lock so the request is active but cannot start its SQL
	// work. Cancel must not wait for this lock while holding core.mu.
	ctx, cancel := context.WithCancel(context.Background())
	d := &advancedSQLite{ctx: ctx, cancel: cancel, queries: map[uint64]*advancedDiskQuery{}, closed: make(chan struct{})}
	c.mu.Lock()
	c.advancedDisk = d
	c.advancedCancel = cancel
	c.mu.Unlock()
	d.mu.Lock()

	done := make(chan struct{})
	go func() { svc.CancelAdvancedSearch(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		d.mu.Unlock()
		t.Fatal("cancel blocked on operation lock")
	}
	if d.ctx.Err() != context.Canceled {
		t.Fatal("operation context not cancelled")
	}
	d.mu.Unlock()
	select {
	case <-d.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel cleanup deadlocked")
	}
}

func TestAdvancedSQLiteSavedCacheAndCorruption(t *testing.T) {
	a := pvf.New()
	if _, err := a.AddFileText(fmt.Sprintf("unique/%d.equ", time.Now().UnixNano()), "[name]\n`cached value`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/cache.pvf"
	if err := a.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	open := func() (*core, *ArchiveService) {
		archive, err := pvf.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		c := NewCore()
		if err = c.setArchive(archive); err != nil {
			t.Fatal(err)
		}
		return c, NewArchiveService(c)
	}
	c, svc := open()
	if _, err := svc.AdvancedSearch("string", "cached value", "", false, 0, 1); err != nil {
		t.Fatal(err)
	}
	d := c.advancedDisk
	cache := d.cachePath
	if cache == "" {
		t.Fatal("clean archive not cached")
	}
	defer os.Remove(cache)
	c.closeArchive()
	<-d.closed
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("clean cache disappeared: %v", err)
	}
	c, svc = open()
	if _, err := svc.AdvancedSearch("string", "cached value", "", false, 0, 1); err != nil {
		t.Fatal(err)
	}
	if svc.AdvancedIndexStatus().Stage != "ready-cache" {
		t.Fatal("clean index not reused")
	}
	d = c.advancedDisk
	c.closeArchive()
	<-d.closed
	if err := os.WriteFile(cache, []byte("corrupt derived cache"), 0600); err != nil {
		t.Fatal(err)
	}
	c, svc = open()
	if _, err := svc.AdvancedSearch("string", "cached value", "", false, 0, 1); err != nil {
		t.Fatal(err)
	}
	if svc.AdvancedIndexStatus().Stage != "ready-sqlite" {
		t.Fatal("corrupt cache not rebuilt")
	}
	d = c.advancedDisk
	if err := NewEditorService(c).SetText(0, "[name]\n`unsaved value`"); err != nil {
		t.Fatal(err)
	}
	<-d.closed
	if _, err := svc.AdvancedSearch("string", "unsaved value", "", false, 0, 1); err != nil {
		t.Fatal(err)
	}
	d = c.advancedDisk
	if d.cachePath != "" {
		t.Fatal("unsaved index published as clean")
	}
	c.closeArchive()
	<-d.closed
}

func TestAdvancedSQLiteResultDiskLimit(t *testing.T) {
	c, svc := advancedFixture(t)
	if _, err := svc.AdvancedSearch("string", "共同", "", false, 0, 1); err != nil {
		t.Fatal(err)
	}
	d := c.advancedDisk
	d.mu.Lock()
	for len(d.queries) > 0 {
		if err := d.evictOldest(); err != nil {
			d.mu.Unlock()
			t.Fatal(err)
		}
	}
	d.queryBudget = 8192
	d.mu.Unlock()
	if _, err := svc.AdvancedSearch("string", "different query", "", false, 0, 1); err == nil {
		t.Fatal("disk limit not enforced")
	}
	if len(d.queries) != 0 {
		t.Fatal("failed query was published")
	}
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "index.db" {
		t.Fatalf("failed query leaked files: %v", entries)
	}
	// Read/edit source data remains available after a derived query failure.
	c.mu.RLock()
	text, err := c.archive.Text(0)
	c.mu.RUnlock()
	if err != nil || text == "" {
		t.Fatal("source unavailable after disk failure")
	}
}

func TestAdvancedSQLiteCloseDuringScan(t *testing.T) {
	a := pvf.New()
	for i := 0; i < 20000; i++ {
		if _, err := a.AddFileText(fmt.Sprintf("dir/%d.equ", i), "[name]\n`shared`", pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	svc := NewArchiveService(c)
	result := make(chan error, 1)
	go func() { _, err := svc.AdvancedSearch("string", "shared", "", false, 0, 20); result <- err }()
	deadline := time.Now().Add(5 * time.Second)
	var d *advancedSQLite
	for time.Now().Before(deadline) {
		c.mu.RLock()
		d = c.advancedDisk
		building := c.advancedStatus.State == AdvancedIndexStateBuilding
		c.mu.RUnlock()
		if d != nil && building {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if d == nil {
		c.closeArchive()
		t.Fatal("build did not start")
	}
	start := time.Now()
	c.closeArchive()
	if time.Since(start) > time.Second {
		t.Error("close held up by scan")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("closed build published results")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scan did not cancel")
	}
	select {
	case <-d.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("scan cleanup blocked")
	}
	if _, err := os.Stat(d.dir); !os.IsNotExist(err) {
		t.Fatal("cancelled build leaked files")
	}
}

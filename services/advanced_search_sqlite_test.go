package services

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"pvfine/internal/pvf"
)

// BenchmarkAdvancedIndexBuild measures a cold build without query or cache reuse.
func BenchmarkAdvancedIndexBuild(b *testing.B) {
	a := pvf.New()
	var script strings.Builder
	for i := 0; i < 64; i++ {
		fmt.Fprintf(&script, "{7=`value-%d`}\n", i)
	}
	for i := 0; i < 4096; i++ {
		if _, err := a.AddFileText(fmt.Sprintf("dir/%d.equ", i), script.String(), pvf.TypeScript); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		d := &advancedSQLite{ctx: ctx, cancel: cancel, closed: make(chan struct{})}
		c := &core{archive: a, advancedDisk: d}
		err := d.build(c, a)
		d.close()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestAdvancedSQLiteBatchesAcrossTransactions(t *testing.T) {
	a := pvf.New()
	raw := make([]byte, 70*10)
	for i := 0; i < 70; i++ {
		offset := a.StringOffset(fmt.Sprintf("value-%d", i))
		raw[i*10], raw[i*10+5] = 7, 3
		binary.LittleEndian.PutUint32(raw[i*10+1:], uint32(offset))
		binary.LittleEndian.PutUint32(raw[i*10+6:], uint32(offset))
	}
	for i := 0; i < 300; i++ {
		a.AddFile(fmt.Sprintf("dir/%d.equ", i), raw, pvf.TypeScript)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &advancedSQLite{ctx: ctx, cancel: cancel, closed: make(chan struct{})}
	defer d.close()
	c := &core{archive: a, advancedDisk: d}
	if err := d.build(c, a); err != nil {
		t.Fatal(err)
	}
	// Each of four shards crosses its 4,096-row transaction boundary and
	// leaves a partial final batch. Check every file's details across shards.
	if err := d.attachShards(ctx, d.db); err != nil {
		t.Fatal(err)
	}
	var parts []string
	for shard := 0; shard < d.shards; shard++ {
		parts = append(parts, fmt.Sprintf("SELECT * FROM refs%d.refs", shard))
	}
	rows, err := d.db.Query(`SELECT file_index,count(*),sum(occurrences),
 sum(fields=0 AND occurrences=2 AND types=55),sum(fields)
 FROM (` + strings.Join(parts, " UNION ALL ") + `) GROUP BY file_index ORDER BY file_index`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	files := 0
	for rows.Next() {
		var index, count, occurrences, tokens, fields int
		if err := rows.Scan(&index, &count, &occurrences, &tokens, &fields); err != nil {
			t.Fatal(err)
		}
		if index != files || count != 72 || occurrences != 142 || tokens != 70 || fields != 3 {
			t.Fatalf("file %d: refs=%d occurrences=%d tokens=%d fields=%d", index, count, occurrences, tokens, fields)
		}
		files++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if files != 300 {
		t.Fatalf("indexed %d files, want 300", files)
	}
}

// BenchmarkAdvancedIndexRealArchive opts in via PVF_TESTFILE. It measures only
// cold index construction, without opening or querying. Cache restore is timed
// separately using temporary hard links, never the application's reusable cache.
func BenchmarkAdvancedIndexRealArchive(b *testing.B) {
	benchmarkAdvancedIndexReal(b, (*advancedSQLite).build)
}

func benchmarkAdvancedIndexReal(b *testing.B, build func(*advancedSQLite, *core, *pvf.Archive) error) {
	path := os.Getenv("PVF_TESTFILE")
	if path == "" {
		b.Skip("PVF_TESTFILE required")
	}
	a, err := pvf.Open(path)
	if err != nil {
		b.Fatal("cannot open benchmark archive")
	}
	// An unused, in-memory pool entry marks the archive dirty and prevents both
	// restoring and publishing an index cache. Never save this benchmark archive.
	for i := 0; !a.Modified(); i++ {
		a.StringOffset(fmt.Sprintf("__pvfine_cold_index_benchmark_%d__", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		d := &advancedSQLite{ctx: ctx, cancel: cancel, closed: make(chan struct{})}
		c := &core{archive: a, advancedDisk: d}
		err := build(d, c, a)
		b.StopTimer()
		if err != nil {
			d.close()
			b.Fatal(err)
		}
		root := b.TempDir()
		d.cachePath = filepath.Join(root, "cache")
		if err := d.publishCache(root); err != nil {
			d.close()
			b.Fatal(err)
		}
		d.close()
		ctx, cancel = context.WithCancel(context.Background())
		warm := &advancedSQLite{ctx: ctx, cancel: cancel, dir: b.TempDir(), cachePath: d.cachePath, closed: make(chan struct{})}
		started := time.Now()
		ok := warm.restoreCache()
		b.ReportMetric(float64(time.Since(started).Microseconds())/1000, "cache-ms")
		warm.close()
		if !ok {
			b.Fatal("real archive cache could not be restored")
		}
		b.StartTimer()
	}
}

func BenchmarkAdvancedReferenceBatchSizes(b *testing.B) {
	for _, size := range []int{1, 8, 16, 32, 128} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				b.StopTimer()
				db, err := openAdvancedDB(filepath.Join(b.TempDir(), "refs.db"))
				if err != nil {
					b.Fatal(err)
				}
				if _, err = db.Exec(`CREATE TABLE refs(file_index INTEGER,offset INTEGER,occurrences INTEGER,types INTEGER,fields INTEGER,PRIMARY KEY(file_index,offset)) WITHOUT ROWID`); err != nil {
					b.Fatal(err)
				}
				tx, err := db.Begin()
				if err != nil {
					b.Fatal(err)
				}
				stmt, err := tx.Prepare(`INSERT INTO refs VALUES` + strings.TrimSuffix(strings.Repeat("(?,?,?,?,?),", size), ","))
				if err != nil {
					b.Fatal(err)
				}
				args := make([]any, size*5)
				b.StartTimer()
				for row := 0; row < 65536; row += size {
					for i := 0; i < size; i++ {
						args[i*5], args[i*5+1] = (row+i)/64, (row+i)%64
						args[i*5+2], args[i*5+3], args[i*5+4] = 1, 7, 0
					}
					if _, err = stmt.ExecContext(context.Background(), args...); err != nil {
						b.Fatal(err)
					}
				}
				if err = tx.Commit(); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				stmt.Close()
				db.Close()
			}
		})
	}
}

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

func TestAdvancedSearchFindsLocalized110USNames(t *testing.T) {
	for _, disk := range []bool{false, true} {
		t.Run(fmt.Sprint("disk=", disk), func(t *testing.T) {
			c, a, itemIndex, _ := paged110LayoutFixture(t)
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
			c.startSearchIndex()
			waitForSearchIndex(t, c)
			for _, regex := range []bool{false, true} {
				query := "白色兽语腰带"
				if regex {
					query = "^白色兽语"
				}
				result, err := NewArchiveService(c).AdvancedSearch("string", query, "equipment/character/x", regex, 0, 20)
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Hits) != 1 || result.Hits[0].FileIndex != itemIndex {
					t.Fatalf("localized results = %#v", result.Hits)
				}
				found := false
				for _, detail := range result.Hits[0].Details {
					if detail.Kind == "string" && detail.Value == "白色兽语腰带 [A款]" && reflect.DeepEqual(detail.TokenTypes, []int32{8}) {
						found = true
					}
				}
				if !found {
					t.Fatalf("localized details = %#v", result.Hits[0].Details)
				}
			}
		})
	}
}

func TestAdvancedSearchFindsLocalized110USDescriptions(t *testing.T) {
	for _, disk := range []bool{false, true} {
		t.Run(fmt.Sprint("disk=", disk), func(t *testing.T) {
			a := pvf.New()
			if _, err := a.AddFileText("list/n_string.lst", "3 `String/Equipment.uv.str`", pvf.TypeScript); err != nil {
				t.Fatal(err)
			}
			encode := func(value string) []byte {
				out := make([]byte, 0, len(value)*2)
				for _, r := range value {
					out = append(out, byte(r), byte(r>>8))
				}
				return out
			}
			a.AddFile("String/Equipment.uv.str", encode("name_1>测试装备\r\ndesc_1>技能伤害增加 10%\r\n"), pvf.TypeScript)
			if _, err := a.AddFileText("list/equipment.lst", "1 `equipment/test.equ`", pvf.TypeScript); err != nil {
				t.Fatal(err)
			}
			itemIndex, err := a.AddFileText("equipment/test.equ", "[name]\n{8=`<3::name_1>`}\n[basic explain]\n{10=`<3::desc_1>`}", pvf.TypeScript)
			if err != nil {
				t.Fatal(err)
			}
			c := NewCore()
			if err := c.setArchive(a); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(c.closeArchive)
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
			c.startSearchIndex()
			waitForSearchIndex(t, c)
			for _, query := range []string{"技能伤害", "^技能伤害增加"} {
				result, err := NewArchiveService(c).AdvancedSearch("string", query, "equipment", strings.HasPrefix(query, "^"), 0, 20)
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Hits) != 1 || result.Hits[0].FileIndex != itemIndex {
					t.Fatalf("description search %q = %#v", query, result.Hits)
				}
				found := false
				for _, detail := range result.Hits[0].Details {
					if detail.Kind == "string" && detail.Value == "技能伤害增加 10%" && reflect.DeepEqual(detail.TokenTypes, []int32{10}) {
						found = true
					}
				}
				if !found {
					t.Fatalf("description details %q = %#v", query, result.Hits[0].Details)
				}
			}
		})
	}
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
	for i := 0; i < 4; i++ {
		if _, err := a.AddFileText(fmt.Sprintf("unique/%d-%d.equ", time.Now().UnixNano(), i), "[name]\n`cached value`", pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
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
	defer os.RemoveAll(cache)
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
	if err := os.WriteFile(filepath.Join(cache, "index.db"), []byte("corrupt derived cache"), 0600); err != nil {
		t.Fatal(err)
	}
	c, svc = open()
	if _, err := svc.AdvancedSearch("string", "cached value", "", false, 0, 1); err != nil {
		t.Fatal(err)
	}
	if svc.AdvancedIndexStatus().Stage != "ready-sqlite" {
		t.Fatal("corrupt cache not rebuilt")
	}
	for _, missing := range []bool{true, false} {
		d = c.advancedDisk
		c.closeArchive()
		<-d.closed
		shard := filepath.Join(cache, advancedShardName(0))
		var err error
		if missing {
			err = os.Remove(shard)
		} else {
			err = os.WriteFile(shard, []byte("corrupt shard"), 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		c, svc = open()
		result, err := svc.AdvancedSearch("string", "cached value", "", false, 0, 10)
		if err != nil || len(result.Hits) != 4 || svc.AdvancedIndexStatus().Stage != "ready-sqlite" {
			t.Fatalf("damaged shard was not rebuilt: %v", err)
		}
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
	if len(entries) != 1+d.shards {
		t.Fatalf("failed query leaked files: %v", entries)
	}
	for _, entry := range entries {
		if entry.Name() != "index.db" && !strings.HasPrefix(entry.Name(), "refs-") {
			t.Fatalf("failed query leaked a result file: %s", entry.Name())
		}
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

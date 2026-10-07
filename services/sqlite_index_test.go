package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
	"pvfine/internal/pvf"
)

type sqliteBackupTestError int

func (e sqliteBackupTestError) Error() string { return fmt.Sprintf("SQLite code %d", e) }
func (e sqliteBackupTestError) Code() int     { return int(e) }

func TestStepSQLiteBackupRetriesLocks(t *testing.T) {
	for _, code := range []int{sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED, sqlite3.SQLITE_BUSY_RECOVERY, sqlite3.SQLITE_LOCKED_SHAREDCACHE} {
		for _, wantMore := range []bool{false, true} {
			t.Run(fmt.Sprintf("code=%d/more=%t", code, wantMore), func(t *testing.T) {
				calls := 0
				more, err := stepSQLiteBackup(context.Background(), func(pages int32) (bool, error) {
					if pages != 512 {
						t.Fatalf("pages = %d, want 512", pages)
					}
					calls++
					if calls <= 2 {
						return false, fmt.Errorf("backup: %w", sqliteBackupTestError(code))
					}
					return wantMore, nil
				})
				if err != nil || more != wantMore || calls != 3 {
					t.Fatalf("more = %t, err = %v, calls = %d", more, err, calls)
				}
			})
		}
	}
}

func TestStepSQLiteBackupDoesNotRetryOtherErrors(t *testing.T) {
	for _, want := range []error{sqliteBackupTestError(sqlite3.SQLITE_IOERR), errors.New("database is locked")} {
		calls := 0
		_, err := stepSQLiteBackup(context.Background(), func(int32) (bool, error) {
			calls++
			return false, want
		})
		if !errors.Is(err, want) || calls != 1 {
			t.Fatalf("err = %v, calls = %d, want %v on first call", err, calls, want)
		}
	}
}

func TestStepSQLiteBackupCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, err := stepSQLiteBackup(ctx, func(int32) (bool, error) {
		calls++
		cancel()
		return false, sqliteBackupTestError(sqlite3.SQLITE_BUSY_RECOVERY)
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
	_, err = stepSQLiteBackup(ctx, func(int32) (bool, error) {
		t.Fatal("cancelled backup called Step")
		return false, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want cancellation", err)
	}
}

func TestStepSQLiteBackupLockTimeout(t *testing.T) {
	want := sqliteBackupTestError(sqlite3.SQLITE_BUSY_RECOVERY)
	calls := 0
	started := time.Now()
	_, err := stepSQLiteBackup(context.Background(), func(int32) (bool, error) {
		calls++
		return false, want
	})
	if !errors.Is(err, want) || calls < 2 || time.Since(started) < 5*time.Second {
		t.Fatalf("err = %v, calls = %d, elapsed = %s", err, calls, time.Since(started))
	}
}

func TestBackupSQLiteDatabaseWaitsForSourceLock(t *testing.T) {
	dir := t.TempDir()
	source, err := sql.Open("sqlite", filepath.Join(dir, "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := source.Exec("CREATE TABLE data(value TEXT); INSERT INTO data VALUES('original')"); err != nil {
		t.Fatal(err)
	}
	// Pin the locked writer so the backup must acquire a separate connection.
	writer, err := source.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.ExecContext(context.Background(), "BEGIN EXCLUSIVE; UPDATE data SET value='committed'"); err != nil {
		t.Fatal(err)
	}
	defer writer.ExecContext(context.Background(), "ROLLBACK")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	target := filepath.Join(dir, "backup.db")
	done := make(chan error, 1)
	go func() { done <- backupSQLiteDatabase(ctx, source, target) }()
	select {
	case err := <-done:
		t.Fatalf("backup returned before source lock release: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	_, commitErr := writer.ExecContext(context.Background(), "COMMIT")
	backupErr := <-done
	if commitErr != nil {
		t.Fatal(commitErr)
	}
	if backupErr != nil {
		t.Fatal(backupErr)
	}
	snapshot, err := sql.Open("sqlite", target)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	var value string
	if err := snapshot.QueryRow("SELECT value FROM data").Scan(&value); err != nil || value != "committed" {
		t.Fatalf("backup value = %q, err = %v", value, err)
	}
}

func TestSQLiteWildcardSearchBeyondCandidateBatch(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := buildSQLiteFileIndex(db, pvf.New(), "wildcard-pagination"); err != nil {
		t.Fatal(err)
	}
	// More non-matching records than the old candidate batch for a 200-hit page.
	if _, err := db.Exec(`WITH RECURSIVE seq(n) AS (
		SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<6500
	) INSERT INTO records(file_index,name,lower_name,record_id,lower_id,path,lower_path,category,list_path,size,data_type)
	SELECT n,'','','','','misc/'||n||'.txt','misc/'||n||'.txt','file','',0,0 FROM seq`); err != nil {
		t.Fatal(err)
	}
	want := []string{"equipment/character/项.equ", "equipment/character/链.equ"}
	for n, path := range want {
		if _, err := db.Exec(`INSERT INTO records(file_index,name,lower_name,record_id,lower_id,path,lower_path,category,list_path,size,data_type)
		VALUES(?,'','','','',?,?,'file','',0,0)`, 6501+n, path, path); err != nil {
			t.Fatal(err)
		}
	}
	index := &sqliteArchiveIndex{db: db}
	for _, exact := range []bool{false, true} {
		for _, pattern := range []string{"*.equ", "EQUIPMENT/*/?.EQU"} {
			for _, limit := range []int{1, 200} {
				t.Run(fmt.Sprintf("%s/exact=%t/limit=%d", pattern, exact, limit), func(t *testing.T) {
					cursor := 0
					var paths []string
					for page := 0; ; page++ {
						if page > len(want) {
							t.Fatal("pagination did not terminate")
						}
						result, err := index.search(pattern, cursor, limit, exact)
						if err != nil {
							t.Fatal(err)
						}
						if len(result.Hits) == 0 && result.NextCursor >= 0 {
							t.Fatal("empty intermediate wildcard page")
						}
						for _, hit := range result.Hits {
							paths = append(paths, hit.Path)
						}
						if result.NextCursor < 0 {
							break
						}
						if result.NextCursor <= cursor {
							t.Fatal("cursor did not advance")
						}
						cursor = result.NextCursor
					}
					if len(paths) != len(want) {
						t.Fatalf("paths = %v, want %v", paths, want)
					}
					for n := range want {
						if paths[n] != want[n] {
							t.Fatalf("paths = %v, want %v", paths, want)
						}
					}
				})
			}
		}
	}
	result, err := index.search("*.missing", 0, 200, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 0 || result.NextCursor != -1 {
		t.Fatalf("unmatched wildcard result = %#v", result)
	}
}

func TestSQLiteArchiveIndexQueries(t *testing.T) {
	a := pvf.New()
	a.AddFile("equip/a.equ", []byte("[name]\n`A`"), pvf.TypeScript)
	a.AddFile("equip/b.equ", []byte("[name]\n`B`"), pvf.TypeScript)
	a.AddFile("text/readme.str", []byte{0}, pvf.TypeUnicode)

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := configureArchiveIndex(db, true); err != nil {
		t.Fatal(err)
	}
	if err := buildSQLiteFileIndex(db, a, "test"); err != nil {
		t.Fatal(err)
	}
	index := &sqliteArchiveIndex{db: db}
	roots, err := index.children("")
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range roots {
		if root.Path == "" {
			t.Fatal("root sentinel leaked into children")
		}
	}
	if _, err := index.children("equip"); err != nil {
		t.Fatal(err)
	}
	node, err := index.resolve("equip/a.equ")
	if err != nil || node == nil || node.FileIndex != 0 {
		t.Fatalf("resolve = %#v, %v", node, err)
	}
	if _, _, err := index.buildSemantic(context.Background(), NewCore(), a, nil, 1); err != nil {
		t.Fatal(err)
	}
	result, err := index.search("equip", 0, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 2 {
		t.Fatalf("search hits = %d, want 2", len(result.Hits))
	}
}

func assertSQLiteFilePathQueryUsesIndex(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT name,path,file_index,size,data_type FROM files WHERE path=? ORDER BY file_index LIMIT 1`, "equip/missing.equ")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	detail := strings.Join(plan, "\n")
	if !strings.Contains(detail, "SEARCH files USING INDEX files_path") ||
		strings.Contains(detail, "SCAN files") || strings.Contains(detail, "TEMP B-TREE") {
		t.Fatalf("path query must use indexed lookup without scanning or sorting: %s", detail)
	}
}

func TestSQLiteFilePathIndexQueriesAndBatchResolution(t *testing.T) {
	a := pvf.New()
	for i := 0; i < 100; i++ {
		if _, err := a.AddFileText(fmt.Sprintf("equip/%03d.equ", i), "[name]\n`A`", pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	index, _, err := openSQLiteArchiveIndex(a)
	if err != nil {
		t.Fatal(err)
	}
	defer index.close()
	assertSQLiteFilePathQueryUsesIndex(t, index.db)
	node, err := index.resolve("equip/missing.equ")
	if err != nil || node != nil {
		t.Fatalf("missing path = %#v, %v", node, err)
	}
	// Duplicate archive paths remain legal; resolve must choose the lowest index.
	if _, err := index.db.Exec(`INSERT INTO files
SELECT 100,path,lower_path,name,lower_name,parent,size,data_type,change_kind FROM files WHERE file_index=99`); err != nil {
		t.Fatal(err)
	}
	node, err = index.resolve("equip/099.equ")
	if err != nil || node == nil || node.FileIndex != 99 {
		t.Fatalf("duplicate path = %#v, %v", node, err)
	}
	c := NewCore()
	c.archive, c.diskIndex = a, index
	paths := []string{"equip/missing.equ", "equip/099.equ"}
	for i := 0; i < 40; i++ {
		paths = append(paths, fmt.Sprintf("equip/%03d.equ", i))
	}
	paths = append(paths, "equip/099.equ")
	nodes, err := NewArchiveService(c).ResolveFiles(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 41 || nodes[0].FileIndex != 99 || nodes[40].FileIndex != 39 {
		t.Fatalf("batch resolution returned unexpected results: %#v", nodes)
	}
}

func TestSQLiteFilePathIndexUpgradesCachedArchive(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("LocalAppData", cacheDir)
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("HOME", cacheDir)
	a := pvf.New()
	if _, err := a.AddFileText("equip/a.equ", "[name]\n`A`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cache.pvf")
	if err := a.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	a, err := pvf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	index, hit, err := openSQLiteArchiveIndex(a)
	if err != nil {
		t.Fatal(err)
	}
	if hit {
		index.close()
		t.Fatal("fresh archive unexpectedly hit the cache")
	}
	// Simulate a complete cache written before the path index was introduced.
	if _, err := index.db.Exec(`DROP INDEX files_path;
INSERT INTO records(file_index,name,lower_name,record_id,lower_id,path,lower_path,category,list_path,size,data_type)
VALUES(0,'preserved','preserved','42','42','equip/a.equ','equip/a.equ','equipment','equip/equipment.lst',0,1);
UPDATE meta SET value='1' WHERE key='complete';`); err != nil {
		index.close()
		t.Fatal(err)
	}
	index.close()
	for pass := 0; pass < 2; pass++ {
		index, hit, err = openSQLiteArchiveIndex(a)
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer index.close()
			if !hit || !index.ready {
				t.Fatal("upgrade must preserve the complete semantic cache")
			}
			assertSQLiteFilePathQueryUsesIndex(t, index.db)
			var name string
			if err := index.db.QueryRow("SELECT name FROM records WHERE record_id='42'").Scan(&name); err != nil || name != "preserved" {
				t.Fatalf("semantic cache changed: name=%q error=%v", name, err)
			}
			node, err := index.resolve("equip/a.equ")
			if err != nil || node == nil || node.FileIndex != 0 {
				t.Fatalf("resolve after upgrade = %#v, %v", node, err)
			}
		}()
	}
}

func TestSQLiteFilePathIndexUpgradeCancellation(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := buildSQLiteFileIndex(db, pvf.New(), "cancel-path-index"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP INDEX files_path"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ensureSQLiteFilePathIndex(ctx, db); !errors.Is(err, context.Canceled) {
		t.Fatalf("upgrade error = %v, want cancellation", err)
	}
	if err := ensureSQLiteFilePathIndex(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertSQLiteFilePathQueryUsesIndex(t, db)
}

func TestSQLiteArchiveIndexPublishesSemanticCandidate(t *testing.T) {
	a := pvf.New()
	a.AddFile("equip/a.equ", []byte("[name]\n`A`"), pvf.TypeScript)

	index, _, err := openSQLiteArchiveIndex(a)
	if err != nil {
		t.Fatal(err)
	}
	defer index.close()
	c := NewCore()
	c.mu.Lock()
	c.archive = a
	c.diskIndex = index
	c.indexGen = 1
	c.mu.Unlock()
	oldDB := index.db
	if _, _, err := index.buildSemantic(context.Background(), c, a, nil, 1); err != nil {
		t.Fatal(err)
	}
	if index.db == oldDB {
		t.Fatal("semantic rebuild reused the old SQLite connection")
	}
	if result, err := index.search("equip", 0, 10, false); err != nil || len(result.Hits) != 1 {
		t.Fatalf("search after reopen = %#v, %v", result, err)
	}
}

func TestSQLiteArchiveIndexConfiguresEveryConnection(t *testing.T) {
	a := pvf.New()
	a.AddFile("equip/a.equ", []byte("[name]\n`A`"), pvf.TypeScript)
	index, _, err := openSQLiteArchiveIndex(a)
	if err != nil {
		t.Fatal(err)
	}
	defer index.close()
	checkConnections := func(t *testing.T) {
		for n := 0; n < 3; n++ {
			conn, err := index.db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			// Keep each connection pinned until all pool slots are checked.
			defer conn.Close()
			for pragma, want := range map[string]int{
				"busy_timeout": 5000,
				"foreign_keys": 1,
				"temp_store":   1,
				"cache_size":   -8192,
				"mmap_size":    0,
				"synchronous":  1,
			} {
				var got int
				if err := conn.QueryRowContext(context.Background(), "PRAGMA "+pragma).Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Errorf("connection %d: %s = %d, want %d", n, pragma, got, want)
				}
			}
		}
	}
	t.Run("opened", checkConnections)
	c := NewCore()
	c.archive = a
	c.diskIndex = index
	c.indexGen = 1
	if _, _, err := index.buildSemantic(context.Background(), c, a, nil, 1); err != nil {
		t.Fatal(err)
	}
	t.Run("published", checkConnections)
}

func TestSQLiteArchiveIndexNewReaderWaitsForLock(t *testing.T) {
	db, err := openArchiveIndexDatabase(filepath.Join(t.TempDir(), "index.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := configureArchiveIndex(db, false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE data(value TEXT); INSERT INTO data VALUES('original')"); err != nil {
		t.Fatal(err)
	}
	writer, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.ExecContext(context.Background(), "BEGIN EXCLUSIVE; UPDATE data SET value='committed'"); err != nil {
		t.Fatal(err)
	}
	defer writer.ExecContext(context.Background(), "ROLLBACK")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		var value string
		err := db.QueryRowContext(ctx, "SELECT value FROM data").Scan(&value)
		if err == nil && value != "committed" {
			err = fmt.Errorf("value = %q, want committed", value)
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("reader returned before source lock release: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	_, commitErr := writer.ExecContext(context.Background(), "COMMIT")
	queryErr := <-done
	if commitErr != nil {
		t.Fatal(commitErr)
	}
	if queryErr != nil {
		t.Fatal(queryErr)
	}
}

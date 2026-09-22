package services

// The large-archive index is deliberately kept outside the pvf package. The
// parser remains useful on its own, while the application can choose a disk
// backed projection when an archive contains hundreds of thousands of files.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	sqlite "modernc.org/sqlite"
	"pvfine/internal/pvf"
)

const largeArchiveIndexThreshold = 100000

type sqliteArchiveIndex struct {
	dbMu     sync.RWMutex
	db       *sql.DB
	path     string
	identity string
	ready    bool
	dirty    bool
	tempDir  string
}

func (i *sqliteArchiveIndex) refreshFileMetadata(a *pvf.Archive, indexes map[int32]struct{}) error {
	if i == nil || len(indexes) == 0 {
		return nil
	}
	i.dbMu.Lock()
	defer i.dbMu.Unlock()
	if i.db == nil {
		return nil
	}
	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`UPDATE files SET size=?,data_type=?,change_kind=? WHERE file_index=?`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for index := range indexes {
		if index < 0 || index >= a.FileCount() {
			continue
		}
		file := a.File(index)
		if _, err := stmt.Exec(file.DataSize, file.DataType, archiveChangeKind(a, index), index); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (i *sqliteArchiveIndex) close() {
	if i == nil {
		return
	}
	i.dbMu.Lock()
	defer i.dbMu.Unlock()
	if i.db == nil {
		return
	}
	_ = i.db.Close()
	i.db = nil
	if i.tempDir != "" {
		_ = os.RemoveAll(i.tempDir)
		i.tempDir = ""
	}
}

func configureArchiveIndex(db *sql.DB, writable bool) error {
	// Keep the pool bounded. A semantic rebuild uses one transaction while
	// directory/search requests may hold a small number of read connections.
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(3)
	pragmas := []string{
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA temp_store=FILE",
		"PRAGMA cache_size=-8192",
		"PRAGMA mmap_size=0",
	}
	if writable {
		pragmas = append(pragmas, "PRAGMA journal_mode=OFF", "PRAGMA synchronous=OFF")
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return err
		}
	}
	return nil
}

type sqliteBackuper interface {
	NewBackup(string) (*sqlite.Backup, error)
}

// backupSQLiteDatabase creates a consistent disk snapshot without taking the
// write lock needed to switch the live database's journal mode. The snapshot
// becomes the rebuild candidate; readers continue using the old database.
func backupSQLiteDatabase(ctx context.Context, source *sql.DB, target string) error {
	if source == nil {
		return errors.New("SQLite 索引连接已关闭")
	}
	_ = os.Remove(target)
	conn, err := source.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Raw(func(driverConn any) error {
		backuper, ok := driverConn.(sqliteBackuper)
		if !ok {
			return errors.New("SQLite 驱动不支持在线备份")
		}
		backup, err := backuper.NewBackup(target)
		if err != nil {
			return err
		}
		finished := false
		defer func() {
			if !finished {
				_ = backup.Finish()
			}
		}()
		lastLoggedPage := 0
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			more, err := backup.Step(512)
			if err != nil {
				return err
			}
			pageCount := backup.PageCount()
			if pageCount-lastLoggedPage >= 16384 {
				log.Printf("[pvfine:index] sqlite phase=backup progress pages=%d/%d", pageCount, pageCount+backup.Remaining())
				lastLoggedPage = pageCount
			}
			if !more {
				break
			}
		}
		finished = true
		return backup.Finish()
	})
}

func (i *sqliteArchiveIndex) prepareSemanticCandidate(ctx context.Context, c *core, a *pvf.Archive, gen uint64) (*sql.DB, string, error) {
	if c == nil {
		return nil, "", nil
	}
	c.mu.RLock()
	if c.archive == nil {
		c.mu.RUnlock()
		return nil, "", nil
	}
	current := c.archive == a && c.indexGen == gen && c.diskIndex == i
	c.mu.RUnlock()
	if !current {
		return nil, "", context.Canceled
	}
	dir := filepath.Dir(i.path)
	tmp, err := os.CreateTemp(dir, ".semantic-*.db")
	if err != nil {
		return nil, "", err
	}
	candidatePath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(candidatePath)
		return nil, "", err
	}
	_ = os.Remove(candidatePath)
	i.dbMu.RLock()
	source := i.db
	if source == nil {
		i.dbMu.RUnlock()
		_ = os.Remove(candidatePath)
		return nil, "", errors.New("SQLite 索引连接已关闭")
	}
	backupStartedAt := time.Now()
	log.Printf("[pvfine:index] sqlite phase=backup started")
	if err := backupSQLiteDatabase(ctx, source, candidatePath); err != nil {
		i.dbMu.RUnlock()
		_ = os.Remove(candidatePath)
		return nil, "", err
	}
	i.dbMu.RUnlock()
	candidateDB, err := sql.Open("sqlite", candidatePath)
	if err != nil {
		_ = os.Remove(candidatePath)
		return nil, "", err
	}
	if err := configureArchiveIndex(candidateDB, true); err != nil {
		_ = candidateDB.Close()
		_ = os.Remove(candidatePath)
		return nil, "", err
	}
	if _, err := candidateDB.Exec("PRAGMA cache_size=-65536; PRAGMA locking_mode=EXCLUSIVE"); err != nil {
		_ = candidateDB.Close()
		_ = os.Remove(candidatePath)
		return nil, "", err
	}
	var journalMode, synchronous string
	_ = candidateDB.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	_ = candidateDB.QueryRow("PRAGMA synchronous").Scan(&synchronous)
	log.Printf("[pvfine:index] sqlite candidate settings: journal=%s synchronous=%s", journalMode, synchronous)
	c.mu.RLock()
	stillCurrent := c.archive == a && c.indexGen == gen && c.diskIndex == i
	c.mu.RUnlock()
	if !stillCurrent || ctx.Err() != nil {
		_ = candidateDB.Close()
		_ = os.Remove(candidatePath)
		return nil, "", context.Canceled
	}
	log.Printf("[pvfine:index] sqlite phase=backup finished elapsed=%s", time.Since(backupStartedAt).Round(time.Millisecond))
	return candidateDB, candidatePath, nil
}

func (i *sqliteArchiveIndex) publishSemanticCandidate(c *core, a *pvf.Archive, gen uint64, candidateDB *sql.DB, candidatePath string) error {
	if candidateDB == nil || candidatePath == "" {
		return nil
	}
	if err := candidateDB.Close(); err != nil {
		return err
	}
	startedAt := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.archive != a || c.indexGen != gen || c.diskIndex != i {
		return context.Canceled
	}
	i.dbMu.Lock()
	defer i.dbMu.Unlock()
	oldDB := i.db
	if oldDB != nil {
		_ = oldDB.Close()
	}
	dir := filepath.Dir(i.path)
	oldPath := ""
	if _, err := os.Stat(i.path); err == nil {
		tmp, err := os.CreateTemp(dir, ".index-old-*.db")
		if err != nil {
			return err
		}
		oldPath = tmp.Name()
		if err := tmp.Close(); err != nil {
			_ = os.Remove(oldPath)
			return err
		}
		_ = os.Remove(oldPath)
		if err := os.Rename(i.path, oldPath); err != nil {
			return err
		}
	}
	_ = os.Remove(i.path + "-wal")
	_ = os.Remove(i.path + "-shm")
	if err := os.Rename(candidatePath, i.path); err != nil {
		if oldPath != "" {
			_ = os.Rename(oldPath, i.path)
		}
		return err
	}
	newDB, err := sql.Open("sqlite", i.path)
	if err == nil {
		err = configureArchiveIndex(newDB, false)
	}
	if err == nil {
		err = enableArchiveIndexWAL(newDB)
	}
	if err != nil {
		if newDB != nil {
			_ = newDB.Close()
		}
		_ = os.Remove(i.path)
		if oldPath != "" {
			_ = os.Rename(oldPath, i.path)
		}
		if restored, restoreErr := sql.Open("sqlite", i.path); restoreErr == nil && configureArchiveIndex(restored, false) == nil {
			i.db = restored
		} else if restoreErr == nil {
			_ = restored.Close()
		}
		return err
	}
	if oldPath != "" {
		_ = os.Remove(oldPath)
	}
	i.db = newDB
	log.Printf("[pvfine:index] sqlite phase=publish elapsed=%s", time.Since(startedAt).Round(time.Millisecond))
	return nil
}

func archiveIndexIdentity(a *pvf.Archive) (string, string, error) {
	path := a.SourcePath()
	if path == "" {
		return "", "", errors.New("归档没有源路径")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "metadata:%d\x00", searchIndexCacheVersion)
	fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d\x00%d\x00%d", filepath.Clean(path), info.Size(), info.ModTime().UnixNano(), a.FileCount(), a.Header().GroupCount, a.Header().BodySize)
	identity := hex.EncodeToString(h.Sum(nil))
	return identity, filepath.Clean(path), nil
}

func archiveIndexCachePath(a *pvf.Archive) (string, string, error) {
	identity, _, err := archiveIndexIdentity(a)
	if err != nil {
		return "", "", err
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(root, "pvfine", "archive-index")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	return filepath.Join(dir, identity+".db"), identity, nil
}

func openSQLiteArchiveIndex(a *pvf.Archive) (*sqliteArchiveIndex, bool, error) {
	tempDir := ""
	path, identity, err := archiveIndexCachePath(a)
	if err != nil {
		// Synthetic archives and unusual test files still benefit from a disk
		// index. Keep them in a temporary file that is removed on close.
		dir, tempErr := os.MkdirTemp("", "pvfine-index-")
		if tempErr != nil {
			return nil, false, err
		}
		path = filepath.Join(dir, "index.db")
		identity = path
		tempDir = dir
	}

	if !a.Modified() {
		if db, openErr := sql.Open("sqlite", path); openErr == nil {
			if configureArchiveIndex(db, false) == nil && sqliteIndexComplete(db, identity) {
				_ = enableArchiveIndexWAL(db)
				log.Printf("[pvfine:index] sqlite file index cache hit: files=%d", a.FileCount())
				return &sqliteArchiveIndex{db: db, path: path, identity: identity, ready: true}, true, nil
			}
			_ = db.Close()
		}
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".index-*.db")
	if err != nil {
		return nil, false, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(tmpPath)
	db, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		return nil, false, err
	}
	if err := configureArchiveIndex(db, true); err != nil {
		_ = db.Close()
		_ = os.Remove(tmpPath)
		return nil, false, err
	}
	idx := &sqliteArchiveIndex{db: db, path: path, identity: identity, tempDir: tempDir}
	fileIndexStartedAt := time.Now()
	log.Printf("[pvfine:index] sqlite file index rebuild started: files=%d", a.FileCount())
	if err := buildSQLiteFileIndex(db, a, identity); err != nil {
		log.Printf("[pvfine:index] sqlite file index rebuild failed: elapsed=%s error=%v", time.Since(fileIndexStartedAt).Round(time.Millisecond), err)
		idx.close()
		_ = os.Remove(tmpPath)
		return nil, false, err
	}
	log.Printf("[pvfine:index] sqlite file index rebuild finished: files=%d elapsed=%s", a.FileCount(), time.Since(fileIndexStartedAt).Round(time.Millisecond))
	if err := db.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, false, err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(path)
		if err := os.Rename(tmpPath, path); err != nil {
			_ = os.Remove(tmpPath)
			return nil, false, err
		}
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		return nil, false, err
	}
	if err := configureArchiveIndex(db, false); err != nil {
		_ = db.Close()
		return nil, false, err
	}
	_ = enableArchiveIndexWAL(db)
	idx.db = db
	pruneArchiveIndexCache(filepath.Dir(path), path)
	return idx, false, nil
}

func enableArchiveIndexWAL(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return err
	}
	_, err := db.Exec("PRAGMA synchronous=NORMAL")
	return err
}

func pruneArchiveIndexCache(dir, current string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type cacheFile struct {
		path string
		size int64
		mod  int64
	}
	files := make([]cacheFile, 0, len(entries))
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".db" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		files = append(files, cacheFile{path: path, size: info.Size(), mod: info.ModTime().UnixNano()})
		total += info.Size()
	}
	const maxCacheBytes = int64(2 << 30)
	if total <= maxCacheBytes {
		return
	}
	sort.Slice(files, func(left, right int) bool { return files[left].mod < files[right].mod })
	for _, file := range files {
		if file.path == current || total <= maxCacheBytes {
			break
		}
		if os.Remove(file.path) == nil {
			total -= file.size
		}
	}
}

func sqliteIndexComplete(db *sql.DB, identity string) bool {
	var value string
	if err := db.QueryRow("SELECT value FROM meta WHERE key='identity'").Scan(&value); err != nil {
		return false
	}
	var complete string
	if err := db.QueryRow("SELECT value FROM meta WHERE key='complete'").Scan(&complete); err != nil {
		return false
	}
	return value == identity && complete == "1"
}

func buildSQLiteFileIndex(db *sql.DB, a *pvf.Archive, identity string) error {
	fileIndexProgressStartedAt := time.Now()
	const schema = `
CREATE TABLE meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE files(
 file_index INTEGER PRIMARY KEY, path TEXT NOT NULL, lower_path TEXT NOT NULL,
 name TEXT NOT NULL, lower_name TEXT NOT NULL, parent TEXT NOT NULL,
 size INTEGER NOT NULL, data_type INTEGER NOT NULL, change_kind TEXT NOT NULL
);
CREATE TABLE dirs(path TEXT PRIMARY KEY, parent TEXT NOT NULL, name TEXT NOT NULL, child_count INTEGER NOT NULL DEFAULT 0);
CREATE TABLE records(
 id INTEGER PRIMARY KEY AUTOINCREMENT, file_index INTEGER NOT NULL,
 name TEXT NOT NULL, lower_name TEXT NOT NULL, record_id TEXT NOT NULL,
 lower_id TEXT NOT NULL, path TEXT NOT NULL, lower_path TEXT NOT NULL,
 category TEXT NOT NULL, list_path TEXT NOT NULL, size INTEGER NOT NULL,
 data_type INTEGER NOT NULL
);
CREATE TABLE tags(file_index INTEGER NOT NULL, tag_id TEXT NOT NULL, name TEXT NOT NULL, category TEXT NOT NULL,
 PRIMARY KEY(file_index, tag_id, category));
CREATE TABLE visuals(file_index INTEGER PRIMARY KEY, icon_path TEXT NOT NULL, icon_index INTEGER NOT NULL,
 field_path TEXT NOT NULL, field_index INTEGER NOT NULL);
INSERT INTO dirs(path,parent,name,child_count) VALUES('','','',0);
INSERT INTO meta(key,value) VALUES('schema','1'),('identity',?),('complete','0');
`
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(schema, identity); err != nil {
		_ = tx.Rollback()
		return err
	}
	fileStmt, err := tx.Prepare(`INSERT INTO files(file_index,path,lower_path,name,lower_name,parent,size,data_type,change_kind) VALUES(?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	dirStmt, err := tx.Prepare(`INSERT OR IGNORE INTO dirs(path,parent,name,child_count) VALUES(?,?,?,0)`)
	if err != nil {
		_ = fileStmt.Close()
		_ = tx.Rollback()
		return err
	}
	knownDirs := make(map[string]struct{}, 1024)
	for index := int32(0); index < a.FileCount(); index++ {
		path := a.Path(index)
		if path == "" {
			continue
		}
		file := a.File(index)
		parent, name := splitParent(path)
		if _, err := fileStmt.Exec(index, path, strings.ToLower(path), name, strings.ToLower(name), parent, file.DataSize, file.DataType, archiveChangeKind(a, index)); err != nil {
			_ = dirStmt.Close()
			_ = fileStmt.Close()
			_ = tx.Rollback()
			return err
		}
		for current := parent; current != ""; {
			upper, dirName := splitParent(current)
			if _, known := knownDirs[current]; !known {
				if _, err := dirStmt.Exec(current, upper, dirName); err != nil {
					_ = dirStmt.Close()
					_ = fileStmt.Close()
					_ = tx.Rollback()
					return err
				}
				knownDirs[current] = struct{}{}
			}
			current = upper
		}
		if index > 0 && index%100000 == 0 {
			log.Printf("[pvfine:index] sqlite file index progress: files=%d/%d elapsed=%s", index, a.FileCount(), time.Since(fileIndexProgressStartedAt).Round(time.Millisecond))
		}
	}
	_ = dirStmt.Close()
	_ = fileStmt.Close()
	if _, err := tx.Exec(`CREATE INDEX files_parent_name ON files(parent,name,file_index);
CREATE INDEX files_lower_path ON files(lower_path);
CREATE INDEX dirs_parent_name ON dirs(parent,name);`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`UPDATE dirs SET child_count=(SELECT COUNT(*) FROM files WHERE parent=dirs.path)+(SELECT COUNT(*) FROM dirs child WHERE child.parent=dirs.path)`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX records_lower_path ON records(lower_path);
CREATE INDEX records_lower_name ON records(lower_name);
CREATE INDEX records_lower_id ON records(lower_id);
CREATE INDEX records_file ON records(file_index,id);
CREATE INDEX tags_file ON tags(file_index);`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (i *sqliteArchiveIndex) buildSemantic(ctx context.Context, c *core, a *pvf.Archive, specs []searchableListSpec, gen uint64) (int, int, error) {
	startedAt := time.Now()
	log.Printf("[pvfine:index] sqlite semantic rebuild begin: generation=%d files=%d specs=%d", gen, a.FileCount(), len(specs))
	// Build into a complete candidate database so the live index remains
	// readable and SQLite never has to make a large reader/writer transaction
	// share the same file. The candidate is atomically published after commit.
	workDB := i.db
	candidateDB, candidatePath, candidateErr := i.prepareSemanticCandidate(ctx, c, a, gen)
	if candidateErr != nil {
		if errors.Is(candidateErr, context.Canceled) || ctx.Err() != nil {
			log.Printf("[pvfine:index] sqlite semantic rebuild cancelled before candidate: elapsed=%s", time.Since(startedAt).Round(time.Millisecond))
			return 0, 0, context.Canceled
		}
		log.Printf("[pvfine:index] sqlite phase=backup failed: elapsed=%s error=%v", time.Since(startedAt).Round(time.Millisecond), candidateErr)
		return 0, 0, candidateErr
	}
	candidatePublished := false
	if candidateDB != nil {
		workDB = candidateDB
	}
	fastMode := candidateDB == nil
	if candidateDB != nil {
		log.Printf("[pvfine:index] sqlite phase=build-candidate mode=fast")
	}
	defer func() {
		if candidateDB != nil && !candidatePublished {
			_ = candidateDB.Close()
			_ = os.Remove(candidatePath)
		}
		if candidateDB == nil && fastMode {
			restoreStartedAt := time.Now()
			if _, err := i.db.Exec("PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL"); err != nil {
				log.Printf("[pvfine:index] sqlite phase=restore-journal failed elapsed=%s error=%v", time.Since(restoreStartedAt).Round(time.Millisecond), err)
				return
			}
			log.Printf("[pvfine:index] sqlite phase=restore-journal elapsed=%s total=%s", time.Since(restoreStartedAt).Round(time.Millisecond), time.Since(startedAt).Round(time.Millisecond))
		}
	}()
	if candidateDB == nil {
		// Direct unit tests may call buildSemantic without installing the archive
		// into a core. Preserve that path while still reporting lock failures.
		pragmaStartedAt := time.Now()
		if _, pragmaErr := workDB.Exec("PRAGMA journal_mode=OFF; PRAGMA synchronous=OFF"); pragmaErr == nil {
			log.Printf("[pvfine:index] sqlite phase=disable-journal mode=fast elapsed=%s", time.Since(pragmaStartedAt).Round(time.Millisecond))
		} else {
			fastMode = false
			log.Printf("[pvfine:index] sqlite phase=disable-journal mode=normal elapsed=%s error=%v", time.Since(pragmaStartedAt).Round(time.Millisecond), pragmaErr)
		}
	}
	tx, err := workDB.Begin()
	if err != nil {
		log.Printf("[pvfine:index] sqlite semantic rebuild failed before transaction: elapsed=%s error=%v", time.Since(startedAt).Round(time.Millisecond), err)
		return 0, 0, err
	}
	count, skipped := 0, 0
	rollback := func(e error) (int, int, error) {
		_ = tx.Rollback()
		if e != nil {
			log.Printf("[pvfine:index] sqlite semantic rebuild rolled back: records=%d skipped=%d elapsed=%s error=%v", count, skipped, time.Since(startedAt).Round(time.Millisecond), e)
		}
		return 0, 0, e
	}
	// Secondary search indexes are rebuilt once after the bulk load. Keeping
	// them maintained for every semantic row makes a forced rebuild several
	// times slower on million-file archives.
	resetStartedAt := time.Now()
	if _, err := tx.Exec(`DROP INDEX IF EXISTS records_lower_path; DROP INDEX IF EXISTS records_lower_name; DROP INDEX IF EXISTS records_lower_id; DROP INDEX IF EXISTS records_file; DROP INDEX IF EXISTS tags_file`); err != nil {
		return rollback(err)
	}
	if _, err := tx.Exec(`DELETE FROM records; DELETE FROM tags; DELETE FROM visuals; UPDATE meta SET value='0' WHERE key='complete'`); err != nil {
		return rollback(err)
	}
	log.Printf("[pvfine:index] sqlite phase=reset elapsed=%s", time.Since(resetStartedAt).Round(time.Millisecond))
	recordStmt, err := tx.Prepare(`INSERT INTO records(file_index,name,lower_name,record_id,lower_id,path,lower_path,category,list_path,size,data_type) VALUES(?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return rollback(err)
	}
	tagStmt, err := tx.Prepare(`INSERT OR REPLACE INTO tags(file_index,tag_id,name,category) VALUES(?,?,?,?)`)
	if err != nil {
		_ = recordStmt.Close()
		return rollback(err)
	}
	visualStmt, err := tx.Prepare(`INSERT OR REPLACE INTO visuals(file_index,icon_path,icon_index,field_path,field_index) VALUES(?,?,?,?,?)`)
	if err != nil {
		_ = tagStmt.Close()
		_ = recordStmt.Close()
		return rollback(err)
	}
	npcNames := make(map[string]string)
	metadataCache := make(map[int32]pvf.ScriptMetadata, 65536)
	visualsWritten := make(map[int32]struct{}, 65536)
	loadStartedAt := time.Now()
	pairsSeen, targetsFound, metadataReads, metadataCacheHits, metadataErrors := 0, 0, 0, 0, 0
	for specIndex, spec := range specs {
		specStartedAt := time.Now()
		specSkippedBefore := skipped
		specCountBefore := count
		if err := ctx.Err(); err != nil {
			_ = visualStmt.Close()
			_ = tagStmt.Close()
			_ = recordStmt.Close()
			return rollback(err)
		}
		c.mu.RLock()
		if c.archive != a || c.indexGen != gen {
			c.mu.RUnlock()
			_ = visualStmt.Close()
			_ = tagStmt.Close()
			_ = recordStmt.Close()
			return rollback(context.Canceled)
		}
		listIndex, ok := a.FindList(spec.listPath)
		if !ok {
			c.mu.RUnlock()
			log.Printf("[pvfine:index] sqlite spec %d/%d list=%s missing", specIndex+1, len(specs), spec.listPath)
			continue
		}
		listPath := a.Path(listIndex)
		pairs, err := a.ListPairs(listIndex)
		c.mu.RUnlock()
		if err != nil {
			skipped++
			log.Printf("[pvfine:index] sqlite spec %d/%d list=%s pairs=0 records=0 skipped=%d elapsed=%s error=%v", specIndex+1, len(specs), spec.listPath, skipped-specSkippedBefore, time.Since(specStartedAt).Round(time.Millisecond), err)
			continue
		}
		pairsSeen += len(pairs)
		if sameSearchPath(spec.listPath, itemShopListPath) {
			c.mu.RLock()
			npcNames = buildNPCNameIndexFromArchive(a)
			c.mu.RUnlock()
		}
		for _, pair := range pairs {
			if err := ctx.Err(); err != nil {
				_ = visualStmt.Close()
				_ = tagStmt.Close()
				_ = recordStmt.Close()
				return rollback(err)
			}
			c.mu.RLock()
			if c.archive != a || c.indexGen != gen {
				c.mu.RUnlock()
				_ = visualStmt.Close()
				_ = tagStmt.Close()
				_ = recordStmt.Close()
				return rollback(context.Canceled)
			}
			targetPath, index, ok := findListTargetInArchive(a, listPath, pair.Path)
			if !ok {
				c.mu.RUnlock()
				skipped++
				continue
			}
			targetsFound++
			file := a.File(index)
			metadata, metadataCached := metadataCache[index]
			var metadataErr error
			if metadataCached {
				metadataCacheHits++
			} else {
				metadataReads++
				metadata, metadataErr = readIndexedMetadataFromArchive(a, index, listPath, npcNames)
				if metadataErr == nil && !isItemShopEntry(listPath, targetPath) {
					metadataCache[index] = metadata
				}
			}
			c.mu.RUnlock()
			if metadataErr != nil {
				metadataErrors++
				metadata = pvf.ScriptMetadata{}
			}
			name := markedName(metadata)
			if _, err := recordStmt.Exec(index, name, strings.ToLower(name), pair.ID, strings.ToLower(pair.ID), targetPath, strings.ToLower(targetPath), spec.category, listPath, file.DataSize, file.DataType); err != nil {
				_ = visualStmt.Close()
				_ = tagStmt.Close()
				_ = recordStmt.Close()
				return rollback(err)
			}
			if _, err := tagStmt.Exec(index, pair.ID, name, spec.category); err != nil {
				_ = visualStmt.Close()
				_ = tagStmt.Close()
				_ = recordStmt.Close()
				return rollback(err)
			}
			iconPath, iconIndex := "", int32(-1)
			fieldPath, fieldIndex := "", int32(-1)
			if metadata.Icon != nil {
				iconPath, iconIndex = metadata.Icon.Path, metadata.Icon.Index
			}
			if metadata.FieldImage != nil {
				fieldPath, fieldIndex = metadata.FieldImage.Path, metadata.FieldImage.Index
			}
			if _, written := visualsWritten[index]; !written {
				if _, err := visualStmt.Exec(index, iconPath, iconIndex, fieldPath, fieldIndex); err != nil {
					_ = visualStmt.Close()
					_ = tagStmt.Close()
					_ = recordStmt.Close()
					return rollback(err)
				}
				visualsWritten[index] = struct{}{}
			}
			count++
		}
		log.Printf("[pvfine:index] sqlite spec %d/%d list=%s pairs=%d records=%d skipped=%d elapsed=%s", specIndex+1, len(specs), spec.listPath, len(pairs), count-specCountBefore, skipped-specSkippedBefore, time.Since(specStartedAt).Round(time.Millisecond))
	}
	log.Printf("[pvfine:index] sqlite phase=load records=%d pairs=%d targets=%d metadata_reads=%d metadata_cache_hits=%d metadata_errors=%d elapsed=%s", count, pairsSeen, targetsFound, metadataReads, metadataCacheHits, metadataErrors, time.Since(loadStartedAt).Round(time.Millisecond))
	_ = visualStmt.Close()
	_ = tagStmt.Close()
	_ = recordStmt.Close()
	fileFallbackIndexStartedAt := time.Now()
	if _, err := tx.Exec(`CREATE INDEX records_file ON records(file_index,id)`); err != nil {
		return rollback(err)
	}
	log.Printf("[pvfine:index] sqlite phase=create-file-fallback-index elapsed=%s", time.Since(fileFallbackIndexStartedAt).Round(time.Millisecond))
	fileRowsStartedAt := time.Now()
	fileRows, err := tx.Exec(`INSERT INTO records(file_index,name,lower_name,record_id,lower_id,path,lower_path,category,list_path,size,data_type)
SELECT f.file_index,f.name,f.lower_name,'','',f.path,f.lower_path,'file','',f.size,f.data_type
FROM files f WHERE NOT EXISTS (SELECT 1 FROM records r WHERE r.file_index=f.file_index)`)
	if err != nil {
		return rollback(err)
	}
	fileRowsAdded, _ := fileRows.RowsAffected()
	log.Printf("[pvfine:index] sqlite phase=file-fallback rows=%d elapsed=%s", fileRowsAdded, time.Since(fileRowsStartedAt).Round(time.Millisecond))
	if _, err := tx.Exec(`UPDATE meta SET value='1' WHERE key='complete'`); err != nil {
		return rollback(err)
	}
	indexStartedAt := time.Now()
	if _, err := tx.Exec(`CREATE INDEX records_lower_path ON records(lower_path);
CREATE INDEX records_lower_name ON records(lower_name);
CREATE INDEX records_lower_id ON records(lower_id);
CREATE INDEX tags_file ON tags(file_index);`); err != nil {
		return rollback(err)
	}
	log.Printf("[pvfine:index] sqlite phase=create-indexes elapsed=%s", time.Since(indexStartedAt).Round(time.Millisecond))
	commitStartedAt := time.Now()
	if err := tx.Commit(); err != nil {
		log.Printf("[pvfine:index] sqlite semantic rebuild commit failed: elapsed=%s total=%s error=%v", time.Since(commitStartedAt).Round(time.Millisecond), time.Since(startedAt).Round(time.Millisecond), err)
		return 0, 0, err
	}
	if candidateDB != nil {
		if err := i.publishSemanticCandidate(c, a, gen, candidateDB, candidatePath); err != nil {
			log.Printf("[pvfine:index] sqlite phase=publish failed: total=%s error=%v", time.Since(startedAt).Round(time.Millisecond), err)
			return 0, 0, err
		}
		candidatePublished = true
	}
	i.ready = true
	log.Printf("[pvfine:index] sqlite semantic rebuild committed: records=%d file_fallback=%d tags=%d visuals=%d skipped=%d commit=%s total=%s", count, fileRowsAdded, count, len(visualsWritten), skipped, time.Since(commitStartedAt).Round(time.Millisecond), time.Since(startedAt).Round(time.Millisecond))
	return count, skipped, nil
}

func (i *sqliteArchiveIndex) children(parent string) ([]*TreeNode, error) {
	i.dbMu.RLock()
	defer i.dbMu.RUnlock()
	rows, err := i.db.Query(`SELECT name,path,1,-1,0,0,child_count FROM dirs WHERE parent=? AND path<>'' UNION ALL SELECT name,path,0,file_index,size,data_type,0 FROM files WHERE parent=? ORDER BY 3 DESC,name,4`, parent, parent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*TreeNode, 0)
	for rows.Next() {
		node := &TreeNode{}
		if err := rows.Scan(&node.Name, &node.Path, &node.IsDir, &node.FileIndex, &node.Size, &node.DataType, &node.ChildCount); err != nil {
			return nil, err
		}
		result = append(result, node)
	}
	return result, rows.Err()
}

func (i *sqliteArchiveIndex) descendants(scope string) ([]*TreeNode, error) {
	i.dbMu.RLock()
	defer i.dbMu.RUnlock()
	query, args := `SELECT name,path,file_index,size,data_type FROM files ORDER BY path,file_index`, []any{}
	if scope != "" {
		query = `SELECT name,path,file_index,size,data_type FROM files WHERE instr(path,?)=1 ORDER BY path,file_index`
		args = []any{scope + "/"}
	}
	rows, err := i.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*TreeNode, 0)
	for rows.Next() {
		node := &TreeNode{}
		if err := rows.Scan(&node.Name, &node.Path, &node.FileIndex, &node.Size, &node.DataType); err != nil {
			return nil, err
		}
		result = append(result, node)
	}
	return result, rows.Err()
}

func (i *sqliteArchiveIndex) resolve(path string) (*TreeNode, error) {
	i.dbMu.RLock()
	defer i.dbMu.RUnlock()
	node := &TreeNode{}
	err := i.db.QueryRow(`SELECT name,path,file_index,size,data_type FROM files WHERE path=? ORDER BY file_index LIMIT 1`, path).Scan(&node.Name, &node.Path, &node.FileIndex, &node.Size, &node.DataType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (i *sqliteArchiveIndex) suggest(prefix string, limit int) ([]string, error) {
	i.dbMu.RLock()
	defer i.dbMu.RUnlock()
	rows, err := i.db.Query(`SELECT path FROM dirs WHERE lower(path) LIKE ? ORDER BY path LIMIT ?`, strings.ToLower(prefix)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0, limit)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		result = append(result, path)
	}
	return result, rows.Err()
}

func (i *sqliteArchiveIndex) tags(fileIndex int32) ([]TreeTag, error) {
	i.dbMu.RLock()
	defer i.dbMu.RUnlock()
	rows, err := i.db.Query(`SELECT tag_id,name,category FROM tags WHERE file_index=? ORDER BY category,tag_id`, fileIndex)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]TreeTag, 0)
	for rows.Next() {
		var tag TreeTag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Category); err != nil {
			return nil, err
		}
		result = append(result, tag)
	}
	return result, rows.Err()
}

func (i *sqliteArchiveIndex) visuals(fileIndex int32) fileVisuals {
	i.dbMu.RLock()
	defer i.dbMu.RUnlock()
	var iconPath, fieldPath string
	var iconIndex, fieldIndex int32
	if err := i.db.QueryRow(`SELECT icon_path,icon_index,field_path,field_index FROM visuals WHERE file_index=?`, fileIndex).Scan(&iconPath, &iconIndex, &fieldPath, &fieldIndex); err != nil {
		return fileVisuals{}
	}
	var result fileVisuals
	if iconPath != "" && iconIndex >= 0 {
		result.icon = &ImageReference{Path: iconPath, Index: iconIndex}
	}
	if fieldPath != "" && fieldIndex >= 0 {
		result.fieldImage = &ImageReference{Path: fieldPath, Index: fieldIndex}
	}
	return result
}

func (i *sqliteArchiveIndex) search(query string, cursor, limit int, exact bool) (*SearchResult, error) {
	return i.searchScoped(query, cursor, limit, exact, false, "")
}

func (i *sqliteArchiveIndex) searchScoped(query string, cursor, limit int, exact, itemsOnly bool, excludeID string) (*SearchResult, error) {
	i.dbMu.RLock()
	defer i.dbMu.RUnlock()
	result := &SearchResult{Hits: []*SearchHit{}, NextCursor: -1}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if cursor < 0 {
		cursor = 0
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return result, nil
	}
	where := "rowid>?"
	args := []any{cursor}
	if excludeID != "" {
		where += " AND record_id<>?"
		args = append(args, excludeID)
	}
	if itemsOnly {
		where += " AND category IN (?,?)"
		args = append(args, SearchCategoryEquipment, SearchCategoryStackable)
	}
	wildcard := strings.ContainsAny(q, "*?")
	if !wildcard {
		if exact {
			where += " AND (lower_path=? OR lower_name=? OR lower_id=?)"
			args = append(args, q, q, q)
		} else {
			where += " AND (instr(lower_path,?)>0 OR instr(lower_name,?)>0 OR instr(lower_id,?)>0)"
			args = append(args, q, q, q)
		}
	}
	batchLimit := limit*32 + 1
	querySQL := `SELECT rowid,name,record_id,path,category,size,data_type,file_index FROM records WHERE ` + where + ` ORDER BY rowid`
	// Wildcards are filtered below, so a SQL row limit can produce an empty
	// page before reaching any matches. Stream until the page is full or EOF.
	if !wildcard {
		querySQL += ` LIMIT ?`
		args = append(args, batchLimit)
	}
	rows, err := i.db.Query(querySQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	matcher := newSearchMatcher(q, exact)
	scanned := cursor
	var last int64
	rowsSeen := 0
	for rows.Next() {
		rowsSeen++
		var rowid int64
		var hit SearchHit
		if err := rows.Scan(&rowid, &hit.Name, &hit.ID, &hit.Path, &hit.Category, &hit.Size, &hit.DataType, &hit.FileIndex); err != nil {
			return nil, err
		}
		last = rowid
		scanned = int(rowid)
		if !(matcher.match(strings.ToLower(hit.Path)) || matcher.match(strings.ToLower(hit.Name)) || matcher.match(strings.ToLower(hit.ID))) {
			continue
		}
		hit.ChangeKind = ""
		result.Hits = append(result.Hits, &hit)
		if len(result.Hits) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if last > 0 && (len(result.Hits) >= limit || (!wildcard && rowsSeen >= batchLimit)) {
		result.NextCursor = int(last)
	}
	result.Scanned = scanned
	return result, nil
}

func (i *sqliteArchiveIndex) indexedNames(fileIndex int32) (string, error) {
	values, err := i.indexedNameValues(fileIndex)
	if err != nil {
		return "", err
	}
	return strings.Join(values, " / "), nil
}

func (i *sqliteArchiveIndex) indexedNameValues(fileIndex int32) ([]string, error) {
	i.dbMu.RLock()
	defer i.dbMu.RUnlock()
	rows, err := i.db.Query(`SELECT name FROM records WHERE file_index=? AND category<>? AND name<>'' ORDER BY id`, fileIndex, SearchCategoryFile)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (i *sqliteArchiveIndex) indexedFileIndexes() ([]int32, error) {
	i.dbMu.RLock()
	defer i.dbMu.RUnlock()
	rows, err := i.db.Query(`SELECT DISTINCT file_index FROM records WHERE category<>? ORDER BY file_index`, SearchCategoryFile)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]int32, 0)
	for rows.Next() {
		var index int32
		if err := rows.Scan(&index); err != nil {
			return nil, err
		}
		result = append(result, index)
	}
	return result, rows.Err()
}

func (i *sqliteArchiveIndex) hasSemanticFile(fileIndex int32) bool {
	i.dbMu.RLock()
	defer i.dbMu.RUnlock()
	var exists int
	err := i.db.QueryRow(`SELECT 1 FROM records WHERE file_index=? AND category<>? LIMIT 1`, fileIndex, SearchCategoryFile).Scan(&exists)
	return err == nil && exists == 1
}

func (i *sqliteArchiveIndex) eachFileByPath(fn func(index int32, path string, size, dataType int32) bool) error {
	i.dbMu.RLock()
	defer i.dbMu.RUnlock()
	rows, err := i.db.Query(`SELECT file_index,path,size,data_type FROM files ORDER BY path,file_index`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var index, size, dataType int32
		var path string
		if err := rows.Scan(&index, &path, &size, &dataType); err != nil {
			return err
		}
		if !fn(index, path, size, dataType) {
			return nil
		}
	}
	return rows.Err()
}

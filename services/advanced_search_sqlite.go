package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pvfine/internal/pvf"
)

const advancedQueryBudget int64 = 256 << 20
const advancedQueryLimit = 8
const advancedCursorStride uint64 = 1 << 32

var advancedQuerySequence atomic.Uint64
var ErrAdvancedCursorStale = errors.New("高级搜索游标已失效，请重新搜索")

type advancedQueryKey struct {
	query, scope string
	regex        bool
}
type advancedDiskQuery struct {
	id    uint64
	key   advancedQueryKey
	path  string
	count int
	bytes int64
	used  uint64
}

// A session owns an immutable reverse index and at most eight disk result sets.
// mu serializes operations, but is NEVER acquired while holding core.mu.
// Invalidations cancel immediately and dispose of the session asynchronously.
// This lets close/edit obtain core.mu between scanner steps instead of waiting
// for a whole archive scan or SQLite sort.
type advancedSQLite struct {
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	dir         string
	cachePath   string
	queryBudget int64 // zero uses the default; injectable for storage-failure tests
	db          *sql.DB
	ready       bool
	queries     map[uint64]*advancedDiskQuery
	clock       uint64
	closed      chan struct{}
	closeOnce   sync.Once
}

func (d *advancedSQLite) close() {
	d.closeOnce.Do(func() {
		d.cancel()
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.db != nil {
			_ = d.db.Close()
			d.db = nil
		}
		if d.dir != "" {
			_ = os.RemoveAll(d.dir)
		}
		close(d.closed)
	})
}

// CancelAdvancedSearch releases the lazy string index and cancels its current
// build/query. Binary search keeps its existing API and cancellation behavior.
func (s *ArchiveService) CancelAdvancedSearch() {
	s.c.mu.Lock()
	s.c.invalidateAdvancedSearchLocked()
	s.c.mu.Unlock()
	emitEvent("archive:advanced-search-stale")
}

func openAdvancedDB(path string) (*sql.DB, error) {
	// All PRAGMAs are connection-local; the single-connection pool ensures they
	// apply to every operation. Results/index have separate bounded connections.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=OFF; PRAGMA synchronous=OFF; PRAGMA cache_size=-8192; PRAGMA temp_store=FILE; PRAGMA mmap_size=0; PRAGMA busy_timeout=5000`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *ArchiveService) searchAdvancedStringSQLite(query, scope string, regex bool, cursor, limit int) (*AdvancedSearchResult, error) {
	var match func(string) bool
	if regex {
		re, err := regexp.Compile(query)
		if err != nil {
			return nil, fmt.Errorf("正则表达式无效: %w", err)
		}
		match = re.MatchString
	} else {
		lower := strings.ToLower(query)
		match = func(value string) bool { return strings.Contains(strings.ToLower(value), lower) }
	}
	c := s.c
	c.mu.Lock()
	if c.archive == nil {
		c.mu.Unlock()
		return nil, ErrNoArchive
	}
	a := c.archive
	d := c.advancedDisk
	if d == nil {
		if cursor != 0 {
			c.mu.Unlock()
			return nil, ErrAdvancedCursorStale
		}
		ctx, cancel := context.WithCancel(context.Background())
		d = &advancedSQLite{ctx: ctx, cancel: cancel, queries: make(map[uint64]*advancedDiskQuery), closed: make(chan struct{})}
		c.advancedDisk = d
		c.advancedCancel = cancel
	}
	c.mu.Unlock()
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ctx.Err(); err != nil {
		return nil, ErrAdvancedCursorStale
	}
	if !d.ready {
		if err := d.build(c, a); err != nil {
			d.status(c, AdvancedSearchIndexStatus{State: AdvancedIndexStateError, Stage: "error", Error: err.Error()})
			// Failed candidates are never usable; the next first-page request retries.
			c.mu.Lock()
			if c.advancedDisk == d {
				c.invalidateAdvancedSearchLocked()
				if !errors.Is(err, context.Canceled) {
					c.advancedStatus = AdvancedSearchIndexStatus{State: AdvancedIndexStateError, Stage: "error", Error: err.Error()}
				}
			}
			c.mu.Unlock()
			return nil, err
		}
	}
	key := advancedQueryKey{query: query, scope: scope, regex: regex}
	var q *advancedDiskQuery
	after := 0
	if cursor != 0 {
		id := uint64(cursor) / advancedCursorStride
		after = int(uint64(cursor) % advancedCursorStride)
		q = d.queries[id]
		if q == nil || q.key != key || after > q.count {
			return nil, ErrAdvancedCursorStale
		}
	} else {
		for _, existing := range d.queries {
			if existing.key == key {
				q = existing
				break
			}
		}
		if q == nil {
			var err error
			q, err = d.query(key, match)
			if err != nil {
				return nil, err
			}
		}
	}
	d.clock++
	q.used = d.clock
	result, err := d.page(q, after, limit)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.archive != a || c.advancedDisk != d || d.ctx.Err() != nil {
		return nil, ErrAdvancedCursorStale
	}
	for _, hit := range result.Hits {
		hit.Name = c.indexedFileNameLocked(hit.FileIndex)
	}
	return result, nil
}

func (d *advancedSQLite) status(c *core, status AdvancedSearchIndexStatus) {
	c.mu.Lock()
	current := c.advancedDisk == d && d.ctx.Err() == nil
	if current {
		c.advancedStatus = status
	}
	c.mu.Unlock()
	if !current {
		return
	}
	event := "archive:advanced-index-progress"
	if status.State == AdvancedIndexStateReady {
		event = "archive:advanced-index-ready"
	}
	if status.State == AdvancedIndexStateError {
		event = "archive:advanced-index-error"
	}
	emitEvent(event, status)
}

func (d *advancedSQLite) build(c *core, a *pvf.Archive) error {
	started := time.Now()
	root, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	root = filepath.Join(root, "pvfine", "advanced-search")
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	d.dir, err = os.MkdirTemp(root, "session-")
	if err != nil {
		return err
	}

	c.mu.RLock()
	if c.advancedDisk != d || c.archive != a || d.ctx.Err() != nil {
		c.mu.RUnlock()
		return context.Canceled
	}
	digest, clean := a.SavedContentHash()
	totalFiles := int(a.FileCount())
	c.mu.RUnlock()
	if clean {
		// Schema/rule version is part of the filename; modified archives only use
		// session files and can never be published as a clean cached index.
		d.cachePath = filepath.Join(root, "refs-v1-"+digest+".db")
		if d.restoreCache() {
			d.ready = true
			d.status(c, AdvancedSearchIndexStatus{State: AdvancedIndexStateReady, Stage: "ready-cache", Done: totalFiles, Total: totalFiles})
			log.Printf("[pvfine:advanced] sqlite reverse index cache hit: files=%d elapsed=%s", totalFiles, time.Since(started).Round(time.Millisecond))
			return nil
		}
	}
	d.db, err = openAdvancedDB(filepath.Join(d.dir, "index.db"))
	if err != nil {
		return err
	}
	_, err = d.db.ExecContext(d.ctx, `CREATE TABLE meta(version INTEGER); CREATE TABLE pool(offset INTEGER PRIMARY KEY,pool TEXT NOT NULL,value TEXT NOT NULL);
 CREATE TABLE files(file_index INTEGER PRIMARY KEY,path TEXT NOT NULL,lower_path TEXT NOT NULL,size INTEGER,data_type INTEGER);
 CREATE TABLE refs(file_index INTEGER,offset INTEGER,occurrences INTEGER,types INTEGER,fields INTEGER,PRIMARY KEY(file_index,offset)) WITHOUT ROWID;`)
	if err != nil {
		return err
	}
	c.mu.RLock()
	if c.advancedDisk != d || c.archive != a || d.ctx.Err() != nil {
		c.mu.RUnlock()
		return context.Canceled
	}
	total := int(a.FileCount())
	pools := a.NewStringPoolScanner()
	scanner := a.NewStringReferenceScanner()
	c.mu.RUnlock()
	d.status(c, AdvancedSearchIndexStatus{State: AdvancedIndexStateBuilding, Stage: "strings", Total: total})
	log.Printf("[pvfine:advanced] sqlite build started: files=%d", total)
	// Fixed-size transactions keep SQLite's pending writes bounded. Archive read
	// locks cover decoding only, never database writes or index creation.
	tx, err := d.db.BeginTx(d.ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	entries, refs, files, pending := 0, 0, 0, 0
	var poolStmt, fileStmt, refStmt *sql.Stmt
	prepare := func() error {
		var e error
		poolStmt, e = tx.PrepareContext(d.ctx, `INSERT INTO pool VALUES(?,?,?)`)
		if e != nil {
			return e
		}
		fileStmt, e = tx.PrepareContext(d.ctx, `INSERT INTO files VALUES(?,?,?,?,?)`)
		if e != nil {
			return e
		}
		refStmt, e = tx.PrepareContext(d.ctx, `INSERT INTO refs VALUES(?,?,?,?,?)`)
		return e
	}
	closeStatements := func() {
		if poolStmt != nil {
			poolStmt.Close()
		}
		if fileStmt != nil {
			fileStmt.Close()
		}
		if refStmt != nil {
			refStmt.Close()
		}
	}
	defer closeStatements()
	if err = prepare(); err != nil {
		return err
	}
	flush := func() error {
		if pending < 4096 {
			return nil
		}
		closeStatements()
		if e := tx.Commit(); e != nil {
			return e
		}
		var e error
		tx, e = d.db.BeginTx(d.ctx, nil)
		if e != nil {
			return e
		}
		pending = 0
		return prepare()
	}
	for {
		c.mu.RLock()
		if c.advancedDisk != d || c.archive != a || d.ctx.Err() != nil {
			c.mu.RUnlock()
			return context.Canceled
		}
		entry, ok, e := pools.Next(d.ctx)
		c.mu.RUnlock()
		if e != nil {
			return e
		}
		if !ok {
			break
		}
		if _, e = poolStmt.ExecContext(d.ctx, entry.Offset, entry.Pool, entry.Value); e != nil {
			return e
		}
		entries++
		pending++
		if e = flush(); e != nil {
			return e
		}
	}
	log.Printf("[pvfine:advanced] sqlite strings=%d elapsed=%s", entries, time.Since(started).Round(time.Millisecond))
	d.status(c, AdvancedSearchIndexStatus{State: AdvancedIndexStateBuilding, Stage: "references", Total: total})
	for {
		c.mu.RLock()
		if c.advancedDisk != d || c.archive != a || d.ctx.Err() != nil {
			c.mu.RUnlock()
			return context.Canceled
		}
		index, references, ok, e := scanner.Next(d.ctx)
		var path string
		var file pvf.File
		if ok && e == nil {
			path = a.Path(index)
			file = a.File(index)
		}
		c.mu.RUnlock()
		if e != nil {
			return e
		}
		if !ok {
			break
		}
		if _, e = fileStmt.ExecContext(d.ctx, index, path, strings.ToLower(path), file.DataSize, file.DataType); e != nil {
			return e
		}
		pending++
		for _, ref := range references {
			types := 0
			for n, t := range ref.TokenTypes {
				types |= int(t) << (n * 4)
			}
			fields := 0
			for _, f := range ref.FileFields {
				if f == "name" {
					fields |= 1
				} else if f == "path" {
					fields |= 2
				}
			}
			if _, e = refStmt.ExecContext(d.ctx, index, ref.Offset, ref.Occurrences, types, fields); e != nil {
				return e
			}
			refs++
			pending++
			if e = flush(); e != nil {
				return e
			}
		}
		files++
		if files%16384 == 0 {
			d.status(c, AdvancedSearchIndexStatus{State: AdvancedIndexStateBuilding, Stage: "references", Done: files, Total: total})
		}
	}
	closeStatements()
	if err = tx.Commit(); err != nil {
		return err
	}
	d.status(c, AdvancedSearchIndexStatus{State: AdvancedIndexStateBuilding, Stage: "indexes", Done: files, Total: total})
	_, err = d.db.ExecContext(d.ctx, `CREATE INDEX refs_offset ON refs(offset,file_index); CREATE INDEX files_path ON files(path,file_index);`)
	if err != nil {
		return err
	}

	if _, err = d.db.ExecContext(d.ctx, `INSERT INTO meta VALUES(1)`); err != nil {
		return err
	}
	if err = d.db.Close(); err != nil {
		return err
	}
	d.db, err = openAdvancedReadDB(filepath.Join(d.dir, "index.db"))
	if err != nil {
		return err
	}
	if d.cachePath != "" && d.ctx.Err() == nil {
		// Hard links publish atomically without duplicating a gigabyte or exposing
		// a partially built cache. If unavailable, keep the working session index.
		if e := os.Link(filepath.Join(d.dir, "index.db"), d.cachePath); e != nil && !os.IsExist(e) {
			log.Printf("[pvfine:advanced] cache publication unavailable; using session index")
		}
		pruneAdvancedCaches(root)
	}
	d.ready = true
	d.status(c, AdvancedSearchIndexStatus{State: AdvancedIndexStateReady, Stage: "ready-sqlite", Done: files, Total: total})
	info, _ := os.Stat(filepath.Join(d.dir, "index.db"))
	var size int64
	if info != nil {
		size = info.Size()
	}
	log.Printf("[pvfine:advanced] sqlite build finished: strings=%d references=%d files=%d bytes=%d elapsed=%s", entries, refs, files, size, time.Since(started).Round(time.Millisecond))
	return nil
}

func attachAdvancedSource(ctx context.Context, db *sql.DB, path string) error {
	_, err := db.ExecContext(ctx, `ATTACH DATABASE ? AS source`, sqliteFileURI(path, "mode=ro"))
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `PRAGMA source.cache_size=-8192; PRAGMA source.mmap_size=0`)
	return err
}

func sqliteFileURI(path, query string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	u := url.URL{Scheme: "file", RawQuery: query}
	if strings.HasPrefix(path, "//") {
		rest := strings.TrimPrefix(path, "//")
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			u.Host = rest[:slash]
			u.Path = rest[slash:]
		} else {
			u.Host = rest
			u.Path = "/"
		}
	} else {
		if len(path) >= 2 && path[1] == ':' {
			path = "/" + path
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		u.Path = path
	}
	return u.String()
}

func (d *advancedSQLite) evictOldest() error {
	var oldest *advancedDiskQuery
	for _, q := range d.queries {
		if oldest == nil || q.used < oldest.used {
			oldest = q
		}
	}
	if oldest == nil {
		return nil
	}
	if err := os.Remove(oldest.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	delete(d.queries, oldest.id)
	return nil
}

func (d *advancedSQLite) query(key advancedQueryKey, match func(string) bool) (*advancedDiskQuery, error) {
	started := time.Now()
	budget := d.queryBudget
	if budget == 0 {
		budget = advancedQueryBudget
	}
	remaining := func() int64 {
		n := budget
		for _, q := range d.queries {
			n -= q.bytes
		}
		return n
	}
	// Reserve at least 64 MiB before starting. max_page_count makes an oversized
	// current query fail explicitly instead of silently exceeding the disk budget.
	for len(d.queries) >= advancedQueryLimit || remaining() < min(64<<20, budget) {
		if err := d.evictOldest(); err != nil {
			return nil, err
		}
	}
	id := advancedQuerySequence.Add(1)
	// Cursors round-trip through JavaScript numbers without precision loss.
	if id >= 1<<21 {
		return nil, errors.New("高级搜索会话编号已耗尽，请重启应用")
	}
	q := &advancedDiskQuery{id: id, key: key, path: filepath.Join(d.dir, fmt.Sprintf("query-%d.db", id))}
	db, err := openAdvancedDB(q.path)
	if err != nil {
		_ = os.Remove(q.path)
		return nil, err
	}
	success := false
	defer func() {
		db.Close()
		if !success {
			_ = os.Remove(q.path)
		}
	}()
	var pageSize int64
	if err = db.QueryRowContext(d.ctx, `PRAGMA page_size`).Scan(&pageSize); err != nil {
		return nil, err
	}
	if _, err = db.ExecContext(d.ctx, fmt.Sprintf("PRAGMA max_page_count=%d", remaining()/pageSize)); err != nil {
		return nil, err
	}
	if err = attachAdvancedSource(d.ctx, db, filepath.Join(d.dir, "index.db")); err != nil {
		return nil, err
	}
	if _, err = db.ExecContext(d.ctx, `CREATE TABLE matches(offset INTEGER PRIMARY KEY); CREATE TABLE hits(seq INTEGER PRIMARY KEY,file_index INTEGER NOT NULL);`); err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(d.ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(d.ctx, `INSERT INTO matches VALUES(?)`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	rows, err := d.db.QueryContext(d.ctx, `SELECT offset,value FROM pool ORDER BY offset`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	scanned := 0
	for rows.Next() {
		var offset int32
		var value string
		if err = rows.Scan(&offset, &value); err != nil {
			return nil, err
		}
		scanned++
		if match(value) {
			if _, err = stmt.ExecContext(d.ctx, offset); err != nil {
				return nil, err
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	stmt.Close()
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	// Materialize only ordered file identities. Details remain normalized in the
	// reverse index and are fetched for the current page, without duplicating text.
	_, err = db.ExecContext(d.ctx, `INSERT INTO hits(file_index)
 SELECT r.file_index FROM matches m CROSS JOIN source.refs r INDEXED BY refs_offset ON r.offset=m.offset
 JOIN source.files f ON f.file_index=r.file_index
 WHERE ?='' OR f.lower_path=? OR substr(f.lower_path,1,length(?)+1)=?||'/'
 GROUP BY r.file_index ORDER BY f.path,r.file_index`, key.scope, key.scope, key.scope, key.scope)
	if err != nil {
		return nil, fmt.Errorf("高级搜索结果写入失败（结果磁盘预算 256 MiB，可缩小搜索范围）: %w", err)
	}
	if err = db.QueryRowContext(d.ctx, `SELECT count(*) FROM hits`).Scan(&q.count); err != nil {
		return nil, err
	}
	info, err := os.Stat(q.path)
	if err != nil {
		return nil, err
	}
	q.bytes = info.Size()
	d.queries[id] = q
	success = true
	log.Printf("[pvfine:advanced] sqlite query finished: strings_scanned=%d files=%d bytes=%d elapsed=%s", scanned, q.count, q.bytes, time.Since(started).Round(time.Millisecond))
	return q, nil
}

func (d *advancedSQLite) page(q *advancedDiskQuery, after, limit int) (*AdvancedSearchResult, error) {
	started := time.Now()
	db, err := openAdvancedDB(q.path)
	if err != nil {
		_ = os.Remove(q.path)
		return nil, err
	}
	defer db.Close()
	if err = attachAdvancedSource(d.ctx, db, filepath.Join(d.dir, "index.db")); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(d.ctx, `SELECT h.seq,f.file_index,f.path,f.size,f.data_type FROM hits h JOIN source.files f ON f.file_index=h.file_index WHERE h.seq>? ORDER BY h.seq LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	result := &AdvancedSearchResult{Hits: []*AdvancedSearchHit{}, NextCursor: -1}
	last := after
	for rows.Next() {
		hit := &AdvancedSearchHit{}
		if err = rows.Scan(&last, &hit.FileIndex, &hit.Path, &hit.Size, &hit.DataType); err != nil {
			rows.Close()
			return nil, err
		}
		result.Hits = append(result.Hits, hit)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, hit := range result.Hits {
		details, e := db.QueryContext(d.ctx, `SELECT p.pool,p.offset,p.value,r.occurrences,r.types,r.fields FROM source.refs r JOIN matches m ON m.offset=r.offset JOIN source.pool p ON p.offset=r.offset WHERE r.file_index=? ORDER BY r.offset`, hit.FileIndex)
		if e != nil {
			return nil, e
		}
		for details.Next() {
			detail := &AdvancedSearchDetail{Kind: "string"}
			var types, fields int
			if e = details.Scan(&detail.Pool, &detail.PoolOffset, &detail.Value, &detail.Occurrences, &types, &fields); e != nil {
				details.Close()
				return nil, e
			}
			for types != 0 {
				detail.TokenTypes = append(detail.TokenTypes, int32(types&15))
				types >>= 4
			}
			if fields&1 != 0 {
				detail.FileFields = append(detail.FileFields, "name")
			}
			if fields&2 != 0 {
				detail.FileFields = append(detail.FileFields, "path")
			}
			hit.Details = append(hit.Details, detail)
		}
		e = details.Err()
		details.Close()
		if e != nil {
			return nil, e
		}
	}
	if last < q.count {
		result.NextCursor = int(q.id*advancedCursorStride + uint64(last))
	}
	result.Scanned = last
	log.Printf("[pvfine:advanced] sqlite page: hits=%d position=%d total=%d elapsed=%s", len(result.Hits), last, q.count, time.Since(started).Round(time.Millisecond))
	return result, nil
}

func openAdvancedReadDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", sqliteFileURI(path, "mode=ro"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err = db.Exec(`PRAGMA cache_size=-8192; PRAGMA temp_store=FILE; PRAGMA mmap_size=0`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (d *advancedSQLite) restoreCache() bool {
	sessionPath := filepath.Join(d.dir, "index.db")
	// An active session holds its own link, so LRU eviction cannot remove a
	// database used by this or another process/window.
	if err := os.Link(d.cachePath, sessionPath); err != nil {
		return false
	}
	db, err := openAdvancedReadDB(sessionPath)
	valid := false
	if err == nil {
		var version int
		var check string
		valid = db.QueryRowContext(d.ctx, `SELECT version FROM meta`).Scan(&version) == nil && version == 1 &&
			db.QueryRowContext(d.ctx, `PRAGMA quick_check`).Scan(&check) == nil && check == "ok"
	}
	if !valid {
		if db != nil {
			db.Close()
		}
		os.Remove(sessionPath)
		if d.ctx.Err() == nil {
			os.Remove(d.cachePath)
		}
		return false
	}
	d.db = db
	now := time.Now()
	_ = os.Chtimes(d.cachePath, now, now)
	return true
}

func pruneAdvancedCaches(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	type cached struct {
		path string
		size int64
		used time.Time
	}
	var files []cached
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "refs-v1-") || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, cached{filepath.Join(root, entry.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].used.Before(files[j].used) })
	for _, file := range files {
		if total <= 2<<30 {
			break
		}
		if os.Remove(file.path) == nil {
			total -= file.size
		}
	}
}

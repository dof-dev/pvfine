package services

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func advancedIndexFiles(shards int) []string {
	names := []string{"index.db"}
	for shard := 0; shard < shards; shard++ {
		names = append(names, advancedShardName(shard))
	}
	return names
}

// Publish all files with one atomic directory rename. Readers can never mix
// shards from concurrent builders or see a half-written index. Hard links avoid
// copying gigabytes and let active sessions survive eviction of their cache.
func (d *advancedSQLite) publishCache(root string) error {
	staging, err := os.MkdirTemp(root, "publish-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	for _, name := range advancedIndexFiles(d.shards) {
		if err := os.Link(filepath.Join(d.dir, name), filepath.Join(staging, name)); err != nil {
			return err
		}
	}
	if err := d.ctx.Err(); err != nil {
		return err
	}
	return os.Rename(staging, d.cachePath)
}

func advancedDBValid(ctx context.Context, db *sql.DB) bool {
	var check string
	return db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&check) == nil && check == "ok"
}

func (d *advancedSQLite) restoreCache() (valid bool) {
	sessionPath := filepath.Join(d.dir, "index.db")
	if err := os.Link(filepath.Join(d.cachePath, "index.db"), sessionPath); err != nil {
		// A cache directory without its manifest cannot be reused or published
		// over; discard it so the completed replacement can be installed.
		if d.ctx.Err() == nil {
			_ = os.RemoveAll(d.cachePath)
		}
		return false
	}
	linked := []string{sessionPath}
	var db *sql.DB
	defer func() {
		if !valid {
			if db != nil {
				db.Close()
			}
			for _, path := range linked {
				os.Remove(path)
			}
			if d.ctx.Err() == nil {
				os.RemoveAll(d.cachePath)
			}
		}
	}()
	var err error
	db, err = openAdvancedReadDB(sessionPath)
	if err != nil {
		return false
	}
	var version, shards int
	if db.QueryRowContext(d.ctx, `SELECT version,shards FROM meta`).Scan(&version, &shards) != nil || version != 3 || shards < 1 || shards > advancedMaxShards || !advancedDBValid(d.ctx, db) {
		return false
	}
	for shard := 0; shard < shards; shard++ {
		name := advancedShardName(shard)
		path := filepath.Join(d.dir, name)
		if os.Link(filepath.Join(d.cachePath, name), path) != nil {
			return false
		}
		linked = append(linked, path)
	}
	checks := make(chan bool, shards)
	for shard := 0; shard < shards; shard++ {
		go func(shard int) {
			refDB, err := openAdvancedReadDB(filepath.Join(d.dir, advancedShardName(shard)))
			if err != nil {
				checks <- false
				return
			}
			var version, id int
			ok := refDB.QueryRowContext(d.ctx, `SELECT version,shard FROM meta`).Scan(&version, &id) == nil && version == 3 && id == shard && advancedDBValid(d.ctx, refDB)
			refDB.Close()
			checks <- ok
		}(shard)
	}
	ok := true
	for shard := 0; shard < shards; shard++ {
		ok = <-checks && ok
	}
	if !ok || d.ctx.Err() != nil {
		return false
	}
	d.db, d.shards = db, shards
	now := time.Now()
	_ = os.Chtimes(d.cachePath, now, now)
	return true
}

func advancedIndexDirectorySize(dir string) int64 {
	entries, _ := os.ReadDir(dir)
	var size int64
	for _, entry := range entries {
		if !entry.IsDir() {
			if info, err := entry.Info(); err == nil {
				size += info.Size()
			}
		}
	}
	return size
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
	var caches []cached
	var total int64
	for _, entry := range entries {
		legacy := (!entry.IsDir() && strings.HasPrefix(entry.Name(), "refs-v1-") && strings.HasSuffix(entry.Name(), ".db")) ||
			(entry.IsDir() && strings.HasPrefix(entry.Name(), "refs-v2-"))
		current := entry.IsDir() && strings.HasPrefix(entry.Name(), "refs-v3-")
		if !legacy && !current {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path, size := filepath.Join(root, entry.Name()), info.Size()
		if entry.IsDir() {
			size = advancedIndexDirectorySize(path)
		}
		caches = append(caches, cached{path, size, info.ModTime()})
		total += size
	}
	sort.Slice(caches, func(i, j int) bool { return caches[i].used.Before(caches[j].used) })
	for i, cache := range caches {
		// Keep the newest index even if it alone exceeds the soft 2 GiB budget.
		// Evicting a just-built large index forces another multi-minute rebuild
		// every time that archive is reopened.
		if total <= 2<<30 || i == len(caches)-1 {
			break
		}
		if os.RemoveAll(cache.path) == nil {
			total -= cache.size
		}
	}
}

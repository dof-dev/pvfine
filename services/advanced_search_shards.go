package services

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"pvfine/internal/pvf"
)

const advancedMaxShards = 4
const advancedRefBatchRows = 16 // Larger batches incur quadratic modernc argument binding.

func advancedShardName(shard int) string { return fmt.Sprintf("refs-%d.db", shard) }

// Each parser owns one SQLite writer. No connection or transaction is shared
// between workers; their B-trees and secondary indexes can be built in parallel.
type advancedReferenceWriter struct {
	db      *sql.DB
	tx      *sql.Tx
	stmt    *sql.Stmt
	args    []any
	pending int
}

func newAdvancedReferenceWriter(ctx context.Context, dir string, shard int) (*advancedReferenceWriter, error) {
	db, err := openAdvancedDB(filepath.Join(dir, advancedShardName(shard)))
	if err != nil {
		return nil, err
	}
	w := &advancedReferenceWriter{db: db, args: make([]any, 0, advancedRefBatchRows*5)}
	_, err = db.ExecContext(ctx, `CREATE TABLE refs(file_index INTEGER,offset INTEGER,occurrences INTEGER,types INTEGER,fields INTEGER,PRIMARY KEY(file_index,offset)) WITHOUT ROWID;
 CREATE TABLE meta(version INTEGER,shard INTEGER)`)
	if err == nil {
		_, err = db.ExecContext(ctx, `INSERT INTO meta VALUES(3,?)`, shard)
	}
	if err == nil {
		err = w.begin(ctx)
	}
	if err != nil {
		w.close()
		return nil, err
	}
	return w, nil
}

func advancedRefInsert(rows int) string {
	return `INSERT INTO refs VALUES` + strings.TrimSuffix(strings.Repeat("(?,?,?,?,?),", rows), ",")
}

func (w *advancedReferenceWriter) begin(ctx context.Context) (err error) {
	w.tx, err = w.db.BeginTx(ctx, nil)
	if err == nil {
		w.stmt, err = w.tx.PrepareContext(ctx, advancedRefInsert(advancedRefBatchRows))
	}
	w.pending = 0
	return err
}

func (w *advancedReferenceWriter) flush(ctx context.Context) error {
	if len(w.args) == 0 {
		return nil
	}
	var err error
	if len(w.args) == advancedRefBatchRows*5 {
		_, err = w.stmt.ExecContext(ctx, w.args...)
	} else {
		_, err = w.tx.ExecContext(ctx, advancedRefInsert(len(w.args)/5), w.args...)
	}
	clear(w.args)
	w.args = w.args[:0]
	return err
}

func (w *advancedReferenceWriter) add(ctx context.Context, index int32, refs []pvf.StringReference) error {
	for _, ref := range refs {
		types, fields := 0, 0
		for n, token := range ref.TokenTypes {
			types |= int(token) << (n * 4)
		}
		for _, field := range ref.FileFields {
			if field == "name" {
				fields |= 1
			} else if field == "path" {
				fields |= 2
			}
		}
		w.args = append(w.args, index, ref.Offset, ref.Occurrences, types, fields)
		w.pending++
		if len(w.args) == cap(w.args) {
			if err := w.flush(ctx); err != nil {
				return err
			}
		}
		if w.pending >= 4096 {
			if err := w.commit(ctx); err != nil {
				return err
			}
			if err := w.begin(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *advancedReferenceWriter) commit(ctx context.Context) error {
	if err := w.flush(ctx); err != nil {
		return err
	}
	w.stmt.Close()
	w.stmt = nil
	err := w.tx.Commit()
	w.tx = nil
	return err
}

func (w *advancedReferenceWriter) close() {
	if w.stmt != nil {
		w.stmt.Close()
	}
	if w.tx != nil {
		w.tx.Rollback()
	}
	w.db.Close()
}

func finishAdvancedShards(ctx context.Context, writers []*advancedReferenceWriter) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var wg sync.WaitGroup
	for _, writer := range writers {
		wg.Add(1)
		go func(w *advancedReferenceWriter) {
			defer wg.Done()
			if err := w.commit(ctx); err != nil {
				cancel(err)
				return
			}
			if _, err := w.db.ExecContext(ctx, `CREATE INDEX refs_offset ON refs(offset,file_index)`); err != nil {
				cancel(err)
			}
		}(writer)
	}
	wg.Wait()
	return context.Cause(ctx)
}

func (d *advancedSQLite) attachShards(ctx context.Context, db *sql.DB) error {
	for shard := 0; shard < d.shards; shard++ {
		alias := fmt.Sprintf("refs%d", shard)
		uri := sqliteFileURI(filepath.Join(d.dir, advancedShardName(shard)), "mode=ro")
		if _, err := db.ExecContext(ctx, `ATTACH DATABASE ? AS `+alias, uri); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `PRAGMA `+alias+`.cache_size=-8192; PRAGMA `+alias+`.mmap_size=0`); err != nil {
			return err
		}
	}
	return nil
}

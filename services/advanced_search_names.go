package services

import (
	"context"
	"database/sql"
	"strings"
)

// The explorer's semantic index resolves 110US <table::key> names through
// localization tables. Those display names are absent from the PVF string pool,
// so include them in each query's file-level matches without changing the
// reusable pool/reference index.
func (d *advancedSQLite) addAdvancedNameMatches(c *core, db *sql.DB, key advancedQueryKey, match func(string) bool) error {
	c.mu.RLock()
	if c.advancedDisk != d {
		c.mu.RUnlock()
		return context.Canceled
	}
	if c.indexStatus.State != IndexStateReady {
		c.mu.RUnlock()
		return d.ctx.Err()
	}
	disk := c.diskIndex
	if disk != nil {
		c.mu.RUnlock()
	}
	tx, err := db.BeginTx(d.ctx, nil)
	if err != nil {
		if disk == nil {
			c.mu.RUnlock()
		}
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(d.ctx, `INSERT OR IGNORE INTO name_matches VALUES(?,?)`)
	if err != nil {
		if disk == nil {
			c.mu.RUnlock()
		}
		return err
	}
	defer stmt.Close()
	if disk == nil {
		for _, record := range c.searchRecords {
			if record.hit.Category == SearchCategoryFile || record.hit.Name == "" ||
				!advancedPathInScope(record.hit.Path, key.scope) || !match(record.hit.Name) {
				continue
			}
			if _, err = stmt.ExecContext(d.ctx, record.hit.FileIndex, record.hit.Name); err != nil {
				break
			}
		}
		c.mu.RUnlock()
	} else {
		disk.dbMu.RLock()
		if disk.db != nil {
			query := `SELECT file_index,name FROM records WHERE category<>? AND name<>''`
			args := []any{SearchCategoryFile}
			if key.scope != "" {
				query += ` AND (lower_path=? OR substr(lower_path,1,length(?)+1)=?||'/')`
				args = append(args, key.scope, key.scope, key.scope)
			}
			if !key.regex {
				query += ` AND instr(lower_name,?)>0`
				args = append(args, strings.ToLower(key.query))
			}
			var rows *sql.Rows
			rows, err = disk.db.QueryContext(d.ctx, query, args...)
			if err == nil {
				for rows.Next() {
					var index int32
					var name string
					if err = rows.Scan(&index, &name); err != nil {
						break
					}
					if match(name) {
						_, err = stmt.ExecContext(d.ctx, index, name)
						if err != nil {
							break
						}
					}
				}
				if err == nil {
					err = rows.Err()
				}
				rows.Close()
			}
		}
		disk.dbMu.RUnlock()
	}
	if err != nil {
		return err
	}
	stmt.Close()
	return tx.Commit()
}

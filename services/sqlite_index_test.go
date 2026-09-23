package services

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
	"pvfine/internal/pvf"
)

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

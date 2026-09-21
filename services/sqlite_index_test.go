package services

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
	"pvfine/internal/pvf"
)

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

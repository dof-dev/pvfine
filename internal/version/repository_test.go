package version

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"pvfine/internal/pvf"
)

func makeVersionFixture(t *testing.T) (string, *pvf.Archive, Snapshot) {
	t.Helper()
	a := pvf.New()
	if _, err := a.AddFileText("skill/a.stk", "[name]\n`初始`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddFileText("text/name.str", "初始名称", pvf.TypeUnicode); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Script.pvf")
	if err := a.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	parsed, err := pvf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := SnapshotFromArchive(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return path, parsed, snapshot
}

func TestRepositoryCommitReopenAndMaterialize(t *testing.T) {
	basePath, archive, base := makeVersionFixture(t)
	baseHash, err := HashFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	root := SidecarPath(basePath)
	repo, err := Init(root, basePath, baseHash, base)
	if err != nil {
		t.Fatal(err)
	}
	rootCommit, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}

	index, ok := archive.Find("skill/a.stk")
	if !ok {
		t.Fatal("fixture file missing")
	}
	if err := archive.SetText(index, "[name]\n`修改后`"); err != nil {
		t.Fatal(err)
	}
	after, err := SnapshotFromArchive(archive)
	if err != nil {
		t.Fatal(err)
	}
	changes := Diff(base, after)
	if len(changes) != 1 || changes[0].Operation != OperationModify {
		t.Fatalf("changes = %#v", changes)
	}
	content, err := ContentSnapshotFromArchive(archive, []string{"skill/a.stk"})
	if err != nil {
		t.Fatal(err)
	}
	objects := map[string][]byte{}
	for _, value := range content {
		objects[value.Hash] = ContentObject(value)
	}
	commit, err := repo.Commit("修改技能名称", changes, objects)
	if err != nil {
		t.Fatal(err)
	}
	if commit.ParentID == "" || commit.ChangeCount != 1 {
		t.Fatalf("commit = %#v", commit)
	}
	paths, err := repo.ChangedPathsBetween(rootCommit.ID, commit.ID)
	if err != nil || len(paths) != 1 || paths[0] != "skill/a.stk" {
		t.Fatalf("changed paths forward = %#v err=%v", paths, err)
	}
	paths, err = repo.ChangedPathsBetween(commit.ID, rootCommit.ID)
	if err != nil || len(paths) != 1 || paths[0] != "skill/a.stk" {
		t.Fatalf("changed paths reverse = %#v err=%v", paths, err)
	}

	materialized, err := repo.Snapshot(commit.ID)
	if err != nil {
		t.Fatal(err)
	}
	if materialized["skill/a.stk"].Hash != after["skill/a.stk"].Hash {
		t.Fatalf("materialized = %#v after = %#v", materialized, after)
	}
	if materialized["text/name.str"].Hash != base["text/name.str"].Hash {
		t.Fatalf("unchanged file was changed: %#v", materialized["text/name.str"])
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	head, err := reopened.Head()
	if err != nil || head.ID != commit.ID {
		t.Fatalf("head = %#v err=%v", head, err)
	}
	history, next, err := reopened.History(0, 10)
	if err != nil || next >= 0 || len(history) != 2 {
		t.Fatalf("history = %#v next=%d err=%v", history, next, err)
	}
	got, err := reopened.Snapshot("")
	if err != nil {
		t.Fatal(err)
	}
	if TreeHash(got) != TreeHash(after) {
		t.Fatalf("reopened tree hash = %s want %s", TreeHash(got), TreeHash(after))
	}
}

func TestObjectStoreDeduplicatesAndRejectsInvalidHash(t *testing.T) {
	store := newObjectStore(filepath.Join(t.TempDir(), "objects"))
	raw := []byte("object")
	first, err := store.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put(raw)
	if err != nil || first != second {
		t.Fatalf("duplicate object = %q %q err=%v", first, second, err)
	}
	got, err := store.Read(first)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("read object = %q err=%v", got, err)
	}
	if _, err := store.Read("not-a-hash"); err == nil {
		t.Fatal("invalid hash unexpectedly accepted")
	}
	if _, err := os.Stat(filepath.Join(store.root, first[:2], first[2:])); err != nil {
		t.Fatal(err)
	}
}

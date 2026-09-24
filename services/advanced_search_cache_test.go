package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAdvancedCacheKeepsNewestOversizedIndex(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "refs-v2-old")
	latest := filepath.Join(root, "refs-v3-latest")
	for i, dir := range []string{old, latest} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, "index.db"))
		if err != nil {
			t.Fatal(err)
		}
		// Sparse files exercise the size limit without writing gigabytes.
		err = f.Truncate(3 << 30)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(time.Duration(i-2) * time.Hour)
		if err := os.Chtimes(dir, when, when); err != nil {
			t.Fatal(err)
		}
	}
	active := filepath.Join(root, "session-active")
	if err := os.Mkdir(active, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(old, "index.db"), filepath.Join(active, "index.db")); err != nil {
		t.Fatal(err)
	}
	pruneAdvancedCaches(root)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("older oversized cache was not evicted")
	}
	for _, dir := range []string{latest, active} {
		if info, err := os.Stat(filepath.Join(dir, "index.db")); err != nil || info.Size() != 3<<30 {
			t.Fatalf("newest cache or active session lost: %v", err)
		}
	}
}

package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pvfine/internal/pvf"
)

func TestTWArchiveDiffRealArchive(t *testing.T) {
	path := os.Getenv("PVF_TW_TESTFILE")
	if path == "" {
		t.Skip("PVF_TW_TESTFILE not set")
	}
	a, err := pvf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	changes, err := archiveDiffCompare(context.Background(), a, a, func(int) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("identical TW archive produced %d differences", len(changes))
	}
}

func TestTWArchiveService(t *testing.T) {
	src := os.Getenv("PVF_TW_TESTFILE")
	if src == "" {
		t.Skip("PVF_TW_TESTFILE not set")
	}
	// Keep version/index sidecars and edits away from the source fixture.
	path := filepath.Join(t.TempDir(), "TW.pvf")
	if err := os.Link(src, path); err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	t.Cleanup(c.closeArchive)
	svc := NewArchiveService(c)
	info, err := svc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != pvf.FormatTW || info.FileCount == 0 {
		t.Fatalf("wrong TW information: format=%s", info.Format)
	}
	deadline := time.Now().Add(time.Minute)
	for {
		c.mu.RLock()
		ready := c.diskIndex != nil
		status := c.indexStatus
		c.mu.RUnlock()
		if ready {
			break
		}
		if status.State == IndexStateError {
			t.Fatal(status.Error)
		}
		if time.Now().After(deadline) {
			t.Fatal("TW file index timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
	root, err := svc.ListChildren("")
	if err != nil || len(root) == 0 {
		t.Fatalf("TW tree: %v", err)
	}
	c.mu.RLock()
	var index int32 = -1
	for i := int32(0); i < c.archive.FileCount(); i++ {
		if strings.HasSuffix(c.archive.Path(i), ".equ") {
			index = i
			break
		}
	}
	c.mu.RUnlock()
	if index < 0 {
		t.Fatal("no TW equipment")
	}
	file, err := NewEditorService(c).GetFile(index)
	if err != nil {
		t.Fatal(err)
	}
	c.mu.RLock()
	nutIndex, found := c.archive.Find("sqr/common.nut")
	c.mu.RUnlock()
	if !found {
		t.Fatal("TW NUT missing")
	}
	nut, err := NewEditorService(c).GetFile(nutIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !nut.Editable || nut.DataType != pvf.TypeUnicode || nut.Text == "" {
		t.Fatal("TW NUT is not editable in the editor service")
	}
	if !strings.Contains(file.Text, "[name]") {
		t.Fatal("TW equipment not decompiled")
	}
}

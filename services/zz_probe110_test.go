package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"pvfine/internal/pvf"
)

func TestProbe110FirstFileLoad(t *testing.T) {
	started := time.Now()
	filename := os.Getenv("PVF_TESTFILE")
	if filename == "" {
		t.Skip("PVF_TESTFILE not set; skipping real-archive probe")
	}
	archivePath, err := filepath.Abs(filename)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	a, err := pvf.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	t.Logf("open: %s", time.Since(started))

	index, ok := a.Find("equipment/character/common/amulet/100300877.equ")
	if !ok {
		t.Fatal("target file not found")
	}

	c := NewCore()
	defer func() {
		if c.diskIndex != nil {
			c.diskIndex.close()
		}
	}()
	setupStarted := time.Now()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Logf("setArchive temporary index rebuild: %s", time.Since(setupStarted))
	editor := NewEditorService(c)

	firstStarted := time.Now()
	first, err := editor.GetFile(index)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first GetFile: %s (text %d bytes, annotations %d)", time.Since(firstStarted), len(first.Text), len(first.Annotations))

	secondStarted := time.Now()
	second, err := editor.GetFile(index)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("second GetFile: %s (text %d bytes, annotations %d)", time.Since(secondStarted), len(second.Text), len(second.Annotations))
}

func TestProbe110AsyncOpenBasicFile(t *testing.T) {
	filename := os.Getenv("PVF_TESTFILE")
	if filename == "" {
		t.Skip("PVF_TESTFILE not set; skipping real-archive probe")
	}
	archivePath, err := filepath.Abs(filename)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	defer c.closeArchive()

	started := time.Now()
	if _, err := c.openArchive(archivePath); err != nil {
		t.Fatal(err)
	}
	t.Logf("open returned: %s", time.Since(started))

	c.mu.RLock()
	a := c.archive
	index, ok := a.Find("equipment/character/common/amulet/100300877.equ")
	stage := c.indexStatus.Stage
	c.mu.RUnlock()
	if !ok {
		t.Fatal("target file not found")
	}

	fileStarted := time.Now()
	meta, err := NewEditorService(c).GetFileBasic(index)
	if err != nil {
		t.Fatal(err)
	}
	if !meta.Editable || meta.Text == "" {
		t.Fatalf("basic file = editable %t, text size %d", meta.Editable, len(meta.Text))
	}
	t.Logf("basic file returned: %s (stage=%s text=%d)", time.Since(fileStarted), stage, len(meta.Text))
}

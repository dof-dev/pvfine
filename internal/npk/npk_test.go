package npk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealImagePacks2Metadata(t *testing.T) {
	root := os.Getenv("NPK_TESTDIR")
	if root == "" {
		t.Skip("NPK_TESTDIR 未设置")
	}
	root = resolveTestPath(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	manifest, err := collectTestNPKs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest) != 120 {
		t.Fatalf("NPK count = %d, want 120", len(manifest))
	}
	imgFiles, imageCount := 0, 0
	for _, path := range manifest {
		npkFile, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range npkFile.Entries {
			if !strings.EqualFold(filepath.Ext(entry.Name), ".img") {
				continue
			}
			parsed, err := ParseIMGAt(reader, entry.Offset, entry.Size)
			if err != nil {
				t.Fatalf("parse %s:%s: %v", filepath.Base(path), entry.Name, err)
			}
			imgFiles++
			imageCount += len(parsed.Images)
		}
		_ = reader.Close()
	}
	if imgFiles != 1692 || imageCount != 145816 {
		t.Fatalf("IMG/image count = %d/%d, want 1692/145816", imgFiles, imageCount)
	}
}

func TestRealImagePacks2DecodeSamples(t *testing.T) {
	root := os.Getenv("NPK_TESTDIR")
	if root == "" {
		t.Skip("NPK_TESTDIR 未设置")
	}
	root = resolveTestPath(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	targets := map[string]int32{
		"sprite/item/new_equipment/08_necklace/necklace.img": 69,
		"sprite/item/fieldimage.img":                         6,
		"sprite/item/title/2017_battlesuit.img":              0,
	}
	found := make(map[string]bool)
	paths, err := collectTestNPKs(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		npkFile, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range npkFile.Entries {
			key := strings.ToLower(filepath.ToSlash(entry.Name))
			index, ok := targets[key]
			if !ok || found[key] {
				continue
			}
			img, err := ParseIMGAt(reader, entry.Offset, entry.Size)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeImage(reader, entry.Offset, entry.Size, img, index)
			if err != nil {
				t.Fatalf("decode %s: %v", entry.Name, err)
			}
			if decoded.Bounds().Dx() <= 0 || decoded.Bounds().Dy() <= 0 {
				t.Fatalf("decode %s returned empty image", entry.Name)
			}
			found[key] = true
		}
		_ = reader.Close()
	}
	for target := range targets {
		if !found[target] {
			t.Fatalf("sample %s not found", target)
		}
	}
}

func resolveTestPath(value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	candidate := value
	for index := 0; index < 4; index++ {
		if _, err := os.Stat(candidate); err == nil {
			absolute, _ := filepath.Abs(candidate)
			return absolute
		}
		candidate = filepath.Join("..", candidate)
	}
	return value
}

func collectTestNPKs(root string) ([]string, error) {
	paths := make([]string, 0)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".npk") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for index := range paths {
		for other := index + 1; other < len(paths); other++ {
			if paths[other] < paths[index] {
				paths[index], paths[other] = paths[other], paths[index]
			}
		}
	}
	return paths, nil
}

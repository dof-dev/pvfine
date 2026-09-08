package services

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileSetServiceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "file-sets.json")
	service := newFileSetService(path)
	document := FileSetDocument{
		Version:     fileSetDocumentVersion,
		ActiveSetID: "set-1",
		FileSets: []StoredFileSet{
			{
				ID:   "set-1",
				Name: "测试文件集",
				Entries: []StoredFileSetEntry{
					{Path: "dir\\item.equ", Name: "item", IDs: []string{"1008", "1008"}, Size: 12, DataType: 1},
				},
			},
		},
	}

	if err := service.SaveFileSets(document); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.LoadFileSets()
	if err != nil {
		t.Fatal(err)
	}
	want := FileSetDocument{
		Version:     fileSetDocumentVersion,
		ActiveSetID: "set-1",
		FileSets: []StoredFileSet{
			{
				ID:   "set-1",
				Name: "测试文件集",
				Entries: []StoredFileSetEntry{
					{Path: "dir/item.equ", Name: "item", IDs: []string{"1008"}, Size: 12, DataType: 1},
				},
			},
		},
	}
	if !reflect.DeepEqual(loaded, want) {
		t.Fatalf("loaded = %#v, want %#v", loaded, want)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, err = %v", info.Mode().Perm(), err)
	}
}

func TestFileSetServiceMissingFile(t *testing.T) {
	service := newFileSetService(filepath.Join(t.TempDir(), "file-sets.json"))
	document, err := service.LoadFileSets()
	if err != nil {
		t.Fatal(err)
	}
	if document.Version != fileSetDocumentVersion || len(document.FileSets) != 0 {
		t.Fatalf("default document = %#v", document)
	}
}

func TestFileSetServiceCanPersistEmptyDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file-sets.json")
	service := newFileSetService(path)
	if err := service.SaveFileSets(FileSetDocument{
		Version: fileSetDocumentVersion,
		FileSets: []StoredFileSet{
			{ID: "set-1", Name: "待删除"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.SaveFileSets(FileSetDocument{
		Version:  fileSetDocumentVersion,
		FileSets: []StoredFileSet{},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.LoadFileSets()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != fileSetDocumentVersion || len(loaded.FileSets) != 0 {
		t.Fatalf("empty document = %#v", loaded)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty file set source still exists, err = %v", err)
	}
}

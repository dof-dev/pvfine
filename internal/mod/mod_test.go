package mod

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func samplePackage() Package {
	return Package{Metadata: Metadata{Name: "中文测试"}, Entries: []Entry{
		{Path: "stackable/demo.stk", Operation: ReplaceFile, Encoding: Text, DataType: 1, Data: []byte("[name]\n`中文`")},
		{Path: "list/stackable.lst", Operation: MergeList, Pairs: []Pair{{"123", "stackable/demo.stk"}}},
		{Path: "string/demo.str", Operation: MergeStrings, Pairs: []Pair{{"name", "中文>说明"}, {"empty", ""}}},
	}}
}

func TestExtend110Write(t *testing.T) {
	root := t.TempDir()
	if err := (Extend110Writer{}).Write(root, samplePackage()); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]string{
		"pvf/stackable/demo.stk":   "[name]\n`中文`",
		"merge/list/stackable.lst": "123\t`stackable/demo.stk`\n",
		"merge/string/demo.str":    "name>中文>说明\nempty>\n",
	} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil || string(data) != want || strings.HasPrefix(string(data), "\ufeff") {
			t.Fatalf("%s = %q, error %v", p, data, err)
		}
	}
	var manifest struct {
		Name, Version string
		Priority      int
		Enabled       bool
		Types         map[string]int32
	}
	data, err := os.ReadFile(filepath.Join(root, "pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "中文测试" || manifest.Version != "1.0" || manifest.Priority != 100 || !manifest.Enabled || manifest.Types["stackable/demo.stk"] != 1 {
		t.Fatalf("manifest = %#v", manifest)
	}
	if _, err := os.Stat(filepath.Join(root, "sprite")); !os.IsNotExist(err) {
		t.Fatal("unused resource directory created")
	}
}

func TestExtend110CustomVersion(t *testing.T) {
	pkg := samplePackage()
	pkg.Metadata.Version = "2.1-beta"
	root := t.TempDir()
	if err := (Extend110Writer{}).Write(root, pkg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "pack.json"))
	if err != nil || !strings.Contains(string(data), `"version": "2.1-beta"`) {
		t.Fatalf("custom manifest = %s, %v", data, err)
	}
	for _, invalid := range []string{" 1.0", "1.0 ", "1\n2", strings.Repeat("x", 129)} {
		pkg.Metadata.Version = invalid
		if err := (Extend110Writer{}).Validate(pkg); err == nil {
			t.Fatalf("invalid version accepted: %q", invalid)
		}
	}
}

func TestExtend110RejectsUnsupportedContentAndCollisions(t *testing.T) {
	cases := []Entry{
		{Path: "../outside", Operation: ReplaceFile, Encoding: Text, DataType: 1},
		{Path: "file.equ", Operation: DeleteFile},
		{Path: "data.bin", Operation: ReplaceFile, Encoding: Bytes, DataType: 9},
		{Path: "list/test.lst", Operation: MergeList, Pairs: []Pair{{"text-id", "item/a"}}},
		{Path: "list/test.lst", Operation: MergeList, Pairs: []Pair{{"1", "../outside"}}},
		{Path: "string/test.str", Operation: MergeStrings, Pairs: []Pair{{"bad>key", "value"}}},
		{Path: "string/test.str", Operation: MergeStrings, Pairs: []Pair{{"key", "bad\nline"}}},
	}
	for _, entry := range cases {
		pkg := Package{Metadata: Metadata{Name: "test"}, Entries: []Entry{entry}}
		if err := (Extend110Writer{}).Validate(pkg); err == nil {
			t.Fatalf("accepted %#v", entry)
		}
	}
	pkg := samplePackage()
	pkg.Entries = append(pkg.Entries, Entry{Path: "STACKABLE/DEMO.STK", Operation: ReplaceFile, Encoding: Text, DataType: 1})
	if err := (Extend110Writer{}).Validate(pkg); err == nil {
		t.Fatal("accepted case-insensitive duplicate")
	}
	pkg = samplePackage()
	pkg.Entries = append(pkg.Entries, Entry{Path: "stackable", Operation: ReplaceFile, Encoding: Text, DataType: 1})
	if err := (Extend110Writer{}).Validate(pkg); err == nil {
		t.Fatal("accepted file/directory collision")
	}
}

func TestNameAndPathValidation(t *testing.T) {
	for _, name := range []string{"", ".hidden", "_hidden", "a/b", "a\\b", "CON", "LPT1.txt", "COM¹", strings.Repeat("a", 256), "name.", "name ", " name", "a:b", "a\x00b"} {
		if err := ValidateName(name); err == nil {
			t.Errorf("accepted name %q", name)
		}
	}
	for _, p := range []string{"/root", "dir//file", "../file", "dir/../file", "dir/NUL.txt", "dir/file.", "C:/file", "dir\\file"} {
		if err := ValidatePath(p); err == nil {
			t.Errorf("accepted path %q", p)
		}
	}
	if err := ValidateName("合法名称 MOD"); err != nil {
		t.Fatal(err)
	}
}

type testReader struct{}

func (testReader) Detect(source fs.FS) (bool, error) {
	_, err := fs.Stat(source, "test.marker")
	return err == nil, nil
}
func (testReader) Read(fs.FS) (Package, error) { return samplePackage(), nil }

func TestRegistrySeparatesReadWriteCapabilities(t *testing.T) {
	registry := DefaultRegistry()
	if err := registry.Register(Format{Descriptor: Descriptor{ID: "read-only"}, Reader: testReader{}}); err != nil {
		t.Fatal(err)
	}
	reader, err := registry.Get("read-only")
	if err != nil || !reader.Descriptor.CanRead || reader.Descriptor.CanWrite || reader.Writer != nil {
		t.Fatalf("reader = %#v, error %v", reader, err)
	}
	detected, err := reader.Reader.Detect(fstest.MapFS{"test.marker": &fstest.MapFile{}})
	if err != nil || !detected {
		t.Fatal("reader detection failed")
	}
	pkg, err := reader.Reader.Read(fstest.MapFS{})
	if err != nil || len(pkg.Entries) != 3 {
		t.Fatal("reader did not produce neutral package")
	}
	writer, _ := registry.Get("110USextend")
	if writer.Descriptor.CanRead || !writer.Descriptor.CanWrite || writer.Reader != nil {
		t.Fatal("writer capabilities incorrect")
	}
	writer.Descriptor.Operations[0] = DeleteFile
	again, _ := registry.Get("110USextend")
	if again.Descriptor.Operations[0] == DeleteFile {
		t.Fatal("descriptor leaked mutable registry state")
	}
	if err := registry.Register(writer); err == nil {
		t.Fatal("duplicate registry entry accepted")
	}
}

func TestExtend110OutputLayout(t *testing.T) {
	pkg := samplePackage()
	root := t.TempDir()
	writer := Extend110Writer{}
	if err := writer.Write(root, pkg); err != nil {
		t.Fatal(err)
	}
	files := writer.Files(pkg)
	if len(files) != len(pkg.Entries)+1 || files[0].Path != "pack.json" || files[0].EntryPath != "" {
		t.Fatalf("layout = %#v", files)
	}
	for _, file := range files {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(file.Path))); err != nil {
			t.Fatalf("declared output does not exist: %s: %v", file.Path, err)
		}
	}
}

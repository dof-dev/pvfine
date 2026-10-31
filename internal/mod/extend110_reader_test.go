package mod

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestExtend110RoundTrip(t *testing.T) {
	pkg := samplePackage()
	pkg.Metadata.Version = "2.3-beta"
	root := t.TempDir()
	if err := (Extend110Writer{}).Write(root, pkg); err != nil {
		t.Fatal(err)
	}
	got, err := (Extend110Reader{}).Read(os.DirFS(root))
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata != pkg.Metadata {
		t.Fatalf("metadata = %#v", got.Metadata)
	}
	byPath := map[string]Entry{}
	for _, e := range got.Entries {
		byPath[e.Path] = e
	}
	for _, want := range pkg.Entries {
		actual := byPath[want.Path]
		// Merges carry semantic pairs, not a payload encoding.
		if want.Operation != ReplaceFile {
			actual.Encoding = want.Encoding
		}
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("entry %s = %#v; want %#v", want.Path, actual, want)
		}
	}
}

func TestExtend110ReaderRejectsInvalidPackage(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"traversal":         {"pack.json": {Data: []byte(`{"name":"test","types":{"../x":1}}`)}},
		"missing type":      {"pack.json": {Data: []byte(`{"name":"test"}`)}, "pvf/x.stk": {Data: []byte("[name]\n`x`")}},
		"invalid type":      {"pack.json": {Data: []byte(`{"name":"test","types":{"x.stk":9}}`)}, "pvf/x.stk": {Data: []byte("x")}},
		"missing payload":   {"pack.json": {Data: []byte(`{"name":"test","types":{"x.stk":1}}`)}},
		"duplicate targets": {"pack.json": {Data: []byte(`{"name":"test","types":{"x.str":3}}`)}, "pvf/x.str": {Data: []byte("a>b")}, "merge/X.str": {Data: []byte("a>c")}},
		"duplicate keys":    {"pack.json": {Data: []byte(`{"name":"test"}`)}, "merge/x.str": {Data: []byte("a>b\na>c")}},
		"invalid list":      {"pack.json": {Data: []byte(`{"name":"test"}`)}, "merge/x.lst": {Data: []byte("1 `unterminated")}},
		"invalid utf8":      {"pack.json": {Data: []byte(`{"name":"test"}`)}, "merge/x.str": {Data: []byte{0xff}}},
		"bom":               {"pack.json": {Data: []byte(`{"name":"test"}`)}, "merge/x.str": {Data: []byte("\ufeffa>b")}},
		"sensitive":         {"pack.json": {Data: []byte(`{"name":"test","types":{".env":3}}`)}, "pvf/.env": {Data: []byte("synthetic fixture")}},
		"empty":             {"pack.json": {Data: []byte(`{"name":"test"}`)}},
		"symlink manifest":  {"pack.json": {Mode: fs.ModeSymlink, Data: []byte("manifest")}},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := (Extend110Reader{}).Read(files); err == nil {
				t.Fatal("invalid package accepted")
			}
		})
	}
}

func TestExtend110RejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pack.json"), []byte(`{"name":"test","types":{"x.str":3}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "pvf"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "fixture.str")
	if err := os.WriteFile(outside, []byte("a>b"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "pvf", "x.str")); err != nil {
		t.Skip(err)
	}
	if _, err := (Extend110Reader{}).Read(os.DirFS(root)); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestParseMergeStringsPreservesValues(t *testing.T) {
	pairs, err := ParseMergeText("  // comment\r\na>  text > tail  \r\nb>\n", MergeStrings)
	if err != nil || !reflect.DeepEqual(pairs, []Pair{{"a", "  text > tail  "}, {"b", ""}}) {
		t.Fatalf("pairs = %#v, %v", pairs, err)
	}
}

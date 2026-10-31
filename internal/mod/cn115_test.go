package mod

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func cn115Fixture(t *testing.T) (cn115Manifest, fstest.MapFS) {
	t.Helper()
	m := cn115Manifest{
		Schema: 1, Client: "115CN", Kind: "resource", ID: "unique-test-mod", Name: "测试 MOD", Version: "1.0.0",
		Components: []Component{
			{ID: "base", Title: "基础", DefaultSelected: true},
			{ID: "optional", Title: "可选", Requires: []string{"base"}},
		},
		PVF: []cn115Operation{
			{Component: "base", Source: "payload/item.stk", Path: "stackable/demo.stk", Mode: "replace", Type: 1},
			{Component: "base", Source: "payload/list.lst", Path: "list/stackable.lst", Mode: "merge", Type: 1},
			{Component: "optional", Source: "payload/text.str", Path: "string/demo.str", Mode: "merge", Type: 3},
			{Component: "optional", Source: "payload/new.stk", Path: "stackable/new.stk", Mode: "add"},
		},
	}
	files := fstest.MapFS{
		"payload/item.stk": {Data: []byte("[price]\n200")},
		"payload/new.stk":  {Data: []byte("[price]\n300")},
		"payload/list.lst": {Data: []byte("2\t`stackable/new.stk`\r\n")},
		"payload/text.str": {Data: []byte("name> 新名称 > \r\nempty>\r\n")},
	}
	setCN115Manifest(t, files, m)
	return m, files
}

func setCN115Manifest(t *testing.T, files fstest.MapFS, m cn115Manifest) {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	files["mod.json"] = &fstest.MapFile{Data: data}
}

func TestCN115ReadAndSelection(t *testing.T) {
	_, files := cn115Fixture(t)
	for _, prefix := range []string{"", "中文包/"} {
		t.Run(prefix, func(t *testing.T) {
			root := fstest.MapFS{}
			for p, f := range files {
				root[prefix+p] = f
			}
			pkg, err := (CN115Reader{}).Read(root)
			if err != nil {
				t.Fatal(err)
			}
			defaults, ids, err := SelectComponents(pkg, nil)
			if err != nil || len(ids) != 1 || ids[0] != "base" || len(defaults.Entries) != 2 {
				t.Fatalf("defaults: %+v %v %v", defaults, ids, err)
			}
			all, _, err := SelectComponents(pkg, []string{"base", "optional"})
			if err != nil || len(all.Entries) != 4 || all.Entries[3].Operation != AddFile || all.Entries[2].Pairs[0].Value != " 新名称 > " {
				t.Fatalf("all: %+v %v", all, err)
			}
			empty, _, err := SelectComponents(pkg, []string{})
			if err != nil || len(empty.Entries) != 0 {
				t.Fatal("empty selection became defaults", err)
			}
			for _, ids := range [][]string{{"optional"}, {"unknown"}, {"base", "base"}} {
				if _, _, err := SelectComponents(pkg, ids); err == nil {
					t.Fatalf("accepted invalid selection %v", ids)
				}
			}
		})
	}
}

func TestCN115RejectMalformedPackages(t *testing.T) {
	for _, kind := range []string{"schema", "client", "kind", "id", "component", "cycle", "missing-dependency", "target", "uppercase", "source", "sensitive", "missing-source", "patch", "type", "merge-type", "merge-extension", "duplicate-key", "encoding", "parent-symlink", "file-symlink", "nested-root"} {
		t.Run(kind, func(t *testing.T) {
			m, files := cn115Fixture(t)
			switch kind {
			case "schema":
				m.Schema = 2
			case "client":
				m.Client = "110US"
			case "kind":
				m.Kind = "script"
			case "id":
				m.ID = "Not-Unique"
			case "component":
				m.PVF[0].Component = "unknown"
			case "cycle":
				m.Components[0].Requires = []string{"optional"}
			case "missing-dependency":
				m.Components[0].Requires = []string{"unknown"}
			case "target":
				m.PVF[0].Path = "../escape.stk"
			case "uppercase":
				m.PVF[0].Path = "Stackable/demo.stk"
			case "source":
				m.PVF[0].Source = "../escape"
			case "sensitive":
				m.PVF[0].Source = ".env"
			case "missing-source":
				delete(files, "payload/item.stk")
			case "patch":
				m.PVF[0].Mode = "patch"
			case "type":
				m.PVF[0].Type = 9
			case "merge-type":
				m.PVF[1].Type = 3
			case "merge-extension":
				m.PVF[1].Path = "etc/merge.etc"
			case "duplicate-key":
				files["payload/list.lst"].Data = []byte("2 `a`\n2 `b`")
			case "encoding":
				files["payload/item.stk"].Data = []byte{0xff}
			case "parent-symlink":
				files["payload"] = &fstest.MapFile{Mode: fs.ModeSymlink}
			case "file-symlink":
				files["payload/item.stk"].Mode = fs.ModeSymlink
			case "nested-root":
				nested := fstest.MapFS{}
				setCN115Manifest(t, files, m)
				for p, f := range files {
					nested["one/two/"+p] = f
				}
				files = nested
			}
			if kind != "nested-root" {
				setCN115Manifest(t, files, m)
			}
			if _, err := (CN115Reader{}).Read(files); err == nil {
				t.Fatal("accepted malformed package")
			}
		})
	}
}

func TestCN115SelectionConflicts(t *testing.T) {
	m, files := cn115Fixture(t)
	m.Components[0].ExclusiveGroup = "variant"
	m.Components[1].ExclusiveGroup = "variant"
	setCN115Manifest(t, files, m)
	pkg, err := (CN115Reader{}).Read(files)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := SelectComponents(pkg, []string{"base", "optional"}); err == nil {
		t.Fatal("exclusive group ignored")
	}
	pkg.Components[0].ExclusiveGroup = ""
	pkg.Components[1].ExclusiveGroup = ""
	pkg.Entries[3].Path = pkg.Entries[0].Path
	if _, _, err := SelectComponents(pkg, []string{"base", "optional"}); err == nil {
		t.Fatal("duplicate target ignored")
	}
	if _, _, err := SelectComponents(pkg, []string{"base"}); err != nil {
		t.Fatal("unselected alternative blocked selection", err)
	}
	pkg.Entries[3].Path = "stackable/demo.stk/child"
	if _, _, err := SelectComponents(pkg, []string{"base", "optional"}); err == nil {
		t.Fatal("file/directory conflict ignored")
	}
}

func TestCN115WriteRoundTrip(t *testing.T) {
	pkg := samplePackage()
	pkg.Metadata.ID = "my-unique-mod"
	pkg.Metadata.Version = "2.0.0"
	writer := CN115Writer{}
	if err := writer.Validate(pkg); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := writer.Write(dir, pkg); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := writer.WriteArchive(&data, pkg); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(data.Bytes()), int64(data.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []fs.FS{os.DirFS(dir), z} {
		got, err := (CN115Reader{}).Read(root)
		if err != nil || got.Metadata != pkg.Metadata || len(got.Entries) != len(pkg.Entries) {
			t.Fatalf("roundtrip: %+v %v", got, err)
		}
		for i, entry := range got.Entries {
			if entry.Path != pkg.Entries[i].Path || entry.Operation != pkg.Entries[i].Operation {
				t.Fatalf("entry changed: %+v", entry)
			}
		}
	}
	pkg.Metadata.ID = ""
	if err := writer.Validate(pkg); err == nil {
		t.Fatal("missing unique ID accepted")
	}
}

func TestCN115ZIPSource(t *testing.T) {
	_, files := cn115Fixture(t)
	prefix, err := simplifiedchinese.GB18030.NewEncoder().String("中文包")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"root", "wrapper", "gbk", "traversal", "case-collision", "symlink", "file-directory"} {
		t.Run(kind, func(t *testing.T) {
			var buffer bytes.Buffer
			z := zip.NewWriter(&buffer)
			for p, f := range files {
				name := p
				switch kind {
				case "wrapper":
					name = "中文包/" + p
				case "gbk":
					name = prefix + "/" + p
				}
				header := &zip.FileHeader{Name: name, NonUTF8: kind == "gbk"}
				w, err := z.CreateHeader(header)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write(f.Data); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "traversal":
				_, err = z.Create("../escape")
			case "case-collision":
				_, err = z.Create("MOD.JSON")
			case "symlink":
				header := &zip.FileHeader{Name: "link"}
				header.SetMode(fs.ModeSymlink | 0777)
				_, err = z.CreateHeader(header)
			case "file-directory":
				_, err = z.Create("payload")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := z.Close(); err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(t.TempDir(), "package.dnfmod")
			if err := os.WriteFile(filename, buffer.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}
			root, closer, err := OpenSource(filename, true)
			valid := kind == "root" || kind == "wrapper" || kind == "gbk"
			if valid {
				if err != nil {
					t.Fatal(err)
				}
				defer closer.Close()
				if _, err := (CN115Reader{}).Read(root); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				closer.Close()
				t.Fatal("unsafe ZIP accepted")
			}
			if _, closer, err := OpenSource(filename, false); err == nil {
				closer.Close()
				t.Fatal("ZIP accepted by directory-only format")
			}
			jar := filename + ".jar"
			if err := os.WriteFile(jar, buffer.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}
			if _, closer, err := OpenSource(jar, true); err == nil {
				closer.Close()
				t.Fatal("JAR accepted as a MOD ZIP")
			}
		})
	}
}

func TestCN115ZIPSizeLimit(t *testing.T) {
	var buffer bytes.Buffer
	z := zip.NewWriter(&buffer)
	if _, err := z.CreateRaw(&zip.FileHeader{Name: "huge.stk", UncompressedSize64: maxPayloadSize + 1}); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "oversize.zip")
	if err := os.WriteFile(filename, buffer.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	if _, closer, err := OpenSource(filename, true); err == nil {
		closer.Close()
		t.Fatal("ZIP declared expansion limit ignored")
	}
}

func TestCN115ResourceWarningsAndLimits(t *testing.T) {
	m, files := cn115Fixture(t)
	size := int64(20)
	m.Files = []cn115Resource{{Component: "base", Source: "payload/resource.npk", Target: "ImagePacks2/unique.npk", Size: &size, SHA256: strings.Repeat("a", 64)}}
	m.Requires = []string{"other-mod"}
	setCN115Manifest(t, files, m)
	pkg, err := (CN115Reader{}).Read(files)
	if err != nil || len(pkg.Warnings) != 2 {
		t.Fatal("missing resource/dependency warnings", pkg.Warnings, err)
	}
	for _, target := range []string{"../a.npk", "bin/file.dll", "SoundPacks/sub/file.npk", "Stream/Video/movie.exe"} {
		m.Files[0].Target = target
		setCN115Manifest(t, files, m)
		if _, err := (CN115Reader{}).Read(files); err == nil {
			t.Fatalf("accepted resource target %s", target)
		}
	}
	if _, err := readPackageFile(files, "payload/item.stk", 2); err == nil {
		t.Fatal("payload limit ignored")
	}
}

func TestCN115RealMOD(t *testing.T) {
	filename := os.Getenv("PVF_MOD_TESTFILE")
	if filename == "" {
		t.Skip("PVF_MOD_TESTFILE required")
	}
	root, closer, err := OpenSource(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	pkg, err := (CN115Reader{}).Read(root)
	if err != nil {
		t.Fatal(err)
	}
	pkg, ids, err := SelectComponents(pkg, nil)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[Operation]int{}
	for _, entry := range pkg.Entries {
		counts[entry.Operation]++
	}
	t.Logf("MOD: %s %s; selected=%v entries=%d operations=%v", pkg.Metadata.Name, pkg.Metadata.Version, ids, len(pkg.Entries), counts)
	if len(pkg.Entries) == 0 {
		t.Fatal("no PVF operations")
	}
}

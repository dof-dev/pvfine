package services

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	modpkg "pvfine/internal/mod"
)

func TestMergeExportPackages(t *testing.T) {
	for _, operation := range []modpkg.Operation{modpkg.MergeList, modpkg.MergeStrings} {
		t.Run(string(operation), func(t *testing.T) {
			base := modpkg.Package{Entries: []modpkg.Entry{
				{Path: "keep.stk", Operation: modpkg.ReplaceFile, Data: []byte("keep")},
				{Path: "same.stk", Operation: modpkg.ReplaceFile, Data: []byte("old")},
				{Path: "table", Operation: operation, Pairs: []modpkg.Pair{{Key: "keep", Value: "old"}, {Key: "same", Value: "old"}}},
			}}
			current := modpkg.Package{Metadata: modpkg.Metadata{Name: "new"}, Entries: []modpkg.Entry{
				{Path: "same.stk", Operation: modpkg.ReplaceFile, Data: []byte("new")},
				{Path: "table", Operation: operation, Pairs: []modpkg.Pair{{Key: "same", Value: "new"}, {Key: "add", Value: "new"}}},
				{Path: "new.stk", Operation: modpkg.ReplaceFile, Data: []byte("new")},
			}}
			got, err := mergeExportPackages(base, current)
			if err != nil {
				t.Fatal(err)
			}
			wantPairs := []modpkg.Pair{{Key: "keep", Value: "old"}, {Key: "same", Value: "new"}, {Key: "add", Value: "new"}}
			if len(got.Entries) != 4 || got.Metadata != current.Metadata || string(got.Entries[1].Data) != "new" || !reflect.DeepEqual(got.Entries[2].Pairs, wantPairs) {
				t.Fatalf("merged package = %#v", got)
			}
			if base.Entries[2].Pairs[1].Value != "old" || len(current.Entries[1].Pairs) != 2 {
				t.Fatal("merge mutated source packages")
			}
		})
	}
}

func TestExportMergedModRoundTrip(t *testing.T) {
	for _, formatID := range []string{"110USextend", modpkg.CN115Format} {
		t.Run(formatID, func(t *testing.T) {
			_, exports := exportFixture(t)
			f, _ := exports.registry.Get(formatID)
			baseDir := t.TempDir()
			base := modpkg.Package{Metadata: modpkg.Metadata{ID: "base", Name: "已有MOD", Version: "1.2.3"}, Entries: []modpkg.Entry{
				{Path: "stackable/demo.stk", Operation: modpkg.ReplaceFile, Encoding: modpkg.Text, DataType: 1, Data: []byte("[price]\n1")},
				{Path: "stackable/keep.stk", Operation: modpkg.ReplaceFile, Encoding: modpkg.Text, DataType: 1, Data: []byte("[price]\n2")},
				{Path: "string/demo.str", Operation: modpkg.MergeStrings, Pairs: []modpkg.Pair{{Key: "name", Value: "旧名称"}, {Key: "base-only", Value: "保留"}}},
			}}
			if err := f.Writer.Write(baseDir, base); err != nil {
				t.Fatal(err)
			}
			metadata, err := exports.mergeSourceMetadata(baseDir, formatID)
			if err != nil || metadata.Name != "已有MOD" || metadata.Version != "1.2.4" || metadata.VersionNotice != "" || metadata.Path != baseDir {
				t.Fatalf("source metadata = %#v, %v", metadata, err)
			}
			r := modRequest("selection")
			r.Format, r.ModID, r.MergePath = formatID, "merged", baseDir
			r.Version = metadata.Version
			r.Scopes, r.IncludeDependencies = []string{"stackable/demo.stk"}, true
			preview, err := exports.Prepare(r)
			if err != nil {
				t.Fatal(err)
			}
			if preview.FileCount != 5 || preview.DependencyCount != 3 {
				t.Fatalf("merged preview = %#v", preview)
			}
			selected, err := exports.selectedPlan(exports.plan, []string{"pvf/stackable/keep.stk"})
			if err != nil || len(selected.pkg.Entries) != 4 || len(exports.plan.pkg.Entries) != 5 {
				t.Fatalf("merged file exclusion = %#v, %v", selected, err)
			}
			otherFormat := modpkg.CN115Format
			if formatID == otherFormat {
				otherFormat = "110USextend"
			}
			if _, err := exports.readMergePackage(baseDir, otherFormat); err == nil {
				t.Fatal("mismatched merge format accepted")
			}
			// Preparing the same source again must reread disk rather than reuse stale base data.
			base.Entries[1].Data = []byte("[price]\n3")
			if err := f.Writer.Write(baseDir, base); err != nil {
				t.Fatal(err)
			}
			preview, err = exports.Prepare(r)
			if err != nil {
				t.Fatal(err)
			}
			// Execution uses the frozen package after its source directory disappears.
			if err := os.RemoveAll(baseDir); err != nil {
				t.Fatal(err)
			}
			out, err := exports.executeTo(preview.ID, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			wantName := r.Name + "-1.2.4"
			if formatID == modpkg.CN115Format {
				wantName += ".zip"
			}
			if filepath.Base(out) != wantName {
				t.Fatalf("output name = %s", filepath.Base(out))
			}
			pkg, err := exports.readMergePackage(out, formatID)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range pkg.Entries {
				switch entry.Path {
				case "stackable/demo.stk":
					if !strings.Contains(string(entry.Data), "100") {
						t.Fatal("existing file overwrote current export")
					}
				case "stackable/keep.stk":
					if string(entry.Data) != "[price]\n3" {
						t.Fatal("base-only file lost or cached")
					}
				case "string/demo.str":
					pairs := pairMap(entry.Pairs)
					if pairs["name"] != "测试物品" || pairs["base-only"] != "保留" {
						t.Fatalf("merged pairs = %#v", pairs)
					}
				}
			}
			// Removing merge source returns to a normal export.
			r.MergePath = ""
			plain, err := exports.Prepare(r)
			if err != nil || plain.FileCount != 4 {
				t.Fatalf("plain preview = %#v, %v", plain, err)
			}
			r.MergePath = filepath.Join(t.TempDir(), "missing")
			if _, err := exports.Prepare(r); err == nil {
				t.Fatal("missing merge source accepted")
			}
		})
	}
}

func TestMergeExportFullTable(t *testing.T) {
	for _, test := range []struct {
		operation modpkg.Operation
		text      string
	}{
		{modpkg.MergeStrings, "keep>old\nsame>old\n"},
		{modpkg.MergeList, "1 `old`\n2 `old`\n"},
	} {
		pairs, err := modpkg.ParseMergeText(test.text, test.operation)
		if err != nil {
			t.Fatal(err)
		}
		currentPair := pairs[1]
		currentPair.Value = "new"
		base := modpkg.Package{Entries: []modpkg.Entry{{Path: "table", Operation: modpkg.ReplaceFile, Encoding: modpkg.Text, DataType: 3, Data: []byte(test.text)}}}
		current := modpkg.Package{Entries: []modpkg.Entry{{Path: "table", Operation: test.operation, Pairs: []modpkg.Pair{currentPair}}}}
		got, err := mergeExportPackages(base, current)
		if err != nil {
			t.Fatal(err)
		}
		entry := got.Entries[0]
		values, err := modpkg.ParseMergeText(string(entry.Data), test.operation)
		if err != nil || entry.Operation != modpkg.ReplaceFile || len(values) != 2 || values[0] != pairs[0] || values[1] != currentPair {
			t.Fatalf("full table = %#v, %v", entry, err)
		}
		base.Entries[0].Data = []byte("invalid")
		if _, err := mergeExportPackages(base, current); err == nil {
			t.Fatal("unparseable full table accepted")
		}
	}
}

func TestNextModVersion(t *testing.T) {
	for _, tt := range []struct {
		input, want string
		notice      bool
	}{
		{"1.0", "1.1", false}, {"1.2.3", "1.2.4", false}, {"9", "10", false},
		{"1.009", "1.010", false}, {"1.99999999999999999999", "1.100000000000000000000", false},
		{"v1.0", "v1.0", true}, {"1.0-beta", "1.0-beta", true}, {"", "", true},
		{"1..0", "1..0", true}, {strings.Repeat("9", 128), strings.Repeat("9", 128), true},
	} {
		got, notice := nextModVersion(tt.input)
		if got != tt.want || (notice != "") != tt.notice {
			t.Fatalf("nextModVersion(%q) = %q, %q", tt.input, got, notice)
		}
	}
}

func TestModOutputName(t *testing.T) {
	for _, zipOutput := range []bool{false, true} {
		suffix, defaultVersion := "", "1.0"
		if zipOutput {
			suffix, defaultVersion = ".zip", "1.0.0"
		}
		for _, version := range []string{"", "2.3.4", "beta-1"} {
			name, err := modOutputName(modpkg.Metadata{Name: "测试MOD", Version: version}, zipOutput)
			effective := version
			if effective == "" {
				effective = defaultVersion
			}
			if err != nil || name != "测试MOD-"+effective+suffix {
				t.Fatalf("output name = %q, %v", name, err)
			}
		}
		for _, version := range []string{"1/2", "1\\2", "1:2", strings.Repeat("x", 255)} {
			if _, err := modOutputName(modpkg.Metadata{Name: "测试MOD", Version: version}, zipOutput); err == nil {
				t.Fatalf("invalid filename version accepted: %q", version)
			}
		}
	}
}

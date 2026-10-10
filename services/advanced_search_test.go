package services

import (
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func TestAdvancedSearchModesAndInvalidation(t *testing.T) {
	a := pvf.New()
	first, err := a.AddFileText("dir/first.equ", "[name]\n`alpha`\n{5=`shared`}", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.AddFileText("dir/second.equ", "[name]\n`beta`", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	third, err := a.AddFileText("other/third.equ", "[name]\n`alpha`", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}

	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	svc := NewArchiveService(c)

	textResult, err := svc.AdvancedSearch(AdvancedSearchModeString, "alpha", "dir", false, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(textResult.Hits) != 1 || textResult.Hits[0].FileIndex != first {
		t.Fatalf("text result = %#v", textResult.Hits)
	}
	if len(textResult.Hits[0].Details) == 0 || textResult.Hits[0].Details[0].Value != "alpha" {
		t.Fatalf("text details = %#v", textResult.Hits[0].Details)
	}
	if len(textResult.Hits[0].Details[0].TokenTypes) == 0 || textResult.Hits[0].Details[0].TokenTypes[0] != 6 {
		t.Fatalf("text token details = %#v", textResult.Hits[0].Details[0])
	}
	if status := svc.AdvancedIndexStatus(); status.State != AdvancedIndexStateReady {
		t.Fatalf("advanced status = %#v", status)
	}

	regexResult, err := svc.AdvancedSearch(AdvancedSearchModeString, "^a", "", true, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(regexResult.Hits) != 1 || regexResult.NextCursor < 0 {
		t.Fatalf("regex page = %#v", regexResult)
	}
	regexNext, err := svc.AdvancedSearch(AdvancedSearchModeString, "^a", "", true, regexResult.NextCursor, 1)
	if err != nil || len(regexNext.Hits) != 1 || regexNext.Hits[0].FileIndex != third {
		t.Fatalf("regex next = %#v, err=%v", regexNext, err)
	}

	binaryResult, err := svc.AdvancedSearch(AdvancedSearchModeBinary, "[name]\n`alpha`", "", false, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(binaryResult.Hits) != 2 {
		t.Fatalf("binary result = %#v", binaryResult.Hits)
	}
	if len(binaryResult.Hits[0].Details) != 1 || binaryResult.Hits[0].Details[0].Hex == "" ||
		len(binaryResult.Hits[0].Details[0].ByteOffsets) == 0 {
		t.Fatalf("binary details = %#v", binaryResult.Hits[0].Details)
	}

	if err := NewEditorService(c).SetText(first, "[name]\n`gamma`"); err != nil {
		t.Fatal(err)
	}
	afterOld, err := svc.AdvancedSearch(AdvancedSearchModeString, "alpha", "", false, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterOld.Hits) != 1 || afterOld.Hits[0].FileIndex != third {
		t.Fatalf("old string result after edit = %#v", afterOld.Hits)
	}
	afterNew, err := svc.AdvancedSearch(AdvancedSearchModeString, "gamma", "", false, 0, 20)
	if err != nil || len(afterNew.Hits) != 1 || afterNew.Hits[0].FileIndex != first {
		t.Fatalf("new string result after edit = %#v, err=%v", afterNew.Hits, err)
	}
}

func TestAdvancedSearchRejectsInvalidRegex(t *testing.T) {
	a := pvf.New()
	if _, err := a.AddFileText("dir/item.equ", "[name]\n`alpha`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()

	_, err := NewArchiveService(c).AdvancedSearch(AdvancedSearchModeString, "[", "", true, 0, 20)
	if err == nil {
		t.Fatal("invalid regex should fail")
	}
	if status := NewArchiveService(c).AdvancedIndexStatus(); status.State != AdvancedIndexStateIdle {
		t.Fatalf("invalid regex started index build: %#v", status)
	}
}

func TestSuggestDirectoriesUsesPrefixIndex(t *testing.T) {
	a := pvf.New()
	for _, path := range []string{
		"equipment/character/amulet/a.equ",
		"equipment/character/armor/b.equ",
		"equipment/common/c.equ",
	} {
		if _, err := a.AddFileText(path, "[name]\n`item`", pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	defer c.closeArchive()

	paths, err := NewArchiveService(c).SuggestDirectories("EQUIPMENT/CHAR", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "equipment/character" {
		t.Fatalf("directory suggestions = %#v", paths)
	}
}

func TestAdvancedSearchRealArchive(t *testing.T) {
	c := testArchive(t)
	svc := NewArchiveService(c)
	const scope = "equipment/character/common/amulet"

	stringResult, err := svc.AdvancedSearch(AdvancedSearchModeString, "烈火之心项链", scope, false, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(stringResult.Hits) == 0 {
		t.Fatalf("real string search returned no hits")
	}
	for _, hit := range stringResult.Hits {
		if hit == nil || !strings.HasPrefix(hit.Path, scope+"/") {
			t.Fatalf("real string search escaped scope: %#v", hit)
		}
	}

	binaryResult, err := svc.AdvancedSearch(AdvancedSearchModeBinary, "[name]\n`烈火之心项链`", scope, false, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(binaryResult.Hits) == 0 {
		t.Fatalf("real binary search returned no hits")
	}
}

func TestAdvancedQuickSearchMatchesExplorerWithScopeAndPagination(t *testing.T) {
	for _, disk := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "sqlite"}[disk], func(t *testing.T) {
			c := NewCore()
			svc := NewArchiveService(c)
			path := writeSearchFixture(t, "quick-search.pvf")
			if disk {
				if _, err := svc.Open(path); err != nil {
					t.Fatal(err)
				}
			} else {
				a := mustReopen(t, path)
				if err := c.setArchive(a); err != nil {
					t.Fatal(err)
				}
				c.startSearchIndex()
			}
			t.Cleanup(c.closeArchive)
			waitForSearchIndex(t, c)
			for _, exact := range []bool{false, true} {
				mode := AdvancedSearchModeQuick
				if exact {
					mode = AdvancedSearchModeQuickExact
				}
				for _, query := range []string{"1008", "烈火之心项链", "readme", "MISC/README.TXT", "*.equ", "misc/readme.tx?", "*"} {
					for _, scope := range []string{"", "equipment", "./EQUIPMENT/", "stackable", "equip", "missing"} {
						baseline, err := svc.search(query, 0, 1000, exact)
						if err != nil {
							t.Fatal(err)
						}
						expected := []*SearchHit{}
						for _, hit := range baseline.Hits {
							if advancedPathInScope(hit.Path, normalizeAdvancedScope(scope)) {
								expected = append(expected, hit)
							}
						}
						got := []*AdvancedSearchHit{}
						cursor := 0
						for page := 0; ; page++ {
							if page > len(expected)+1 {
								t.Fatal("pagination did not terminate")
							}
							result, err := svc.AdvancedSearch(mode, query, scope, false, cursor, 1)
							if err != nil {
								t.Fatal(err)
							}
							got = append(got, result.Hits...)
							if result.NextCursor < 0 {
								break
							}
							if result.NextCursor <= cursor {
								t.Fatal("cursor did not advance")
							}
							cursor = result.NextCursor
						}
						if len(got) != len(expected) {
							t.Fatalf("%s query=%q scope=%q: got %d, want %d", mode, query, scope, len(got), len(expected))
						}
						for n, hit := range got {
							want := expected[n]
							if hit.Path != want.Path || hit.Name != want.Name || hit.FileIndex != want.FileIndex {
								t.Fatalf("unexpected hit: %#v, want %#v", hit, want)
							}
							if want.ID != "" && (len(hit.Details) != 1 || hit.Details[0].Kind != "id" || hit.Details[0].Value != want.ID) {
								t.Fatalf("missing ID: %#v", hit)
							}
						}
					}
				}
			}
			if status := svc.AdvancedIndexStatus(); status.State != AdvancedIndexStateIdle {
				t.Fatalf("quick search built string index: %#v", status)
			}
		})
	}
}

package version

import (
	"testing"

	"pvfine/internal/pvf"
	"pvfine/internal/rendering"
)

func TestSnapshotHashIgnoresPresentationRenderer(t *testing.T) {
	a := pvf.New()
	index, err := a.AddFileText("skills/test.skl", "[records]\n1 2 3 4 5\n[/records]", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ContentSnapshotFromArchive(a, []string{a.Path(index)})
	if err != nil {
		t.Fatal(err)
	}

	engine, err := rendering.Parse([]byte(`{
  "version": 1,
  "rules": [{
    "id": "skl.records",
    "match": {"extensions": [".skl"]},
    "target": {"kind": "section", "section": "records"},
    "format": {"tokensPerLine": 2}
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	a.SetScriptRenderer(engine)
	after, err := ContentSnapshotFromArchive(a, []string{a.Path(index)})
	if err != nil {
		t.Fatal(err)
	}
	key := CanonicalPath(a.Path(index))
	if before[key].Hash != after[key].Hash {
		t.Fatalf("snapshot hash changed after presentation override: before=%s after=%s", before[key].Hash, after[key].Hash)
	}
}

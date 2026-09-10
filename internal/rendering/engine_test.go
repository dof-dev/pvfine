package rendering

import "testing"

func TestFormatMatchingUsesExtensionSectionAndGlob(t *testing.T) {
	engine, err := Compile(Document{
		Version: 1,
		Rules: []Rule{
			{
				ID:     "file.lst",
				Match:  MatchSpec{Extensions: []string{".LST"}},
				Target: TargetSpec{Kind: "file"},
				Format: FormatSpec{TokensPerLine: 2},
			},
			{
				ID:     "file.skl",
				Match:  MatchSpec{Extensions: []string{".skl"}},
				Target: TargetSpec{Kind: "file"},
				Format: FormatSpec{TokensPerLine: 2},
			},
			{
				ID:     "section.global",
				Target: TargetSpec{Kind: "section", Section: "Records"},
				Format: FormatSpec{TokensPerLine: 3},
			},
			{
				ID:     "section.skl",
				Match:  MatchSpec{Extensions: []string{".skl"}},
				Target: TargetSpec{Kind: "section", Section: "records"},
				Format: FormatSpec{TokensPerLine: 4},
			},
			{
				ID:     "section.skl.glob",
				Match:  MatchSpec{Extensions: []string{".SKL"}, Glob: "skills/**/*.skl"},
				Target: TargetSpec{Kind: "section", Section: "RECORDS"},
				Format: FormatSpec{TokensPerLine: 5},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := engine.FileFormat("foo/INDEX.LST").TokensPerLine; got != 2 {
		t.Fatalf("lst file format = %d, want 2", got)
	}
	if got := engine.FileFormat("skills/test.SKL").TokensPerLine; got != 2 {
		t.Fatalf("skl file format = %d, want 2", got)
	}
	if got := engine.SectionFormat("other/item.skl", "records").TokensPerLine; got != 4 {
		t.Fatalf("skl section format = %d, want 4", got)
	}
	if got := engine.SectionFormat("skills/advanced/item.SKL", "RECORDS").TokensPerLine; got != 5 {
		t.Fatalf("glob section format = %d, want 5", got)
	}
	if got := engine.SectionFormat("item.equ", "records").TokensPerLine; got != 3 {
		t.Fatalf("global section format = %d, want 3", got)
	}
}

func TestFormatMatchingUsesLaterRuleForEqualSpecificity(t *testing.T) {
	engine, err := Compile(Document{
		Version: 1,
		Rules: []Rule{
			{
				ID:     "first",
				Match:  MatchSpec{Extensions: []string{".equ"}},
				Target: TargetSpec{Kind: "file"},
				Format: FormatSpec{TokensPerLine: 2},
			},
			{
				ID:     "second",
				Match:  MatchSpec{Extensions: []string{".EQU"}},
				Target: TargetSpec{Kind: "file"},
				Format: FormatSpec{TokensPerLine: 7},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.FileFormat("item.equ").TokensPerLine; got != 7 {
		t.Fatalf("equal-specificity format = %d, want 7", got)
	}
}

package services

import "testing"

func TestWildcardMatch(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		pattern string
		want    bool
	}{
		{name: "empty", value: "", pattern: "", want: true},
		{name: "star matches nested path", value: "equipment/character/1008.equ", pattern: "*.equ", want: true},
		{name: "question matches one rune", value: "misc/readme.txt", pattern: "misc/readme.tx?", want: true},
		{name: "question does not match too many runes", value: "misc/readme.txt", pattern: "misc/readme.t???", want: false},
		{name: "unicode question matches one rune", value: "项", pattern: "?", want: true},
		{name: "literal mismatch", value: "misc/readme.txt", pattern: "misc/readme.doc", want: false},
		{name: "star matches empty", value: "readme", pattern: "read*me", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := wildcardMatch(test.value, test.pattern); got != test.want {
				t.Fatalf("wildcardMatch(%q, %q) = %v, want %v", test.value, test.pattern, got, test.want)
			}
		})
	}
}

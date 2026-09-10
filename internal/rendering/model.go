package rendering

// Document is the JSON document that describes TypeScript display rules.
type Document struct {
	Version     int    `json:"version"`
	Description string `json:"description,omitempty"`
	Rules       []Rule `json:"rules"`
}

// Rule describes one file- or section-scoped rendering format.
type Rule struct {
	ID          string     `json:"id"`
	Description string     `json:"description,omitempty"`
	Match       MatchSpec  `json:"match"`
	Target      TargetSpec `json:"target"`
	Format      FormatSpec `json:"format"`
}

// MatchSpec limits a rule to selected file extensions and/or archive paths.
// Matching is case-insensitive.
type MatchSpec struct {
	Extensions []string `json:"extensions,omitempty"`
	Glob       string   `json:"glob,omitempty"`
}

// TargetSpec selects whether a rule applies to a complete file or to all
// sections with one name.
type TargetSpec struct {
	Kind    string `json:"kind"`
	Section string `json:"section,omitempty"`
}

// FormatSpec contains the display formatting knobs supported by V1.
type FormatSpec struct {
	// Offset places the first Offset value tokens on separate lines before
	// TokensPerLine grouping begins.
	Offset int `json:"offset,omitempty"`
	// TokensPerLine wraps value tokens after this many tokens. It is also the
	// fallback when TokensPerLineIndex is configured but its value is missing
	// or invalid.
	TokensPerLine int `json:"tokensPerLine"`
	// TokensPerLineIndex is a zero-based direct token index inside a section;
	// its positive integer value overrides TokensPerLine for that section.
	TokensPerLineIndex *int `json:"tokensPerLineIndex,omitempty"`
}

package annotations

import "strings"

type Document struct {
	Version     int                     `json:"version"`
	Description string                  `json:"description,omitempty"`
	Relations   map[string]RelationSpec `json:"relations,omitempty"`
	Fields      []FieldDefinition       `json:"fields,omitempty"`
	Rules       []Rule                  `json:"rules"`
}

type ListDocument struct {
	Version     int                     `json:"version"`
	Description string                  `json:"description,omitempty"`
	Relations   map[string]RelationSpec `json:"relations"`
}

type RelationSpec struct {
	Kind         string            `json:"kind,omitempty"`
	ListPath     string            `json:"listPath"`
	IDToken      int               `json:"idToken"`
	PathToken    int               `json:"pathToken"`
	RecordTokens int               `json:"recordTokens,omitempty"`
	NameSection  string            `json:"nameSection"`
	ContextToken int               `json:"contextToken,omitempty"`
	ContextPaths map[string]string `json:"contextPaths,omitempty"`
	Relations    []string          `json:"relations,omitempty"`
}

type Rule struct {
	ID          string         `json:"id"`
	Description string         `json:"description,omitempty"`
	Field       string         `json:"field,omitempty"`
	Match       MatchSpec      `json:"match"`
	Target      TargetSpec     `json:"target"`
	Annotation  AnnotationSpec `json:"annotation"`
	Group       string         `json:"group,omitempty"`
}

// FieldDefinition is a reusable field description shared by annotation rules
// and structured previews. A field may optionally expose a PreviewSpec; fields
// without it are still useful to annotations and remain out of previews.
type FieldDefinition struct {
	ID         string         `json:"id"`
	Match      MatchSpec      `json:"match"`
	Target     TargetSpec     `json:"target"`
	Annotation AnnotationSpec `json:"annotation"`
	Preview    *PreviewSpec   `json:"preview,omitempty"`
}

// PreviewSpec describes how a shared field participates in a preview. The
// parser deliberately treats Group, Role and Format as opaque strings so new
// preview providers can add roles without changing the annotation schema.
type PreviewSpec struct {
	// Provider is the backwards-compatible single-provider form.
	Provider string `json:"provider,omitempty"`
	// Providers allows one shared field to feed multiple preview components.
	Providers []string `json:"providers,omitempty"`
	Role      string   `json:"role"`
	Group     string   `json:"group"`
	Order     int      `json:"order"`
	Format    string   `json:"format"`
	Label     string   `json:"label,omitempty"`
}

func (preview *PreviewSpec) providerNames() []string {
	if preview == nil {
		return nil
	}
	result := make([]string, 0, len(preview.Providers)+1)
	if provider := strings.TrimSpace(preview.Provider); provider != "" {
		result = append(result, provider)
	}
	for _, provider := range preview.Providers {
		provider = strings.TrimSpace(provider)
		if provider == "" {
			continue
		}
		duplicate := false
		for _, existing := range result {
			if strings.EqualFold(existing, provider) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, provider)
		}
	}
	return result
}

type MatchSpec struct {
	Extensions []string `json:"extensions,omitempty"`
	Glob       string   `json:"glob,omitempty"`
}

type TargetSpec struct {
	Kind    string      `json:"kind"`
	Section string      `json:"section,omitempty"`
	Index   *int        `json:"index,omitempty"`
	Range   *TokenRange `json:"range,omitempty"`
	// Offset skips the first Offset direct tokens in a section before
	// repeated records are matched. It mirrors rendering rules where a
	// section header is kept outside the repeated token groups.
	Offset       int `json:"offset,omitempty"`
	RecordTokens int `json:"recordTokens,omitempty"`
	// TokensPerLineIndex is a zero-based direct token index whose positive
	// integer value overrides RecordTokens for that section. RecordTokens is
	// retained as the fallback when the token is missing or invalid.
	TokensPerLineIndex *int `json:"tokensPerLineIndex,omitempty"`
	ContextIndex       *int `json:"contextIndex,omitempty"`
	ImagePathToken     *int `json:"imagePathToken,omitempty"`
	// ImageIndexToken is kept only to migrate rules written by the previous
	// schema, where target.index pointed to the IMG path and this field pointed
	// to the numeric image index.
	ImageIndexToken *int `json:"imageIndexToken,omitempty"`
}

type TokenRange struct {
	Start        int `json:"start"`
	EndExclusive int `json:"endExclusive"`
}

type AnnotationSpec struct {
	Title       string            `json:"title"`
	Content     string            `json:"content,omitempty"`
	Type        string            `json:"type"`
	Values      map[string]string `json:"values,omitempty"`
	Relation    string            `json:"relation,omitempty"`
	InlineImage bool              `json:"inlineImage,omitempty"`
}

type Reference struct {
	ID        string
	Name      string
	Path      string
	FileIndex int32
}

type Resolver func(relation, id string) (Reference, bool)

type Result struct {
	Start           int             `json:"start"`
	End             int             `json:"end"`
	Title           string          `json:"title"`
	Content         string          `json:"content"`
	Type            string          `json:"type"`
	TargetFileIndex int32           `json:"targetFileIndex"`
	RuleIDs         []string        `json:"ruleIds,omitempty"`
	Image           *ImageReference `json:"image,omitempty"`
	InlineImage     bool            `json:"inlineImage,omitempty"`
}

type ImageReference struct {
	Path  string `json:"path"`
	Index int32  `json:"index"`
}

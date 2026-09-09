package annotations

type Document struct {
	Version     int                     `json:"version"`
	Description string                  `json:"description,omitempty"`
	Relations   map[string]RelationSpec `json:"relations,omitempty"`
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
	Match       MatchSpec      `json:"match"`
	Target      TargetSpec     `json:"target"`
	Annotation  AnnotationSpec `json:"annotation"`
	Group       string         `json:"group,omitempty"`
}

type MatchSpec struct {
	Extensions []string `json:"extensions,omitempty"`
	Glob       string   `json:"glob,omitempty"`
}

type TargetSpec struct {
	Kind           string      `json:"kind"`
	Section        string      `json:"section,omitempty"`
	Index          *int        `json:"index,omitempty"`
	Range          *TokenRange `json:"range,omitempty"`
	RecordTokens   int         `json:"recordTokens,omitempty"`
	ContextIndex   *int        `json:"contextIndex,omitempty"`
	ImagePathToken *int        `json:"imagePathToken,omitempty"`
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

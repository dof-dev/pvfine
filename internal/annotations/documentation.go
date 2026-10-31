package annotations

import (
	"pvfine/internal/pvf"
	"strings"
)

// ResolveDocumentationRule shares the engine's whole-block field inheritance.
func ResolveDocumentationRule(rule Rule, fields []FieldDefinition) (Rule, error) {
	return resolveFieldRule(rule, fields)
}

// DocumentationMatches uses the same glob and extension semantics as annotations.
func DocumentationMatches(match MatchSpec, filePath string, directory bool) bool {
	extensions := make(map[string]struct{}, len(match.Extensions))
	for _, ext := range match.Extensions {
		extensions[strings.ToLower(ext)] = struct{}{}
	}
	return (compiledRule{rule: Rule{Match: match}, extensions: extensions}).matches(filePath, directory)
}

type DocumentationAnchor struct {
	Rule           Rule
	Start, End     int
	Value, Context string
}

// DocumentationAnchors projects compiled rules through the runtime target engine,
// including implicit fields, repeated records, separators and dynamic widths.
func (e *Engine) DocumentationAnchors(filePath string, view pvf.ScriptView) []DocumentationAnchor {
	result := []DocumentationAnchor{}
	if e == nil {
		return result
	}
	for _, r := range e.rules {
		if !r.matches(filePath, false) {
			continue
		}
		for _, a := range e.editorAnchors(r.rule, view) {
			result = append(result, DocumentationAnchor{r.rule, a.start, a.end, a.value, a.context})
		}
	}
	return result
}

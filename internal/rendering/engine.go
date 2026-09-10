package rendering

import (
	"path"
	"strings"
)

// Engine is an immutable, compiled set of script rendering rules.
type Engine struct {
	document Document
	rules    []compiledRule
}

type compiledRule struct {
	rule        Rule
	extensions  map[string]struct{}
	specificity int
	order       int
}

// Compile validates and compiles a rendering rule document.
func Compile(document Document) (*Engine, error) {
	if document.Rules == nil {
		document.Rules = make([]Rule, 0)
	}
	if err := Validate(document); err != nil {
		return nil, err
	}

	rules := make([]compiledRule, 0, len(document.Rules))
	for order, rule := range document.Rules {
		extensions := make(map[string]struct{}, len(rule.Match.Extensions))
		for _, extension := range rule.Match.Extensions {
			extensions[strings.ToLower(strings.TrimSpace(extension))] = struct{}{}
		}
		specificity := 0
		if len(rule.Match.Extensions) > 0 {
			specificity++
		}
		if strings.TrimSpace(rule.Match.Glob) != "" {
			specificity++
		}
		rules = append(rules, compiledRule{
			rule:        rule,
			extensions:  extensions,
			specificity: specificity,
			order:       order,
		})
	}
	return &Engine{document: document, rules: rules}, nil
}

// Document returns the source document used to build the engine.
func (e *Engine) Document() Document {
	if e == nil {
		return Document{}
	}
	document := e.document
	document.Rules = append([]Rule(nil), e.document.Rules...)
	for index := range document.Rules {
		document.Rules[index].Match.Extensions = append([]string(nil), document.Rules[index].Match.Extensions...)
		if value := document.Rules[index].Format.TokensPerLineIndex; value != nil {
			copy := *value
			document.Rules[index].Format.TokensPerLineIndex = &copy
		}
	}
	return document
}

// FileFormat resolves the best file-scoped rule for filePath. A zero value
// means that the decoder should use its ordinary layout.
func (e *Engine) FileFormat(filePath string) FormatSpec {
	return e.resolve(filePath, "", "file")
}

// SectionFormat resolves the best section-scoped rule for a section name.
// Section names are intentionally matched without considering nesting or
// occurrence, matching the annotation engine's V1 semantics.
func (e *Engine) SectionFormat(filePath, section string) FormatSpec {
	return e.resolve(filePath, section, "section")
}

func (e *Engine) resolve(filePath, section, kind string) FormatSpec {
	if e == nil {
		return FormatSpec{}
	}
	bestSpecificity := -1
	bestOrder := -1
	var format FormatSpec
	for _, compiled := range e.rules {
		rule := compiled.rule
		if rule.Target.Kind != kind || !compiled.matches(filePath) {
			continue
		}
		if kind == "section" && !strings.EqualFold(strings.TrimSpace(rule.Target.Section), strings.TrimSpace(section)) {
			continue
		}
		if compiled.specificity < bestSpecificity ||
			(compiled.specificity == bestSpecificity && compiled.order < bestOrder) {
			continue
		}
		bestSpecificity = compiled.specificity
		bestOrder = compiled.order
		format = compiled.rule.Format
	}
	return format
}

func (r compiledRule) matches(filePath string) bool {
	filePath = normalizePath(filePath)
	if len(r.extensions) > 0 {
		if _, ok := r.extensions[strings.ToLower(path.Ext(filePath))]; !ok {
			return false
		}
	}
	glob := strings.TrimSpace(r.rule.Match.Glob)
	return glob == "" || globMatch(glob, filePath)
}

func normalizePath(value string) string {
	return strings.ToLower(strings.Trim(strings.ReplaceAll(value, "\\", "/"), "/"))
}

func globMatch(pattern, value string) bool {
	patternParts := splitPath(normalizePath(pattern))
	valueParts := splitPath(normalizePath(value))
	type state struct{ pattern, value int }
	memo := make(map[state]bool)
	seen := make(map[state]bool)
	var match func(int, int) bool
	match = func(patternIndex, valueIndex int) bool {
		key := state{pattern: patternIndex, value: valueIndex}
		if seen[key] {
			return memo[key]
		}
		seen[key] = true
		if patternIndex == len(patternParts) {
			memo[key] = valueIndex == len(valueParts)
			return memo[key]
		}
		if patternParts[patternIndex] == "**" {
			if match(patternIndex+1, valueIndex) ||
				(valueIndex < len(valueParts) && match(patternIndex, valueIndex+1)) {
				memo[key] = true
			}
			return memo[key]
		}
		if valueIndex >= len(valueParts) {
			return false
		}
		matched, err := path.Match(patternParts[patternIndex], valueParts[valueIndex])
		if err == nil && matched {
			memo[key] = match(patternIndex+1, valueIndex+1)
		}
		return memo[key]
	}
	return match(0, 0)
}

func splitPath(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, "/")
}

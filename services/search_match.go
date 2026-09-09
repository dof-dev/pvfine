package services

import (
	"strings"
	"unicode/utf8"
)

type searchMatcher struct {
	query   string
	exact   bool
	pattern []rune
}

func newSearchMatcher(query string, exact bool) searchMatcher {
	matcher := searchMatcher{query: query, exact: exact}
	if strings.ContainsAny(query, "*?") {
		matcher.pattern = []rune(query)
	}
	return matcher
}

func (m searchMatcher) match(value string) bool {
	if m.pattern != nil {
		return wildcardMatchPattern(value, m.pattern)
	}
	if m.exact {
		return value == m.query
	}
	return strings.Contains(value, m.query)
}

// wildcardMatch matches the basic wildcard syntax used by explorer search.
// Both * and ? operate on runes so a wildcard does not split a UTF-8 character.
func wildcardMatch(value, pattern string) bool {
	return wildcardMatchPattern(value, []rune(pattern))
}

func wildcardMatchPattern(value string, pattern []rune) bool {
	valueIndex := 0
	patternIndex := 0
	starIndex := -1
	starValueIndex := 0
	for valueIndex < len(value) {
		valueRune, valueSize := utf8.DecodeRuneInString(value[valueIndex:])
		if patternIndex < len(pattern) &&
			(pattern[patternIndex] == '?' || pattern[patternIndex] == valueRune) {
			patternIndex++
			valueIndex += valueSize
			continue
		}
		if patternIndex < len(pattern) && pattern[patternIndex] == '*' {
			starIndex = patternIndex
			starValueIndex = valueIndex
			patternIndex++
			continue
		}
		if starIndex < 0 {
			return false
		}
		patternIndex = starIndex + 1
		_, starValueSize := utf8.DecodeRuneInString(value[starValueIndex:])
		starValueIndex += starValueSize
		valueIndex = starValueIndex
	}

	for patternIndex < len(pattern) && pattern[patternIndex] == '*' {
		patternIndex++
	}
	return patternIndex == len(pattern)
}

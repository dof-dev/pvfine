package pvf

import (
	"math"
	"strconv"
	"strings"
)

const (
	ScriptElementSection = "section"
	ScriptElementToken   = "token"
)

// ScriptView is a tolerant semantic projection of the exact editor text.
// Start and End are JavaScript UTF-16 code-unit offsets.
type ScriptView struct {
	Text     string
	Elements []ScriptElement
}

// ScriptElement describes one opening section tag or one direct value token.
type ScriptElement struct {
	Kind        string
	TokenType   int32
	Value       string
	Section     string
	SectionID   int
	SectionPath []string
	Index       int
	Start       int
	End         int
}

type scriptLexeme struct {
	isSection bool
	closing   bool
	name      string
	tokenType int32
	value     string
	start     int
	end       int
}

type semanticSection struct {
	id         int
	name       string
	path       []string
	paired     bool
	tokenCount int
}

// ParseScriptView parses decompiled or edited TypeScript text without changing
// its formatting. Malformed fragments are skipped in the same tolerant spirit
// as encodeScript, while valid tokens retain their exact editor positions.
func ParseScriptView(text string) ScriptView {
	lexemes := lexScriptText(text)
	pairedNames := make(map[string]bool)
	for _, lexeme := range lexemes {
		if lexeme.isSection && lexeme.closing {
			pairedNames[lexeme.name] = true
		}
	}

	elements := make([]ScriptElement, 0, len(lexemes))
	sections := make([]semanticSection, 0)
	nextSectionID := 1
	for _, lexeme := range lexemes {
		if lexeme.isSection {
			if lexeme.closing {
				for len(sections) > 0 && !sections[len(sections)-1].paired {
					sections = sections[:len(sections)-1]
				}
				for i := len(sections) - 1; i >= 0; i-- {
					if sections[i].paired && sections[i].name == lexeme.name {
						sections = sections[:i]
						break
					}
				}
				continue
			}

			for len(sections) > 0 && !sections[len(sections)-1].paired {
				sections = sections[:len(sections)-1]
			}
			sectionPath := make([]string, 0, len(sections)+1)
			if len(sections) > 0 {
				sectionPath = append(sectionPath, sections[len(sections)-1].path...)
			}
			sectionPath = append(sectionPath, lexeme.name)
			sectionID := nextSectionID
			nextSectionID++
			elements = append(elements, ScriptElement{
				Kind:        ScriptElementSection,
				TokenType:   3,
				Value:       lexeme.name,
				Section:     lexeme.name,
				SectionID:   sectionID,
				SectionPath: append([]string(nil), sectionPath...),
				Index:       -1,
				Start:       lexeme.start,
				End:         lexeme.end,
			})
			sections = append(sections, semanticSection{
				id:     sectionID,
				name:   lexeme.name,
				path:   sectionPath,
				paired: pairedNames[lexeme.name],
			})
			continue
		}

		element := ScriptElement{
			Kind:      ScriptElementToken,
			TokenType: lexeme.tokenType,
			Value:     lexeme.value,
			Index:     -1,
			Start:     lexeme.start,
			End:       lexeme.end,
		}
		if len(sections) > 0 {
			current := &sections[len(sections)-1]
			element.Section = current.name
			element.SectionID = current.id
			element.SectionPath = append([]string(nil), current.path...)
			element.Index = current.tokenCount
			current.tokenCount++
		}
		elements = append(elements, element)
	}

	return ScriptView{Text: text, Elements: elements}
}

func lexScriptText(text string) []scriptLexeme {
	rs := []rune(text)
	offsets := make([]int, len(rs)+1)
	for i, r := range rs {
		offsets[i+1] = offsets[i] + utf16RuneLen(r)
	}
	isWS := func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f'
	}

	lexemes := make([]scriptLexeme, 0)
	for i := 0; i < len(rs); {
		if isWS(rs[i]) {
			i++
			continue
		}
		if rs[i] == '#' {
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
			continue
		}
		if rs[i] == '`' {
			start := i
			value, next, _ := readBacktickString(rs, i)
			lexemes = append(lexemes, scriptLexeme{
				tokenType: 6,
				value:     value,
				start:     offsets[start],
				end:       offsets[next],
			})
			i = next
			continue
		}
		if rs[i] == '{' {
			start := i
			if end := findMarkerEnd(rs, i+1); end >= 0 {
				if typ, value, ok := parseTextMarker(string(rs[start : end+1])); ok {
					lexemes = append(lexemes, scriptLexeme{
						tokenType: typ,
						value:     value,
						start:     offsets[start],
						end:       offsets[end+1],
					})
					i = end + 1
					continue
				}
			}
			i++
			continue
		}
		if rs[i] == '[' {
			start := i
			if end := indexRune(rs, i+1, ']'); end > i {
				tag := string(rs[start : end+1])
				name, closing, ok := parseSectionTag(tag)
				lexemes = append(lexemes, scriptLexeme{
					isSection: ok,
					closing:   closing,
					name:      name,
					tokenType: 3,
					value:     tag,
					start:     offsets[start],
					end:       offsets[end+1],
				})
				i = end + 1
				continue
			}
			i++
			continue
		}

		start := i
		for i < len(rs) && !isWS(rs[i]) && rs[i] != '`' && rs[i] != '{' && rs[i] != '[' {
			i++
		}
		if i == start {
			i++
			continue
		}
		raw := string(rs[start:i])
		typ, value := normalizeTextToken(raw)
		lexemes = append(lexemes, scriptLexeme{
			tokenType: typ,
			value:     value,
			start:     offsets[start],
			end:       offsets[i],
		})
	}
	return lexemes
}

func parseTextMarker(marker string) (int32, string, bool) {
	if len(marker) < 4 || marker[0] != '{' || marker[len(marker)-1] != '}' {
		return 0, "", false
	}
	var typ int32
	switch {
	case strings.HasPrefix(marker, "{5="):
		typ = 5
	case strings.HasPrefix(marker, "{7="):
		typ = 7
	default:
		return 0, "", false
	}
	inner := strings.TrimSpace(marker[3 : len(marker)-1])
	innerRunes := []rune(inner)
	if value, next, ok := readBacktickString(innerRunes, 0); ok && next == len(innerRunes) {
		return typ, value, true
	}
	return typ, inner, true
}

func normalizeTextToken(raw string) (int32, string) {
	if value, err := strconv.ParseInt(raw, 10, 32); err == nil {
		return 0, strconv.FormatInt(value, 10)
	}
	if value, err := strconv.ParseFloat(raw, 32); err == nil {
		return 2, strconv.FormatFloat(float64(float32(value)), 'g', -1, 32)
	}
	return 3, raw
}

func utf16RuneLen(r rune) int {
	if r > math.MaxUint16 {
		return 2
	}
	return 1
}

package pvf

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// ParseListText reads logical version/export text using the script lexer but
// rejects malformed fragments that the interactive editor tolerates.
func ParseListText(text string) ([]ListPair, error) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	tokens := lexScriptText(text)
	if len(tokens)%2 != 0 {
		return nil, fmt.Errorf("incomplete list pair")
	}
	units := utf16.Encode([]rune(text))
	gapOK := func(gap string) bool {
		for _, line := range strings.Split(gap, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				return false
			}
		}
		return true
	}
	end := 0
	for _, token := range tokens {
		if token.isSection || !gapOK(string(utf16.Decode(units[end:token.start]))) {
			return nil, fmt.Errorf("invalid list token")
		}
		raw := string(utf16.Decode(units[token.start:token.end]))
		if token.tokenType == 6 {
			runes := []rune(raw)
			closed := false
			for i := 1; i < len(runes); i++ {
				if runes[i] != '`' {
					continue
				}
				if i+1 < len(runes) && runes[i+1] == '`' {
					i++
					continue
				}
				closed = i == len(runes)-1
				break
			}
			if !closed {
				return nil, fmt.Errorf("unterminated list string")
			}
		}
		end = token.end
	}
	if !gapOK(string(utf16.Decode(units[end:]))) {
		return nil, fmt.Errorf("invalid trailing list text")
	}
	result, seen := []ListPair{}, map[string]bool{}
	for i := 0; i < len(tokens); i += 2 {
		id, value := tokens[i], tokens[i+1]
		validID := id.tokenType == 0 || id.tokenType == 6 || id.tokenType == 8 || id.tokenType == 10
		if !validID || id.isSection || value.isSection || (value.tokenType != 6 && value.tokenType != 8 && value.tokenType != 10) || id.value == "" || value.value == "" {
			return nil, fmt.Errorf("invalid list pair")
		}
		if !seen[id.value] {
			result = append(result, ListPair{ID: id.value, Path: value.value})
			seen[id.value] = true
		}
	}
	return result, nil
}

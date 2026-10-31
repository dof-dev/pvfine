package services

import (
	"fmt"
	"strings"
)

// itemCreationSource validates source boundaries before the permissive script
// encoder runs. Comments are whitespace, never binary tag tokens in a copy.
func itemCreationSource(text string) (string, error) {
	out := []byte(text)
	tags := []string{}
	paired := map[string]bool{"if": true, "then": true, "multiple then": true, "booster info": true, "booster select category": true}
	quoteEnd := func(start int) (int, error) {
		for i := start + 1; i < len(text); i++ {
			if text[i] != '`' {
				continue
			}
			if i+1 < len(text) && text[i+1] == '`' {
				i++
				continue
			}
			return i + 1, nil
		}
		return 0, fmt.Errorf("参考物品存在未闭合文本，请先修复 DSL")
	}
	erase := func(start, end int) {
		for i := start; i < end; i++ {
			if out[i] != '\r' && out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	for i := 0; i < len(text); {
		switch {
		case text[i] == '`':
			end, err := quoteEnd(i)
			if err != nil {
				return "", err
			}
			i = end
		case text[i] == '#' || strings.HasPrefix(text[i:], "//"):
			start := i
			for i < len(text) && text[i] != '\r' && text[i] != '\n' {
				i++
			}
			erase(start, i)
		case strings.HasPrefix(text[i:], "/*"):
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return "", fmt.Errorf("参考物品存在未闭合注释")
			}
			end += i + 4
			erase(i, end)
			i = end
		case text[i] == '{':
			start := i
			i++
			for i < len(text) && text[i] != '}' {
				if text[i] == '`' {
					end, err := quoteEnd(i)
					if err != nil {
						return "", err
					}
					i = end
				} else {
					i++
				}
			}
			if i >= len(text) {
				return "", fmt.Errorf("参考物品存在未闭合类型标记")
			}
			marker := text[start : i+1]
			if !strings.HasPrefix(marker, "{5=") && !strings.HasPrefix(marker, "{7=") && !strings.HasPrefix(marker, "{8=") && !strings.HasPrefix(marker, "{10=") {
				return "", fmt.Errorf("参考物品包含无法识别的类型标记")
			}
			i++
		case text[i] == '[':
			end := strings.IndexByte(text[i+1:], ']')
			if end < 0 {
				return "", fmt.Errorf("参考物品存在未闭合标签")
			}
			end += i + 1
			name := strings.ToLower(strings.TrimSpace(text[i+1 : end]))
			if name == "" || strings.ContainsAny(name, "[\r\n`") {
				return "", fmt.Errorf("参考物品标签格式异常")
			}
			tags = append(tags, name)
			if strings.HasPrefix(name, "/") {
				paired[strings.TrimSpace(name[1:])] = true
			}
			i = end + 1
		default:
			i++
		}
	}
	stack := []string{}
	for _, tag := range tags {
		if strings.HasPrefix(tag, "/") {
			name := strings.TrimSpace(tag[1:])
			if len(stack) == 0 || stack[len(stack)-1] != name {
				return "", fmt.Errorf("参考物品闭合标签不匹配")
			}
			stack = stack[:len(stack)-1]
		} else if paired[tag] {
			stack = append(stack, tag)
		}
	}
	if len(stack) > 0 {
		return "", fmt.Errorf("参考物品存在未闭合容器")
	}
	return string(out), nil
}

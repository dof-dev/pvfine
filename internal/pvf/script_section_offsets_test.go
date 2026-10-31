package pvf

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestScriptSectionClosingOffsetsIgnoresQuotedAndCommentTags(t *testing.T) {
	text := "// 😀 [/character job]\r\n[character job]\r\n`[mage]` `none`\r\n`[/character job]`\r\n// [/character job]\r\n[/character job] // real\r\n[character job]\r\n`[swordman]` `none`"
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	offset := func(index int) int { return len(utf16.Encode([]rune(normalized[:index]))) }
	first := offset(strings.Index(normalized, "[character job]"))
	close := offset(strings.Index(normalized, "[/character job] // real"))
	unclosed := offset(strings.LastIndex(normalized, "[character job]"))
	result := ScriptSectionClosingOffsets(text, "character job")
	if len(result) != 1 || result[first] != close {
		t.Fatalf("错误结束位置：%v，期望%d -> %d", result, first, close)
	}
	if _, found := result[unclosed]; found {
		t.Fatal("不应为未闭合分支制造插入位置")
	}
}

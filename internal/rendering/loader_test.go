package rendering

import (
	"strings"
	"testing"
)

func TestLoadDefault(t *testing.T) {
	engine, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault() error = %v", err)
	}
	rules := engine.Document().Rules
	if len(rules) < 3 {
		t.Fatalf("default rule count = %d, want at least 3", len(rules))
	}
	seen := make(map[string]bool, len(rules))
	for _, rule := range rules {
		seen[rule.ID] = true
	}
	for _, id := range []string{"file.lst", "section.skill-data-up", "section.skill-levelup"} {
		if !seen[id] {
			t.Fatalf("default rules missing %q", id)
		}
	}
}

func TestParseRejectsInvalidDocuments(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "version",
			json: `{"version":2,"rules":[]}`,
			want: "version 必须为 1",
		},
		{
			name: "duplicate id",
			json: `{"version":1,"rules":[{"id":"same","match":{},"target":{"kind":"file"},"format":{"tokensPerLine":2}},{"id":"same","match":{},"target":{"kind":"file"},"format":{"tokensPerLine":3}}]}`,
			want: "id 重复",
		},
		{
			name: "extension",
			json: `{"version":1,"rules":[{"id":"bad","match":{"extensions":["lst"]},"target":{"kind":"file"},"format":{"tokensPerLine":2}}]}`,
			want: "必须是以点开头的文件后缀",
		},
		{
			name: "target",
			json: `{"version":1,"rules":[{"id":"bad","match":{},"target":{"kind":"section"},"format":{"tokensPerLine":2}}]}`,
			want: "target.section 不能为空",
		},
		{
			name: "format",
			json: `{"version":1,"rules":[{"id":"bad","match":{},"target":{"kind":"file"},"format":{"tokensPerLine":0}}]}`,
			want: "tokensPerLine 必须是正数",
		},
		{
			name: "offset",
			json: `{"version":1,"rules":[{"id":"bad","match":{},"target":{"kind":"file"},"format":{"offset":-1,"tokensPerLine":2}}]}`,
			want: "format.offset 不能为负数",
		},
		{
			name: "dynamic file rule",
			json: `{"version":1,"rules":[{"id":"bad","match":{},"target":{"kind":"file"},"format":{"tokensPerLine":2,"tokensPerLineIndex":0}}]}`,
			want: "tokensPerLineIndex 只允许用于 section 规则",
		},
		{
			name: "dynamic index",
			json: `{"version":1,"rules":[{"id":"bad","match":{},"target":{"kind":"section","section":"records"},"format":{"tokensPerLine":2,"tokensPerLineIndex":-1}}]}`,
			want: "tokensPerLineIndex 不能为负数",
		},
		{
			name: "glob",
			json: `{"version":1,"rules":[{"id":"bad","match":{"glob":"["},"target":{"kind":"file"},"format":{"tokensPerLine":2}}]}`,
			want: "match.glob 无效",
		},
		{
			name: "unknown field",
			json: `{"version":1,"rules":[],"extra":true}`,
			want: "解析渲染规则失败",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.json))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestParseRejectsTrailingJSON(t *testing.T) {
	_, err := Parse([]byte(`{"version":1,"rules":[]} {}`))
	if err == nil || !strings.Contains(err.Error(), "只能包含一个 JSON 文档") {
		t.Fatalf("Parse() error = %v", err)
	}
}

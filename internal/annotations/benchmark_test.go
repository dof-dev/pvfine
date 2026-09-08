package annotations

import (
	"fmt"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func BenchmarkAnnotateManyRules(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			rules := make([]Rule, count)
			for i := range rules {
				rules[i] = Rule{
					ID: fmt.Sprint(i), Match: MatchSpec{Extensions: []string{".equ"}},
					Target:     TargetSpec{Kind: "token", Section: fmt.Sprintf("section%d", i), Index: intPtr(0)},
					Annotation: AnnotationSpec{Title: "说明", Type: "text"},
				}
			}
			engine, err := Compile(Document{Version: 1, Rules: rules})
			if err != nil {
				b.Fatal(err)
			}
			var text strings.Builder
			for i := 0; i < 100; i++ {
				fmt.Fprintf(&text, "[section%d]\n1 2 3 4 5\n", i)
			}
			view := pvf.ParseScriptView(text.String())
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				engine.Annotate("a.equ", view, nil)
			}
		})
	}
}

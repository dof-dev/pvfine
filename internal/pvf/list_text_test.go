package pvf

import "testing"

func TestParseListText(t *testing.T) {
	pairs, err := ParseListText("# comment\r\n1\t`中文/😀.stk`\r\n2 {8=`string/a.str`}\n1 `ignored`")
	if err != nil || len(pairs) != 2 || pairs[0].Path != "中文/😀.stk" || pairs[1].ID != "2" {
		t.Fatalf("pairs = %#v, error %v", pairs, err)
	}
	for _, text := range []string{"1", "1 `unterminated", "1 `path` {", "[section]\n1 `path`", "1 5", "1.0 `path`"} {
		if _, err := ParseListText(text); err == nil {
			t.Errorf("accepted malformed list %q", text)
		}
	}
}

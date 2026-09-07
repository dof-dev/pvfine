package pvf

import (
	"bytes"
	"strings"
	"testing"
)

func TestScriptCodecIsInverse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		decoded string
	}{
		{
			name:    "all token forms",
			input:   "# comment\n [section] -42\t1.5 plain `inline``tick` {5=`block``tick`} {7=`7 block`}\n",
			decoded: "[section]\n\t-42\t1.5\nplain\n\t`inline``tick`\n\t{5=`block``tick`}\n\t{7=`7 block`}",
		},
		{
			name:    "unicode strings",
			input:   "[name]\n`测试``物品`\n[icon]\n`item/测试.img`",
			decoded: "[name]\n\t`测试``物品`\n\n[icon]\n\t`item/测试.img`",
		},
		{
			name:    "nested sections",
			input:   "[if]\n[cooltime]\n30000\n[attack type]\n`physical`\n[attack success]\n1\n[/if]\n[then]\n[target]\n`myself` -1\n[/then]",
			decoded: "[if]\n\t[cooltime]\n\t\t30000\n\t[attack type]\n\t\t`physical`\n\t[attack success]\n\t\t1\n[/if]\n\n[then]\n\t[target]\n\t\t`myself`\t-1\n[/then]\n",
		},
		{
			name:    "direct tokens in paired section",
			input:   "[need material]\n3336 80\n[/need material]",
			decoded: "[need material]\n\t3336\t80\n[/need material]\n",
		},
		{
			name:    "empty script",
			input:   "",
			decoded: "",
		},
		{
			name:    "first token has no prefix",
			input:   "`first` 2",
			decoded: "`first`\t2",
		},
		{
			name:    "first block token has no prefix",
			input:   "{5=`first block`}",
			decoded: "{5=`first block`}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := New()
			encoded, err := a.encodeScript(tt.input)
			if err != nil {
				t.Fatalf("encodeScript() error = %v", err)
			}

			if got := a.decodeScript(encoded); got != tt.decoded {
				t.Errorf("decodeScript(encodeScript(input)) = %q, want %q", got, tt.decoded)
			}

			reencoded, err := a.encodeScript(tt.decoded)
			if err != nil {
				t.Fatalf("re-encode decoded script: %v", err)
			}
			if !bytes.Equal(reencoded, encoded) {
				t.Errorf("encodeScript(decodeScript(encoded)) changed payload:\n got: % X\nwant: % X", reencoded, encoded)
			}
		})
	}
}

func TestSkillDataUpFormatting(t *testing.T) {
	rows := []string{
		"`[at fighter]`\t233\t`[all]`\t`[level]`\t0\t`%`\t6",
		"`[at fighter]`\t233\t`[all]`\t`[level]`\t1\t`%`\t6",
		"`[at fighter]`\t233\t`[all]`\t`[cooltime]`\t0\t`%`\t-6",
		"`[at fighter]`\t234\t`[all]`\t`[level]`\t0\t`%`\t10",
		"`[at fighter]`\t234\t`[all]`\t`[level]`\t1\t`%`\t25",
	}
	input := "[skill data up]\n" + strings.Join(rows, "\t") + "\n[/skill data up]"
	want := "[skill data up]\n\t" + strings.Join(rows, "\n\t") + "\n[/skill data up]\n"

	a := New()
	encoded, err := a.encodeScript(input)
	if err != nil {
		t.Fatalf("encodeScript() error = %v", err)
	}
	if got := a.decodeScript(encoded); got != want {
		t.Errorf("skill data up formatting = %q, want %q", got, want)
	}
	reencoded, err := a.encodeScript(want)
	if err != nil {
		t.Fatalf("re-encode formatted skill data up: %v", err)
	}
	if !bytes.Equal(reencoded, encoded) {
		t.Errorf("formatted skill data up changed payload:\n got: % X\nwant: % X", reencoded, encoded)
	}
}

func TestListFileFormatting(t *testing.T) {
	input := "`one` `two` `three` `four` `five`"
	want := "`one`\t`two`\n`three`\t`four`\n`five`"

	a := New()
	encoded, err := a.encodeScript(input)
	if err != nil {
		t.Fatalf("encodeScript() error = %v", err)
	}
	if got := a.decodeScriptForPath(encoded, "skill/test.lst"); got != want {
		t.Errorf("lst formatting = %q, want %q", got, want)
	}
	reencoded, err := a.encodeScript(want)
	if err != nil {
		t.Fatalf("re-encode formatted lst: %v", err)
	}
	if !bytes.Equal(reencoded, encoded) {
		t.Errorf("formatted lst changed payload:\n got: % X\nwant: % X", reencoded, encoded)
	}
}

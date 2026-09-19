package pvf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

// TestPaged110TokenTypes covers the string-pool token kinds the newer client
// emits: 8 (values such as <31::equip_name_1>) and 10 (multi-line command
// text). Both must render as {N=`...`} and survive a text round trip.
func TestPaged110TokenTypes(t *testing.T) {
	a, _ := openPaged110Fixture(t)

	type expect struct {
		path     string
		fragment string
	}
	for _, tc := range []expect{
		{"stackable/10000001/10000039.stk", "{8=`<13::name_10000039>`}"},
		{"aradadventure/equipment/equipment_1.equ", "{8=`<31::equip_name_1>`}"},
		{"aicharacter/_jojochan/atgunner/sky/action/damage1.act", "{10=` rs(1, 0, 0, 0, 1, 0, 1, 0, 0, 1, 0, 0) `}"},
	} {
		idx, ok := a.Find(tc.path)
		if !ok {
			t.Errorf("%s not found", tc.path)
			continue
		}
		text, err := a.Text(idx)
		if err != nil {
			t.Errorf("%s: %v", tc.path, err)
			continue
		}
		if !strings.Contains(text, tc.fragment) {
			t.Errorf("%s: rendered text lacks %q:\n%s", tc.path, tc.fragment, text)
		}
		// Re-encode and compare the token streams: the {8=}/{10=} markers must
		// parse back into types 8/10.
		raw := scriptTokens(t, a, idx)
		again, err := a.encodeScript(text)
		if err != nil {
			t.Errorf("%s: re-encode: %v", tc.path, err)
			continue
		}
		if !bytes.Equal(raw, again) {
			t.Errorf("%s: round trip changed the token stream (%d -> %d bytes)",
				tc.path, len(raw), len(again))
		}
	}
}

// TestPaged110RoundTripSample re-encodes a sample of real scripts and checks
// that the token stream is reproduced byte for byte.
func TestPaged110RoundTripSample(t *testing.T) {
	a, _ := openPaged110Fixture(t)
	checked, mismatches := 0, 0
	for i := int32(0); i < a.FileCount() && checked < 60; i++ {
		f := a.File(i)
		if f.DataType != TypeScript || f.DataSize == 0 {
			continue
		}
		text, err := a.Text(i)
		if err != nil {
			continue
		}
		raw := scriptTokens(t, a, i)
		again, err := a.encodeScript(text)
		if err != nil {
			t.Errorf("%s: re-encode: %v", a.Path(i), err)
			continue
		}
		checked++
		if !bytes.Equal(raw, again) {
			mismatches++
			if mismatches <= 3 {
				t.Errorf("%s: token stream not reproduced (%d -> %d bytes)", a.Path(i), len(raw), len(again))
			}
		}
	}
	if checked < 20 {
		t.Fatalf("only %d files checked", checked)
	}
	if mismatches > 0 {
		t.Errorf("%d of %d files did not round trip", mismatches, checked)
	}
}

// scriptTokens returns the raw 5-byte token stream of entry i.
func scriptTokens(t *testing.T, a *Archive, i int32) []byte {
	t.Helper()
	f := a.File(i)
	chunk, err := a.Chunk(f.ChunkIndex)
	if err != nil {
		t.Fatalf("chunk %d: %v", f.ChunkIndex, err)
	}
	if int(f.DataOffset+f.DataSize) > len(chunk) {
		t.Fatalf("%s: entry outside chunk", a.Path(i))
	}
	return chunk[f.DataOffset : f.DataOffset+f.DataSize]
}

// TestPaged110NameTokensPresent asserts that name-bearing sections are no
// longer rendered with a missing value.
func TestPaged110NameTokensPresent(t *testing.T) {
	a, _ := openPaged110Fixture(t)
	empty := 0
	seen := 0
	for i := int32(0); i < a.FileCount() && seen < 200; i++ {
		p := strings.ToLower(a.Path(i))
		if !strings.HasSuffix(p, ".stk") && !strings.HasSuffix(p, ".equ") {
			continue
		}
		text, err := a.Text(i)
		if err != nil {
			continue
		}
		seen++
		if strings.HasPrefix(text, "[name]\n\n") || strings.Contains(text, "[name]\n\n") {
			empty++
		}
	}
	if seen < 50 {
		t.Fatalf("only %d item files inspected", seen)
	}
	if empty > 0 {
		t.Errorf("%d of %d item files render an empty [name] section", empty, seen)
	}
	_ = binary.LittleEndian
	_ = fmt.Sprintf
}

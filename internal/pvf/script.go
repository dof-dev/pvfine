package pvf

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/korean"
)

type type1Token struct {
	typ   byte
	value int32
}

// encodeScript re-tokenizes decompiled text back into a TypeScript payload,
// ported from the reference implementation: `[tag]` -> tag token, “ `...` “
// -> inline string, `{5=`...`}` / `{7=`...`}` -> block strings, integers /
// floats -> numeric tokens, bare words -> string tokens. `#` starts a comment.
func (a *Archive) encodeScript(text string) ([]byte, error) {
	var tokens []type1Token
	rs := []rune(text)
	n := len(rs)
	isWS := func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f'
	}

	i := 0
	for i < n {
		ch := rs[i]
		if isWS(ch) {
			i++
			continue
		}
		if ch == '#' { // comment to end of line
			for i < n && rs[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '`' {
			if value, next, ok := readBacktickString(rs, i); ok {
				tokens = append(tokens, type1Token{6, a.StringOffset(value)})
				i = next
				continue
			}
		}
		if ch == '{' {
			if end := findMarkerEnd(rs, i+1); end > i {
				marker := strings.TrimSpace(string(rs[i : end+1]))
				if tok, ok := a.tryParseSpecialMarker(marker); ok {
					tokens = append(tokens, tok)
					i = end + 1
					continue
				}
			}
		}
		if ch == '[' {
			if end := indexRune(rs, i+1, ']'); end > i {
				tag := string(rs[i : end+1])
				tokens = append(tokens, type1Token{3, a.StringOffset(tag)})
				i = end + 1
				continue
			}
		}

		start := i
		for i < n && !isWS(rs[i]) && rs[i] != '`' && rs[i] != '{' && rs[i] != '[' {
			i++
		}
		if i == start {
			i++
			continue
		}
		token := string(rs[start:i])
		if v, err := strconv.ParseInt(token, 10, 32); err == nil {
			tokens = append(tokens, type1Token{0, int32(v)})
			continue
		}
		if f, err := strconv.ParseFloat(token, 32); err == nil {
			tokens = append(tokens, type1Token{2, int32(math.Float32bits(float32(f)))})
			continue
		}
		tokens = append(tokens, type1Token{3, a.StringOffset(token)})
	}

	raw := make([]byte, len(tokens)*5)
	for i, t := range tokens {
		raw[i*5] = t.typ
		binary.LittleEndian.PutUint32(raw[i*5+1:], uint32(t.value))
	}
	return raw, nil
}

func (a *Archive) tryParseSpecialMarker(marker string) (type1Token, bool) {
	var tok type1Token
	if len(marker) < 4 || marker[0] != '{' || marker[len(marker)-1] != '}' {
		return tok, false
	}
	var typ byte
	switch {
	case strings.HasPrefix(marker, "{5="):
		typ = 5
	case strings.HasPrefix(marker, "{7="):
		typ = 7
	default:
		return tok, false
	}
	inner := strings.TrimSpace(marker[3 : len(marker)-1])
	innerRunes := []rune(inner)
	if value, next, ok := readBacktickString(innerRunes, 0); ok && next == len(innerRunes) {
		inner = value
	}
	if v, err := strconv.ParseInt(inner, 10, 32); err == nil {
		return type1Token{typ, int32(v)}, true
	}
	return type1Token{typ, a.StringOffset(inner)}, true
}

// readBacktickString reads a quoted string starting at start and unescapes doubled backticks.
func readBacktickString(rs []rune, start int) (value string, next int, ok bool) {
	if start >= len(rs) || rs[start] != '`' {
		return "", start, false
	}
	var sb strings.Builder
	i := start + 1
	for i < len(rs) {
		if rs[i] == '`' {
			if i+1 < len(rs) && rs[i+1] == '`' {
				sb.WriteRune('`')
				i += 2
				continue
			}
			return sb.String(), i + 1, true
		}
		sb.WriteRune(rs[i])
		i++
	}
	return sb.String(), i, true
}

// findMarkerEnd returns the index of the '}' closing the marker that starts
// at start, honoring `...` sections; -1 when unterminated.
func findMarkerEnd(rs []rune, start int) int {
	inBacktick := false
	for i := start; i < len(rs); i++ {
		if rs[i] == '`' {
			if inBacktick && i+1 < len(rs) && rs[i+1] == '`' {
				i++
				continue
			}
			inBacktick = !inBacktick
			continue
		}
		if !inBacktick && rs[i] == '}' {
			return i
		}
	}
	return -1
}

func indexRune(rs []rune, from int, target rune) int {
	for i := from; i < len(rs); i++ {
		if rs[i] == target {
			return i
		}
	}
	return -1
}

// ---- Korean-server mojibake -------------------------------------------------
//
// Korean-server PVFs store .str payloads as UTF-16 code units that are really
// EUC-KR bytes painted through the CP437 font (e.g. 한국 = C7 D1 -> "╟╤").
// fixKoreanMojibake detects that pattern and restores readable Korean;
// encodeKoreanMojibake performs the inverse for byte-level round trips.

const mojibakeProbe = 2000

func looksLikeCP437Painting(s string) bool {
	box, checked := 0, 0
	for _, r := range s {
		if checked >= mojibakeProbe {
			break
		}
		checked++
		if r >= 0x2500 && r <= 0x25FF {
			box++
		}
	}
	if checked == 0 {
		return false
	}
	// Long payloads: absolute count; short ones: dominance ratio.
	return box >= 50 || (checked >= 20 && box*10 >= checked*3)
}

var cp437ByteToRune = func() [256]rune {
	var table [256]rune
	dec := charmap.CodePage437.NewDecoder()
	var in []byte
	for b := 0; b < 256; b++ {
		in = append(in[:0], byte(b))
		out, err := dec.Bytes(in)
		if err != nil || len(out) == 0 {
			table[b] = rune(b) // ASCII fallback
			continue
		}
		rs := []rune(string(out))
		if len(rs) == 1 {
			table[b] = rs[0]
		} else {
			table[b] = rune(b)
		}
	}
	return table
}()

var cp437RuneToByte = func() map[rune]byte {
	m := make(map[rune]byte, 256)
	for b, r := range cp437ByteToRune {
		if _, dup := m[r]; !dup {
			m[r] = byte(b)
		}
	}
	return m
}()

func fixKoreanMojibake(s string) string {
	if !looksLikeCP437Painting(s) {
		return s
	}
	raw := make([]byte, 0, len(s))
	for _, r := range s {
		if b, ok := cp437RuneToByte[r]; ok {
			raw = append(raw, b)
		} else {
			raw = append(raw, '?')
		}
	}
	dec := korean.EUCKR.NewDecoder()
	out, err := dec.Bytes(raw)
	if err != nil {
		return s
	}
	return string(out)
}

// EncodeKoreanMojibake converts readable EUC-KR text into the byte-level form
// Korean-server PVFs use, for lossless editing of such payloads via
// SetRawBytes.
func EncodeKoreanMojibake(s string) ([]byte, error) {
	enc := korean.EUCKR.NewEncoder()
	b, err := enc.Bytes([]byte(s))
	if err != nil {
		return nil, err
	}
	u16 := make([]uint16, 0, len(b))
	for _, by := range b {
		u16 = append(u16, uint16(cp437ByteToRune[by]))
	}
	out := make([]byte, len(u16)*2)
	for i, u := range u16 {
		binary.LittleEndian.PutUint16(out[i*2:], u)
	}
	return out, nil
}

package services

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/saintfish/chardet"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/simplifiedchinese"
	textunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
)

// decodeImportText normalizes source text without changing the source bytes.
// Unicode BOMs take precedence, since UTF-16/32 can also be valid UTF-8 bytes.
func decodeImportText(data []byte) (string, string, error) {
	for _, bom := range []struct {
		prefix []byte
		name   string
		codec  encoding.Encoding
	}{
		{[]byte{0xff, 0xfe, 0, 0}, "UTF-32LE", utf32.UTF32(utf32.LittleEndian, utf32.IgnoreBOM)},
		{[]byte{0, 0, 0xfe, 0xff}, "UTF-32BE", utf32.UTF32(utf32.BigEndian, utf32.IgnoreBOM)},
		{[]byte{0xff, 0xfe}, "UTF-16LE", textunicode.UTF16(textunicode.LittleEndian, textunicode.IgnoreBOM)},
		{[]byte{0xfe, 0xff}, "UTF-16BE", textunicode.UTF16(textunicode.BigEndian, textunicode.IgnoreBOM)},
	} {
		if bytes.HasPrefix(data, bom.prefix) {
			text, err := decodeImportEncoding(data[len(bom.prefix):], bom.codec)
			return text, bom.name, err
		}
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) && !utf8.Valid(data) {
		return "", "", fmt.Errorf("UTF-8 BOM 文件包含无效字节")
	}
	// Script syntax gives BOM-less UTF-16 a strong alternating-NUL signal.
	if len(data) >= 4 && len(data)%2 == 0 {
		even, odd := 0, 0
		for i, b := range data {
			if b == 0 {
				if i%2 == 0 {
					even++
				} else {
					odd++
				}
			}
		}
		for _, candidate := range []struct {
			name         string
			zeros, other int
			order        textunicode.Endianness
		}{
			{"UTF-16LE", odd, even, textunicode.LittleEndian},
			{"UTF-16BE", even, odd, textunicode.BigEndian},
		} {
			if candidate.zeros*4 >= len(data) && candidate.other*10 < candidate.zeros {
				text, err := decodeImportEncoding(data, textunicode.UTF16(candidate.order, textunicode.IgnoreBOM))
				if err == nil {
					return text, candidate.name, nil
				}
			}
		}
	}
	if utf8.Valid(data) && validImportText(string(data)) {
		return strings.TrimPrefix(string(data), "\uFEFF"), "UTF-8", nil
	}
	// NULs are usually binary data or BOM-less UTF-16/32, not a legacy charset.
	if bytes.IndexByte(data, 0) >= 0 {
		return "", "", fmt.Errorf("无法可靠识别文本编码，请先将文件转换为 UTF-8（Unicode 文件请保留 BOM）")
	}
	// Bound detector work for large files. Decoding still validates the entire file.
	sample := data
	if len(sample) > 64<<10 {
		sample = sample[:64<<10]
	}
	candidates, err := chardet.NewTextDetector().DetectAll(sample)
	if err != nil {
		return "", "", fmt.Errorf("无法识别文本编码，请先将文件转换为 UTF-8")
	}
	// Prefer the archive's common East Asian encodings over statistical guesses
	// for single-byte Western encodings on short Chinese/Korean scripts.
	priority := map[string]int{"GB-18030": 1, "Big5": 2, "EUC-KR": 3, "Shift_JIS": 4, "EUC-JP": 5}
	sort.SliceStable(candidates, func(i, j int) bool {
		// Strong statistical evidence takes precedence over the regional default.
		if candidates[i].Confidence != candidates[j].Confidence && (candidates[i].Confidence >= 50 || candidates[j].Confidence >= 50) {
			return candidates[i].Confidence > candidates[j].Confidence
		}
		left, right := priority[candidates[i].Charset], priority[candidates[j].Charset]
		if (left > 0) != (right > 0) {
			return left > 0
		}
		if candidates[i].Confidence != candidates[j].Confidence {
			return candidates[i].Confidence > candidates[j].Confidence
		}
		if left != right {
			return left < right
		}
		return candidates[i].Charset < candidates[j].Charset
	})
	for _, candidate := range candidates {
		if candidate.Confidence < 10 || (priority[candidate.Charset] == 0 && candidate.Confidence < 50) {
			continue
		}
		codec, err := htmlindex.Get(candidate.Charset)
		if candidate.Charset == "GB-18030" {
			codec, err = simplifiedchinese.GB18030, nil
		}
		if err != nil {
			continue
		}
		text, err := decodeImportEncoding(data, codec)
		if err == nil {
			name := candidate.Charset
			if name == "GB-18030" {
				name = "GB18030"
				if encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(text)); err == nil && bytes.Equal(encoded, data) {
					name = "GBK"
				}
			}
			return text, name, nil
		}
	}
	return "", "", fmt.Errorf("无法可靠识别文本编码或文件包含无效字节，请先将文件转换为 UTF-8")
}

func decodeImportEncoding(data []byte, codec encoding.Encoding) (string, error) {
	decoded, err := codec.NewDecoder().Bytes(data)
	if err != nil || !utf8.Valid(decoded) || !validImportText(string(decoded)) || bytes.Contains(decoded, []byte("\uFFFD")) {
		return "", fmt.Errorf("文本编码无效，无法无损转换为 UTF-8")
	}
	// Many x/text decoders replace malformed input without returning an error.
	// Check reversibility to avoid silently accepting truncated/invalid sequences.
	encoded, err := codec.NewEncoder().Bytes(decoded)
	if err != nil || !bytes.Equal(encoded, data) {
		return "", fmt.Errorf("文本包含无效字节，无法无损转换为 UTF-8")
	}
	return string(decoded), nil
}

func validImportText(text string) bool {
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func (file *importFile) decodeText() error {
	text, sourceEncoding, err := decodeImportText(file.data)
	if err != nil {
		return fmt.Errorf("文本文件 %q: %w", file.sourcePath, err)
	}
	file.text = text
	file.sourceEncoding = sourceEncoding
	return nil
}

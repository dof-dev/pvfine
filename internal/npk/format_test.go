package npk

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image/color"
	"os"
	"testing"
)

func putInt32(data *[]byte, values ...int32) {
	for _, value := range values {
		var raw [4]byte
		binary.LittleEndian.PutUint32(raw[:], uint32(value))
		*data = append(*data, raw[:]...)
	}
}

func makeIMG(version int32, headers, payload []byte, imagesSize int32) []byte {
	return makeIMGCount(version, headers, payload, imagesSize, 1)
}

func makeIMGCount(version int32, headers, payload []byte, imagesSize, count int32) []byte {
	data := make([]byte, 0, 32+len(headers)+len(payload)+128)
	if version == 1 {
		data = append(data, []byte(imgMagicOld)...)
		data = append(data, 0)
		data = append(data, 0, 0)
		putInt32(&data, 0, version, count)
	} else {
		data = append(data, []byte(imgMagic)...)
		data = append(data, 0)
		putInt32(&data, imagesSize, 0, version, count)
	}
	data = append(data, headers...)
	data = append(data, payload...)
	return data
}

func imageHeader(format, extra, width, height, size int32) []byte {
	data := make([]byte, 0, 36)
	putInt32(&data, format, extra, width, height, size, 0, 0, width, height)
	return data
}

func onePixelIMG(format int32, raw []byte) []byte {
	header := imageHeader(format, ExtraNone, 1, 1, int32(len(raw)))
	return makeIMG(2, header, raw, int32(len(header)))
}

func decodeOnePixel(t *testing.T, data []byte) color.NRGBA {
	t.Helper()
	parsed, err := ParseIMGAt(bytes.NewReader(data), 0, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeImage(bytes.NewReader(data), 0, int64(len(data)), parsed, 0)
	if err != nil {
		t.Fatal(err)
	}
	pixel, ok := decoded.At(0, 0).(color.NRGBA)
	if !ok {
		return color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA)
	}
	return pixel
}

func TestIMGRawFormatsAndZlib(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want color.NRGBA
	}{
		{name: "1555", data: onePixelIMG(Format1555, []byte{0, 0xfc}), want: color.NRGBA{R: 255, A: 255}},
		{name: "4444", data: onePixelIMG(Format4444, []byte{0, 0xff}), want: color.NRGBA{R: 240, A: 240}},
		{name: "8888", data: onePixelIMG(Format8888, []byte{3, 2, 1, 255}), want: color.NRGBA{R: 1, G: 2, B: 3, A: 255}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := decodeOnePixel(t, test.data); got != test.want {
				t.Fatalf("pixel = %#v, want %#v", got, test.want)
			}
		})
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, _ = writer.Write([]byte{3, 2, 1, 255})
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	header := imageHeader(Format8888, ExtraZlib, 1, 1, int32(compressed.Len()))
	if got := decodeOnePixel(t, makeIMG(2, header, compressed.Bytes(), int32(len(header)))); got != (color.NRGBA{R: 1, G: 2, B: 3, A: 255}) {
		t.Fatalf("zlib pixel = %#v", got)
	}
	if got := decodeOnePixel(t, makeIMG(1, imageHeader(Format8888, ExtraNone, 1, 1, 4), []byte{3, 2, 1, 255}, 0)); got != (color.NRGBA{R: 1, G: 2, B: 3, A: 255}) {
		t.Fatalf("v1 pixel = %#v", got)
	}
}

func TestIMGLinkPaletteDXTAndSprite(t *testing.T) {
	ordinary := imageHeader(Format8888, ExtraNone, 1, 1, 4)
	link := make([]byte, 0, 8)
	putInt32(&link, FormatLink, 0)
	linkIMG := makeIMGCount(2, append(append([]byte{}, ordinary...), link...), []byte{3, 2, 1, 255}, int32(len(ordinary)+len(link)), 2)
	parsed, err := ParseIMGAt(bytes.NewReader(linkIMG), 0, int64(len(linkIMG)))
	if err != nil {
		t.Fatal(err)
	}
	linked, err := DecodeImage(bytes.NewReader(linkIMG), 0, int64(len(linkIMG)), parsed, 1)
	if err != nil || linked.Bounds().Dx() != 1 {
		t.Fatalf("link decode = %v, %v", linked, err)
	}

	palette := make([]byte, 0, 16)
	putInt32(&palette, 2)
	palette = append(palette, 1, 2, 3, 255)
	palette = append(palette, 9, 8, 7, 255)
	var paletteCompressed bytes.Buffer
	paletteWriter := zlib.NewWriter(&paletteCompressed)
	_, _ = paletteWriter.Write([]byte{1})
	_ = paletteWriter.Close()
	paletteHeader := imageHeader(Format8888, ExtraZlib, 1, 1, int32(paletteCompressed.Len()))
	paletteIMG := make([]byte, 0)
	paletteIMG = append(paletteIMG, []byte(imgMagic)...)
	paletteIMG = append(paletteIMG, 0)
	putInt32(&paletteIMG, int32(len(paletteHeader)), 0, 4, 1)
	paletteIMG = append(paletteIMG, palette...)
	paletteIMG = append(paletteIMG, paletteHeader...)
	paletteIMG = append(paletteIMG, paletteCompressed.Bytes()...)
	paletteParsed, err := ParseIMGAt(bytes.NewReader(paletteIMG), 0, int64(len(paletteIMG)))
	if err != nil {
		t.Fatal(err)
	}
	paletteDecoded, err := DecodeImage(bytes.NewReader(paletteIMG), 0, int64(len(paletteIMG)), paletteParsed, 0)
	if err != nil || paletteDecoded.At(0, 0) != (color.NRGBA{R: 9, G: 8, B: 7, A: 255}) {
		t.Fatalf("palette decode = %v, %v", paletteDecoded, err)
	}

	dxtHeader := imageHeader(FormatDXT1, ExtraNone, 4, 4, 8)
	dxt := make([]byte, 8)
	binary.LittleEndian.PutUint16(dxt[0:], 0xf800)
	binary.LittleEndian.PutUint16(dxt[2:], 0x0000)
	var dxtCompressed bytes.Buffer
	dxtWriter := zlib.NewWriter(&dxtCompressed)
	_, _ = dxtWriter.Write(dxt)
	_ = dxtWriter.Close()
	dxtHeader = imageHeader(FormatDXT1, ExtraZlib, 4, 4, int32(dxtCompressed.Len()))
	dxtIMG := makeIMG(2, dxtHeader, dxtCompressed.Bytes(), int32(len(dxtHeader)))
	if got := decodeOnePixel(t, dxtIMG); got != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("DXT decode = %#v", got)
	}
	for _, format := range []int32{FormatDXT3, FormatDXT5} {
		block := make([]byte, 16)
		for index := 0; index < 8; index++ {
			block[index] = 0xff
		}
		binary.LittleEndian.PutUint16(block[8:], 0xf800)
		binary.LittleEndian.PutUint16(block[10:], 0)
		var compressed bytes.Buffer
		writer := zlib.NewWriter(&compressed)
		_, _ = writer.Write(block)
		_ = writer.Close()
		header := imageHeader(format, ExtraZlib, 4, 4, int32(compressed.Len()))
		if got := decodeOnePixel(t, makeIMG(2, header, compressed.Bytes(), int32(len(header)))); got != (color.NRGBA{R: 255, A: 255}) {
			t.Fatalf("DXT%d decode = %#v", format, got)
		}
	}

	spriteRaw := []byte{0, 0, 255, 255, 255, 0, 0, 255}
	var spriteCompressed bytes.Buffer
	spriteWriter := zlib.NewWriter(&spriteCompressed)
	_, _ = spriteWriter.Write(spriteRaw)
	_ = spriteWriter.Close()
	sprite := make([]byte, 0)
	putInt32(&sprite, 0, Format8888, 0, int32(spriteCompressed.Len()), 8, 2, 1)
	spriteImage := imageHeader(Format8888, ExtraSprite, 2, 1, 0)
	putInt32(&spriteImage, 0, 0, 0, 0, 2, 1, 1)
	spriteIMG := make([]byte, 0)
	spriteIMG = append(spriteIMG, []byte(imgMagic)...)
	spriteIMG = append(spriteIMG, 0)
	putInt32(&spriteIMG, int32(len(spriteImage)), 0, 5, 1)
	putInt32(&spriteIMG, 1, int32(spriteCompressed.Len()))
	putInt32(&spriteIMG, 0)
	spriteIMG = append(spriteIMG, sprite...)
	spriteIMG = append(spriteIMG, spriteImage...)
	spriteIMG = append(spriteIMG, spriteCompressed.Bytes()...)
	spriteParsed, err := ParseIMGAt(bytes.NewReader(spriteIMG), 0, int64(len(spriteIMG)))
	if err != nil {
		t.Fatal(err)
	}
	spriteDecoded, err := DecodeImage(bytes.NewReader(spriteIMG), 0, int64(len(spriteIMG)), spriteParsed, 0)
	if err != nil || spriteDecoded.Bounds().Dx() != 1 || spriteDecoded.Bounds().Dy() != 2 {
		t.Fatalf("sprite decode = %v, %v", spriteDecoded, err)
	}

	cycleHeaders := make([]byte, 0, 16)
	putInt32(&cycleHeaders, FormatLink, 1, FormatLink, 0)
	cycleIMG := makeIMGCount(2, cycleHeaders, nil, int32(len(cycleHeaders)), 2)
	cycleParsed, err := ParseIMGAt(bytes.NewReader(cycleIMG), 0, int64(len(cycleIMG)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeImage(bytes.NewReader(cycleIMG), 0, int64(len(cycleIMG)), cycleParsed, 0); err == nil {
		t.Fatal("link cycle should fail")
	}

	var v6Compressed bytes.Buffer
	v6Writer := zlib.NewWriter(&v6Compressed)
	_, _ = v6Writer.Write([]byte{1})
	_ = v6Writer.Close()
	v6Header := imageHeader(Format8888, ExtraZlib, 1, 1, int32(v6Compressed.Len()))
	v6IMG := make([]byte, 0)
	v6IMG = append(v6IMG, []byte(imgMagic)...)
	v6IMG = append(v6IMG, 0)
	putInt32(&v6IMG, int32(len(v6Header)), 0, 6, 1)
	// The v6 color board is a count followed by RGBA colors.
	putInt32(&v6IMG, 1, 2)
	v6IMG = append(v6IMG, 1, 2, 3, 255, 2, 3, 4, 255)
	v6IMG = append(v6IMG, v6Header...)
	v6IMG = append(v6IMG, v6Compressed.Bytes()...)
	v6Parsed, err := ParseIMGAt(bytes.NewReader(v6IMG), 0, int64(len(v6IMG)))
	if err != nil {
		t.Fatal(err)
	}
	v6Decoded, err := DecodeImage(bytes.NewReader(v6IMG), 0, int64(len(v6IMG)), v6Parsed, 0)
	if err != nil || v6Decoded.At(0, 0) != (color.NRGBA{R: 2, G: 3, B: 4, A: 255}) {
		t.Fatalf("v6 decode = %v, %v", v6Decoded, err)
	}
}

func TestNPKFilenameXORAndBadFile(t *testing.T) {
	name := "sprite/item/test.img"
	data := make([]byte, 0, 284)
	data = append(data, []byte(npkMagic)...)
	data = append(data, 0)
	putInt32(&data, 1)
	putInt32(&data, 284, 0)
	encrypted := make([]byte, 256)
	plain := []byte(name)
	for index := range encrypted {
		value := byte(0)
		if index < len(plain) {
			value = plain[index]
		}
		encrypted[index] = value ^ npkFilenameKey[index]
	}
	data = append(data, encrypted...)
	parsed, err := Open(writeTempBytes(t, data))
	if err != nil || len(parsed.Entries) != 1 || parsed.Entries[0].Name != name {
		t.Fatalf("NPK = %#v, %v", parsed, err)
	}
	if _, err := Open(writeTempBytes(t, []byte("bad"))); err == nil {
		t.Fatal("bad NPK should fail")
	}
}

func writeTempBytes(t *testing.T, data []byte) string {
	t.Helper()
	path := t.TempDir() + "/test.npk"
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

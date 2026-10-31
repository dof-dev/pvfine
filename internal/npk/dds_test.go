package npk

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image/color"
	"testing"
)

func ddsTexture(format string, width, height uint32, blocks []byte) []byte {
	data := make([]byte, 128)
	copy(data, "DDS ")
	binary.LittleEndian.PutUint32(data[4:8], 124)
	binary.LittleEndian.PutUint32(data[12:16], height)
	binary.LittleEndian.PutUint32(data[16:20], width)
	binary.LittleEndian.PutUint32(data[76:80], 32)
	binary.LittleEndian.PutUint32(data[80:84], 4)
	copy(data[84:88], format)
	return append(data, blocks...)
}

func TestSpriteDDSHeaderAndAtlasCrop(t *testing.T) {
	for _, tc := range []struct {
		name              string
		format            int32
		blockSize, colors int
	}{{"DXT1", FormatDXT1, 8, 0}, {"DXT3", FormatDXT3, 16, 8}, {"DXT5", FormatDXT5, 16, 8}} {
		for _, wrapped := range []bool{false, true} {
			name := tc.name + "/raw"
			if wrapped {
				name = tc.name + "/dds"
			}
			t.Run(name, func(t *testing.T) {
				// Two atlas blocks: red at x=0, green at x=4. The frame selects green.
				blocks := make([]byte, tc.blockSize*2)
				for i, c := range []uint16{0xf800, 0x07e0} {
					block := blocks[i*tc.blockSize : (i+1)*tc.blockSize]
					if tc.format == FormatDXT3 {
						for j := 0; j < 8; j++ {
							block[j] = 255
						}
					}
					if tc.format == FormatDXT5 {
						block[0] = 255
					}
					binary.LittleEndian.PutUint16(block[tc.colors:], c)
				}
				payload := blocks
				if wrapped {
					payload = ddsTexture(tc.name, 8, 4, blocks)
				}
				var compressed bytes.Buffer
				writer := zlib.NewWriter(&compressed)
				_, _ = writer.Write(payload)
				_ = writer.Close()
				prefix := []byte{}
				putInt32(&prefix, 1, int32(compressed.Len()), 0) // sprite count, data size, palette count
				putInt32(&prefix, 1, tc.format, 0, int32(compressed.Len()), int32(len(payload)), 8, 4)
				frame := imageHeader(tc.format, ExtraSprite, 4, 4, 0)
				putInt32(&frame, 0, 0, 4, 0, 8, 4, 0)
				data := makeIMG(5, append(prefix, frame...), compressed.Bytes(), int32(len(frame)))
				parsed, err := ParseIMGAt(bytes.NewReader(data), 0, int64(len(data)))
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := DecodeImage(bytes.NewReader(data), 0, int64(len(data)), parsed, 0)
				if err != nil {
					t.Fatal(err)
				}
				if decoded.Bounds().Dx() != 4 || decoded.Bounds().Dy() != 4 {
					t.Fatal(decoded.Bounds())
				}
				for y := 0; y < 4; y++ {
					for x := 0; x < 4; x++ {
						got := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
						if got != (color.NRGBA{G: 255, A: 255}) {
							t.Fatalf("裁剪到相邻图标或文件头：(%d,%d)=%+v", x, y, got)
						}
					}
				}
			})
		}
	}
}

func TestSpriteDDSInvalidHeaders(t *testing.T) {
	valid := ddsTexture("DXT1", 4, 4, make([]byte, 8))
	for _, tc := range []struct {
		name   string
		data   []byte
		sprite SpriteMeta
	}{
		{"truncated", []byte("DDS "), SpriteMeta{Format: FormatDXT1, Width: 4, Height: 4}},
		{"dimensions", valid, SpriteMeta{Format: FormatDXT1, Width: 8, Height: 4}},
		{"format", valid, SpriteMeta{Format: FormatDXT5, Width: 4, Height: 4}},
		{"unsupported", ddsTexture("DX10", 4, 4, nil), SpriteMeta{Format: FormatDXT1, Width: 4, Height: 4}},
		{"pixels", valid[:128], SpriteMeta{Format: FormatDXT1, Width: 4, Height: 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeSpriteTexture(tc.data, tc.sprite); err == nil {
				t.Fatal("应拒绝无效DDS")
			}
		})
	}
}

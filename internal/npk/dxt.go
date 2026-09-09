package npk

import (
	"encoding/binary"
	"fmt"
	"image"
)

func decodeDXT(data []byte, format int32, width, height int) (image.Image, error) {
	blockBytes := 8
	if format == FormatDXT5 {
		blockBytes = 16
	} else if format == FormatDXT3 {
		blockBytes = 16
	}
	blocksWide := (width + 3) / 4
	blocksHigh := (height + 3) / 4
	needed := blocksWide * blocksHigh * blockBytes
	if needed < 0 || needed > len(data) {
		return nil, fmt.Errorf("%w: DXT 数据过短", ErrBadImageData)
	}
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	for blockY := 0; blockY < blocksHigh; blockY++ {
		for blockX := 0; blockX < blocksWide; blockX++ {
			offset := (blockY*blocksWide + blockX) * blockBytes
			var pixels [16]Color
			var err error
			switch format {
			case FormatDXT1:
				pixels, err = decodeDXT1Block(data[offset : offset+8])
			case FormatDXT3:
				pixels, err = decodeDXT3Block(data[offset : offset+16])
			case FormatDXT5:
				pixels, err = decodeDXT5Block(data[offset : offset+16])
			default:
				return nil, fmt.Errorf("%w: DXT format=%d", ErrUnsupported, format)
			}
			if err != nil {
				return nil, err
			}
			for row := 0; row < 4; row++ {
				for col := 0; col < 4; col++ {
					x := blockX*4 + col
					y := blockY*4 + row
					if x < width && y < height {
						out.SetNRGBA(x, y, colorNRGBA(pixels[row*4+col]))
					}
				}
			}
		}
	}
	return out, nil
}

func decodeDXT1Block(data []byte) ([16]Color, error) {
	var result [16]Color
	if len(data) < 8 {
		return result, ErrBadImageData
	}
	c0 := binary.LittleEndian.Uint16(data[0:2])
	c1 := binary.LittleEndian.Uint16(data[2:4])
	colors := dxtColors(c0, c1, c0 > c1)
	indices := binary.LittleEndian.Uint32(data[4:8])
	for index := 0; index < 16; index++ {
		code := (indices >> (2 * index)) & 3
		result[index] = colors[code]
	}
	return result, nil
}

func decodeDXT3Block(data []byte) ([16]Color, error) {
	var result [16]Color
	if len(data) < 16 {
		return result, ErrBadImageData
	}
	colors := dxtColors(binary.LittleEndian.Uint16(data[8:10]), binary.LittleEndian.Uint16(data[10:12]), true)
	indices := binary.LittleEndian.Uint32(data[12:16])
	for index := 0; index < 16; index++ {
		code := (indices >> (2 * index)) & 3
		alphaByte := data[index/2]
		alpha := alphaByte >> 4
		if index%2 == 0 {
			alpha = alphaByte & 0x0f
		}
		pixel := colors[code]
		pixel[3] = alpha * 17
		result[index] = pixel
	}
	return result, nil
}

func decodeDXT5Block(data []byte) ([16]Color, error) {
	var result [16]Color
	if len(data) < 16 {
		return result, ErrBadImageData
	}
	alpha := [8]uint8{data[0], data[1]}
	if alpha[0] > alpha[1] {
		for index := 2; index < 8; index++ {
			alpha[index] = uint8(((8-index)*int(alpha[0]) + (index-1)*int(alpha[1])) / 7)
		}
	} else {
		for index := 2; index < 6; index++ {
			alpha[index] = uint8(((6-index)*int(alpha[0]) + (index-1)*int(alpha[1])) / 5)
		}
		alpha[6], alpha[7] = 0, 255
	}
	alphaBits := uint64(0)
	for index := 0; index < 6; index++ {
		alphaBits |= uint64(data[2+index]) << (8 * index)
	}
	colors := dxtColors(binary.LittleEndian.Uint16(data[8:10]), binary.LittleEndian.Uint16(data[10:12]), true)
	indices := binary.LittleEndian.Uint32(data[12:16])
	for index := 0; index < 16; index++ {
		colorIndex := (indices >> (2 * index)) & 3
		alphaIndex := (alphaBits >> (3 * index)) & 7
		pixel := colors[colorIndex]
		pixel[3] = alpha[alphaIndex]
		result[index] = pixel
	}
	return result, nil
}

func dxtColors(c0, c1 uint16, four bool) [4]Color {
	colors := [4]Color{rgb565(c0), rgb565(c1), {}, {}}
	if four {
		colors[2] = Color{
			uint8((2*int(colors[0][0]) + int(colors[1][0])) / 3),
			uint8((2*int(colors[0][1]) + int(colors[1][1])) / 3),
			uint8((2*int(colors[0][2]) + int(colors[1][2])) / 3),
			255,
		}
		colors[3] = Color{
			uint8((int(colors[0][0]) + 2*int(colors[1][0])) / 3),
			uint8((int(colors[0][1]) + 2*int(colors[1][1])) / 3),
			uint8((int(colors[0][2]) + 2*int(colors[1][2])) / 3),
			255,
		}
	} else {
		colors[2] = Color{
			uint8((int(colors[0][0]) + int(colors[1][0])) / 2),
			uint8((int(colors[0][1]) + int(colors[1][1])) / 2),
			uint8((int(colors[0][2]) + int(colors[1][2])) / 2),
			255,
		}
		colors[3] = Color{}
	}
	return colors
}

func rgb565(value uint16) Color {
	r := uint8((value >> 11) & 0x1f)
	g := uint8((value >> 5) & 0x3f)
	b := uint8(value & 0x1f)
	return Color{(r << 3) | (r >> 2), (g << 2) | (g >> 4), (b << 3) | (b >> 2), 255}
}

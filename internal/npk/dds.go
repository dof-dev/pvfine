package npk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
)

// IMG v5 sprite payloads may contain a complete DDS texture after zlib
// decompression. Its 128-byte header is not part of the DXT block stream.
// Raw sprite payloads (including older 8888 sprites) have no such wrapper.
func decodeSpriteTexture(data []byte, sprite SpriteMeta) (image.Image, error) {
	if bytes.HasPrefix(data, []byte("DDS ")) {
		if len(data) < 128 || binary.LittleEndian.Uint32(data[4:8]) != 124 || binary.LittleEndian.Uint32(data[76:80]) != 32 {
			return nil, fmt.Errorf("%w: 无效 DDS 文件头", ErrBadImageData)
		}
		width, height := binary.LittleEndian.Uint32(data[16:20]), binary.LittleEndian.Uint32(data[12:16])
		if width != uint32(sprite.Width) || height != uint32(sprite.Height) {
			return nil, fmt.Errorf("%w: DDS 与 sprite 尺寸不匹配", ErrBadImageData)
		}
		var format int32
		switch string(data[84:88]) {
		case "DXT1":
			format = FormatDXT1
		case "DXT3":
			format = FormatDXT3
		case "DXT5":
			format = FormatDXT5
		default:
			return nil, fmt.Errorf("%w: DDS 像素格式 %q", ErrUnsupported, data[84:88])
		}
		if binary.LittleEndian.Uint32(data[80:84])&4 == 0 || format != sprite.Format {
			return nil, fmt.Errorf("%w: DDS 与 sprite 像素格式不匹配", ErrBadImageData)
		}
		data = data[128:]
	}
	return decodePixels(data, sprite.Format, sprite.Width, sprite.Height, nil)
}

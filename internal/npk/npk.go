// Package npk contains the read-only NPK/IMG decoder used by the icon index.
//
// The indexer only parses headers and keeps offsets. Pixel data is read and
// decoded by DecodeImage when a caller actually asks for one frame.
package npk

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"strings"

	"golang.org/x/text/encoding/korean"
)

const (
	npkMagic      = "NeoplePack_Bill"
	imgMagic      = "Neople Img File"
	imgMagicOld   = "Neople Image File"
	maxNPKEntries = 4_000_000
	maxIMGImages  = 2_000_000
	maxDimension  = 16_384
	maxPixels     = 268_000_000
)

const (
	Format1555 int32 = 14
	Format4444 int32 = 15
	Format8888 int32 = 16
	FormatLink int32 = 17
	FormatDXT1 int32 = 18
	FormatDXT3 int32 = 19
	FormatDXT5 int32 = 20
)

const (
	ExtraNone   int32 = 5
	ExtraZlib   int32 = 6
	ExtraSprite int32 = 7
)

var (
	ErrNotNPK       = errors.New("不是有效的 NPK 文件")
	ErrNotIMG       = errors.New("不是有效的 IMG 文件")
	ErrUnsupported  = errors.New("不支持的 IMG 版本或图片格式")
	ErrBadImageData = errors.New("IMG 图片数据损坏")
)

var npkFilenameKey = []byte("puchikon@neople dungeon and fighter " + strings.Repeat("DNF", 73) + "\x00")

// Entry is an NPK member. Offset and Size are absolute byte positions in the
// NPK file, as stored by the format.
type Entry struct {
	Name   string `json:"name"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
}

// NPK is the cheap, metadata-only representation of an NPK file.
type NPK struct {
	Path    string  `json:"-"`
	Size    int64   `json:"size"`
	Entries []Entry `json:"entries"`
}

// Open parses the NPK header and member table without loading member data.
func Open(path string) (*NPK, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() < 20 {
		return nil, ErrNotNPK
	}
	magic, pos, err := readCStringAt(file, 0, 16)
	if err != nil || magic != npkMagic {
		return nil, ErrNotNPK
	}
	var count int32
	if err := readAt(file, pos, &count); err != nil || count < 0 || count > maxNPKEntries {
		return nil, fmt.Errorf("%w: 无效文件数量", ErrNotNPK)
	}
	pos += 4
	if int64(count)*264 > info.Size()-pos {
		return nil, fmt.Errorf("%w: 文件表越界", ErrNotNPK)
	}
	entries := make([]Entry, 0, count)
	for index := int32(0); index < count; index++ {
		var raw [8]byte
		if _, err := file.ReadAt(raw[:], pos); err != nil {
			return nil, fmt.Errorf("%w: 读取文件表失败: %v", ErrNotNPK, err)
		}
		offset := int64(int32(binary.LittleEndian.Uint32(raw[0:])))
		size := int64(int32(binary.LittleEndian.Uint32(raw[4:])))
		if offset < 0 || size < 0 || offset > info.Size() || size > info.Size()-offset {
			return nil, fmt.Errorf("%w: 第 %d 个文件的偏移越界", ErrNotNPK, index)
		}
		var encrypted [256]byte
		if _, err := file.ReadAt(encrypted[:], pos+8); err != nil {
			return nil, fmt.Errorf("%w: 读取文件名失败: %v", ErrNotNPK, err)
		}
		entries = append(entries, Entry{Name: decodeNPKName(encrypted[:]), Offset: offset, Size: size})
		pos += 264
	}
	return &NPK{Path: path, Size: info.Size(), Entries: entries}, nil
}

func decodeNPKName(encrypted []byte) string {
	decoded := make([]byte, len(encrypted))
	for index, value := range encrypted {
		decoded[index] = value ^ npkFilenameKey[index%len(npkFilenameKey)]
	}
	if zero := bytes.IndexByte(decoded, 0); zero >= 0 {
		decoded = decoded[:zero]
	}
	name, err := korean.EUCKR.NewDecoder().Bytes(decoded)
	if err != nil {
		return strings.TrimSpace(string(decoded))
	}
	return strings.TrimSpace(string(name))
}

// ImageMeta describes one IMG frame. DataOffset and Sprite.DataOffset are
// relative to the beginning of the IMG member in its NPK file.
type ImageMeta struct {
	Format       int32 `json:"format"`
	Extra        int32 `json:"extra"`
	Width        int32 `json:"width"`
	Height       int32 `json:"height"`
	HeaderSize   int32 `json:"headerSize"`
	DataSize     int32 `json:"dataSize"`
	X            int32 `json:"x"`
	Y            int32 `json:"y"`
	MemoryWidth  int32 `json:"memoryWidth"`
	MemoryHeight int32 `json:"memoryHeight"`
	DataOffset   int64 `json:"dataOffset"`
	LinkIndex    int32 `json:"linkIndex,omitempty"`
	SpriteIndex  int32 `json:"spriteIndex,omitempty"`
	Left         int32 `json:"left,omitempty"`
	Top          int32 `json:"top,omitempty"`
	Right        int32 `json:"right,omitempty"`
	Bottom       int32 `json:"bottom,omitempty"`
	Rotate       int32 `json:"rotate,omitempty"`
}

type Color [4]uint8

type SpriteMeta struct {
	Keep       int32 `json:"keep"`
	Format     int32 `json:"format"`
	Index      int32 `json:"index"`
	DataSize   int32 `json:"dataSize"`
	RawSize    int32 `json:"rawSize"`
	Width      int32 `json:"width"`
	Height     int32 `json:"height"`
	DataOffset int64 `json:"dataOffset"`
}

// IMGFile is a parsed IMG metadata snapshot. It is safe to JSON-marshal and
// persist as part of the image index.
type IMGFile struct {
	Version    int32        `json:"version"`
	ImagesSize int64        `json:"imagesSize"`
	DataStart  int64        `json:"dataStart"`
	Images     []ImageMeta  `json:"images"`
	Palettes   [][]Color    `json:"palettes,omitempty"`
	Sprites    []SpriteMeta `json:"sprites,omitempty"`
}

type imgCursor struct {
	r    io.ReaderAt
	base int64
	size int64
	pos  int64
}

func (c *imgCursor) readBytes(count int) ([]byte, error) {
	if count < 0 || int64(count) > c.size-c.pos {
		return nil, io.ErrUnexpectedEOF
	}
	data := make([]byte, count)
	if _, err := c.r.ReadAt(data, c.base+c.pos); err != nil {
		return nil, err
	}
	c.pos += int64(count)
	return data, nil
}

func (c *imgCursor) readInt32() (int32, error) {
	data, err := c.readBytes(4)
	if err != nil {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint32(data)), nil
}

func (c *imgCursor) readInt16() (int16, error) {
	data, err := c.readBytes(2)
	if err != nil {
		return 0, err
	}
	return int16(binary.LittleEndian.Uint16(data)), nil
}

func (c *imgCursor) skip(count int64) error {
	if count < 0 || count > c.size-c.pos {
		return io.ErrUnexpectedEOF
	}
	c.pos += count
	return nil
}

// ParseIMGAt parses only an IMG header/table from an NPK member.
func ParseIMGAt(r io.ReaderAt, offset, size int64) (*IMGFile, error) {
	if offset < 0 || size < 0 {
		return nil, ErrNotIMG
	}
	c := &imgCursor{r: r, base: offset, size: size}
	magic, err := readCursorCString(c, 18)
	if err != nil {
		return nil, ErrNotIMG
	}
	newFormat := magic == imgMagic
	oldFormat := magic == imgMagicOld
	if !newFormat && !oldFormat {
		return nil, ErrNotIMG
	}
	var imagesSize int32
	if newFormat {
		imagesSize, err = c.readInt32()
		if err != nil {
			return nil, err
		}
	} else if _, err := c.readInt16(); err != nil {
		return nil, err
	}
	if imagesSize < 0 {
		return nil, fmt.Errorf("%w: images_size 为负数", ErrNotIMG)
	}
	_, err = c.readInt32() // keep
	if err != nil {
		return nil, err
	}
	version, err := c.readInt32()
	if err != nil {
		return nil, err
	}
	count, err := c.readInt32()
	if err != nil {
		return nil, err
	}
	if count < 0 || count > maxIMGImages {
		return nil, fmt.Errorf("%w: 无效图片数量", ErrNotIMG)
	}
	if version != 1 && version != 2 && version != 4 && version != 5 && version != 6 {
		return nil, fmt.Errorf("%w: v%d", ErrUnsupported, version)
	}
	result := &IMGFile{Version: version, ImagesSize: int64(imagesSize), Images: make([]ImageMeta, 0, count)}
	if version == 5 {
		if err := parseV5Prefix(c, result); err != nil {
			return nil, err
		}
	} else if version == 4 {
		palette, err := readPalette(c)
		if err != nil {
			return nil, err
		}
		result.Palettes = [][]Color{palette}
	} else if version == 6 {
		count, err := c.readInt32()
		if err != nil || count < 0 || count > 1_000_000 {
			return nil, fmt.Errorf("%w: 无效调色板数量", ErrNotIMG)
		}
		result.Palettes = make([][]Color, 0, count)
		for index := int32(0); index < count; index++ {
			palette, err := readPalette(c)
			if err != nil {
				return nil, err
			}
			result.Palettes = append(result.Palettes, palette)
		}
	}

	if version == 1 {
		for index := int32(0); index < count; index++ {
			meta, err := readImageHeader(c, version)
			if err != nil {
				return nil, err
			}
			if meta.Extra == ExtraSprite {
				values, err := readInts(c, 7)
				if err != nil {
					return nil, err
				}
				meta.SpriteIndex, meta.Left, meta.Top, meta.Right, meta.Bottom, meta.Rotate = values[1], values[2], values[3], values[4], values[5], values[6]
				meta.DataSize = 0
			}
			meta.DataOffset = c.pos
			if err := validateImageMeta(meta, size); err != nil {
				return nil, fmt.Errorf("图片 %d: %w", index, err)
			}
			result.Images = append(result.Images, meta)
			if meta.Format != FormatLink && meta.Extra != ExtraSprite {
				if err := c.skip(int64(meta.DataSize)); err != nil {
					return nil, fmt.Errorf("图片 %d 数据越界: %w", index, err)
				}
			}
		}
		result.DataStart = 32
		resolveLinks(result.Images)
		return result, nil
	}

	// v2/v4/v5/v6 keep all ordinary image headers together. The data starts
	// after every extra header (and after the sprite table in v5). Real v4
	// files place the palette before the image headers, so using the cursor is
	// important; images_size alone would point into the palette.
	for index := int32(0); index < count; index++ {
		meta, err := readImageHeader(c, version)
		if err != nil {
			return nil, err
		}
		if meta.Extra == ExtraSprite {
			values, err := readInts(c, 7)
			if err != nil {
				return nil, err
			}
			meta.SpriteIndex, meta.Left, meta.Top, meta.Right, meta.Bottom, meta.Rotate = values[1], values[2], values[3], values[4], values[5], values[6]
			meta.DataSize = 0
		}
		if err := validateImageMeta(meta, size); err != nil {
			return nil, fmt.Errorf("图片 %d: %w", index, err)
		}
		result.Images = append(result.Images, meta)
	}
	dataStart := c.pos
	if version == 2 && imagesSize > 0 {
		// For ordinary v2 files this is equivalent to the cursor position and
		// catches malformed headers whose declared table is shorter.
		declared := int64(32) + int64(imagesSize)
		if declared >= dataStart && declared <= size {
			dataStart = declared
		}
	}
	result.DataStart = dataStart
	cursor := dataStart
	if version == 5 {
		for index := range result.Sprites {
			result.Sprites[index].DataOffset = cursor
			cursor += int64(result.Sprites[index].DataSize)
			if cursor > size {
				return nil, fmt.Errorf("sprite %d 数据越界", index)
			}
		}
	}
	for index := range result.Images {
		meta := &result.Images[index]
		if meta.Format == FormatLink || meta.Extra == ExtraSprite {
			continue
		}
		meta.DataOffset = cursor
		cursor += int64(meta.DataSize)
		if cursor > size {
			return nil, fmt.Errorf("图片 %d 数据越界", index)
		}
	}
	resolveLinks(result.Images)
	return result, nil
}

func parseV5Prefix(c *imgCursor, result *IMGFile) error {
	spriteCount, _ := c.readInt32()
	if _, err := c.readInt32(); err != nil || spriteCount < 0 || spriteCount > maxIMGImages {
		return fmt.Errorf("%w: 无效 sprite 数量", ErrNotIMG)
	}
	palette, err := readPalette(c)
	if err != nil {
		return err
	}
	result.Palettes = [][]Color{palette}
	result.Sprites = make([]SpriteMeta, 0, spriteCount)
	for index := int32(0); index < spriteCount; index++ {
		values, err := readInts(c, 7)
		if err != nil {
			return err
		}
		if values[3] < 0 || values[4] < 0 || values[5] < 0 || values[6] < 0 {
			return fmt.Errorf("%w: sprite %d 参数为负数", ErrNotIMG, index)
		}
		result.Sprites = append(result.Sprites, SpriteMeta{
			Keep: values[0], Format: values[1], Index: values[2], DataSize: values[3], RawSize: values[4], Width: values[5], Height: values[6],
		})
	}
	return nil
}

func readPalette(c *imgCursor) ([]Color, error) {
	count, err := c.readInt32()
	if err != nil || count < 0 || count > 1_000_000 {
		return nil, fmt.Errorf("%w: 无效颜色数量", ErrNotIMG)
	}
	data, err := c.readBytes(int(count) * 4)
	if err != nil {
		return nil, err
	}
	palette := make([]Color, count)
	for index := range palette {
		copy(palette[index][:], data[index*4:index*4+4])
	}
	return palette, nil
}

func readImageHeader(c *imgCursor, version int32) (ImageMeta, error) {
	format, err := c.readInt32()
	if err != nil {
		return ImageMeta{}, err
	}
	if format == FormatLink {
		link, err := c.readInt32()
		return ImageMeta{Format: format, Extra: ExtraNone, LinkIndex: link, Width: 0, Height: 0}, err
	}
	extra, err := c.readInt32()
	if err != nil {
		return ImageMeta{}, err
	}
	values, err := readInts(c, 7)
	if err != nil {
		return ImageMeta{}, err
	}
	if extra != ExtraNone && extra != ExtraZlib && extra != ExtraSprite {
		return ImageMeta{}, fmt.Errorf("%w: extra=%d", ErrUnsupported, extra)
	}
	meta := ImageMeta{Format: format, Extra: extra, Width: values[0], Height: values[1], HeaderSize: values[2], DataSize: values[2], X: values[3], Y: values[4], MemoryWidth: values[5], MemoryHeight: values[6]}
	if version == 1 || version == 2 {
		if extra == ExtraNone {
			meta.DataSize = meta.Width * meta.Height * pixelSize(format)
		}
	}
	if extra == ExtraSprite {
		// The image's seven sprite fields follow in the caller because the
		// regular image header does not contain them.
		meta.DataSize = 0
	}
	return meta, nil
}

func readInts(c *imgCursor, count int) ([]int32, error) {
	values := make([]int32, count)
	for index := range values {
		value, err := c.readInt32()
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

func pixelSize(format int32) int32 {
	switch format {
	case Format1555, Format4444, FormatDXT1, FormatDXT3:
		return 2
	case Format8888, FormatDXT5:
		return 4
	default:
		return 0
	}
}

func validateImageMeta(meta ImageMeta, imgSize int64) error {
	if meta.Format != Format1555 && meta.Format != Format4444 && meta.Format != Format8888 && meta.Format != FormatLink && meta.Format != FormatDXT1 && meta.Format != FormatDXT3 && meta.Format != FormatDXT5 {
		return fmt.Errorf("%w: format=%d", ErrUnsupported, meta.Format)
	}
	if meta.Format == FormatLink {
		if meta.LinkIndex < 0 {
			return fmt.Errorf("%w: link 索引为负数", ErrBadImageData)
		}
		return nil
	}
	if meta.Width <= 0 || meta.Height <= 0 || meta.Width > maxDimension || meta.Height > maxDimension || int64(meta.Width)*int64(meta.Height) > maxPixels {
		return fmt.Errorf("%w: 尺寸 %dx%d", ErrBadImageData, meta.Width, meta.Height)
	}
	if meta.DataSize < 0 || int64(meta.DataSize) > imgSize {
		return fmt.Errorf("%w: 数据长度 %d", ErrBadImageData, meta.DataSize)
	}
	return nil
}

func resolveLinks(images []ImageMeta) {
	for index := range images {
		if images[index].Format == FormatLink && images[index].LinkIndex >= 0 && images[index].LinkIndex < int32(len(images)) {
			// Dimensions are useful to callers during indexing. Pixel decoding
			// still resolves links recursively and detects cycles.
			seen := map[int32]struct{}{int32(index): {}}
			current := images[index].LinkIndex
			for current >= 0 && current < int32(len(images)) && images[current].Format == FormatLink {
				if _, ok := seen[current]; ok {
					break
				}
				seen[current] = struct{}{}
				current = images[current].LinkIndex
			}
			if current >= 0 && current < int32(len(images)) && images[current].Format != FormatLink {
				images[index].Width = images[current].Width
				images[index].Height = images[current].Height
			}
		}
	}
}

func readCursorCString(c *imgCursor, max int) (string, error) {
	data := make([]byte, 0, max)
	for index := 0; index < max; index++ {
		value, err := c.readBytes(1)
		if err != nil {
			return "", err
		}
		if value[0] == 0 {
			return string(data), nil
		}
		data = append(data, value[0])
	}
	return "", io.ErrUnexpectedEOF
}

func readCStringAt(r io.ReaderAt, offset int64, max int) (string, int64, error) {
	data := make([]byte, max)
	n, err := r.ReadAt(data, offset)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", 0, err
	}
	data = data[:n]
	if zero := bytes.IndexByte(data, 0); zero >= 0 {
		return string(data[:zero]), offset + int64(zero) + 1, nil
	}
	return "", 0, io.ErrUnexpectedEOF
}

func readAt(r io.ReaderAt, offset int64, value *int32) error {
	var data [4]byte
	if _, err := r.ReadAt(data[:], offset); err != nil {
		return err
	}
	*value = int32(binary.LittleEndian.Uint32(data[:]))
	return nil
}

// DecodeImage reads the requested image data from an NPK member and returns a
// Go image. It is deliberately separate from ParseIMGAt so scans never load
// every pixel into memory.
func DecodeImage(r io.ReaderAt, imgOffset, imgSize int64, file *IMGFile, index int32) (image.Image, error) {
	if file == nil || index < 0 || index >= int32(len(file.Images)) {
		return nil, fmt.Errorf("图片索引越界: %d", index)
	}
	return decodeImage(r, imgOffset, imgSize, file, index, map[int32]struct{}{})
}

func decodeImage(r io.ReaderAt, imgOffset, imgSize int64, file *IMGFile, index int32, visiting map[int32]struct{}) (image.Image, error) {
	if _, ok := visiting[index]; ok {
		return nil, fmt.Errorf("%w: 图片 link 环", ErrBadImageData)
	}
	visiting[index] = struct{}{}
	defer delete(visiting, index)
	meta := file.Images[index]
	if meta.Format == FormatLink {
		if meta.LinkIndex < 0 || meta.LinkIndex >= int32(len(file.Images)) {
			return nil, fmt.Errorf("%w: link 索引 %d", ErrBadImageData, meta.LinkIndex)
		}
		return decodeImage(r, imgOffset, imgSize, file, meta.LinkIndex, visiting)
	}
	if meta.Extra == ExtraSprite {
		if meta.SpriteIndex < 0 || meta.SpriteIndex >= int32(len(file.Sprites)) {
			return nil, fmt.Errorf("%w: sprite 索引 %d", ErrBadImageData, meta.SpriteIndex)
		}
		sprite := file.Sprites[meta.SpriteIndex]
		compressed, err := readRange(r, imgOffset+sprite.DataOffset, int64(sprite.DataSize), imgOffset, imgSize)
		if err != nil {
			return nil, err
		}
		raw, err := decompressZlib(compressed)
		if err != nil {
			return nil, err
		}
		decoded, err := decodePixels(raw, sprite.Format, sprite.Width, sprite.Height, nil)
		if err != nil {
			return nil, err
		}
		return cropRotate(decoded, meta.Left, meta.Top, meta.Right, meta.Bottom, meta.Rotate)
	}
	data, err := readRange(r, imgOffset+meta.DataOffset, int64(meta.DataSize), imgOffset, imgSize)
	if err != nil {
		return nil, err
	}
	if meta.Extra == ExtraZlib {
		data, err = decompressZlib(data)
		if err != nil {
			return nil, err
		}
	}
	var palette []Color
	if len(file.Palettes) > 0 && (meta.Extra == ExtraZlib || file.Version == 6) {
		palette = file.Palettes[0]
	}
	return decodePixels(data, meta.Format, meta.Width, meta.Height, palette)
}

func readRange(r io.ReaderAt, offset, size, base, memberSize int64) ([]byte, error) {
	if size < 0 || offset < base || offset-base > memberSize || size > memberSize-(offset-base) {
		return nil, fmt.Errorf("%w: 数据偏移越界", ErrBadImageData)
	}
	data := make([]byte, size)
	if _, err := r.ReadAt(data, offset); err != nil {
		return nil, err
	}
	return data, nil
}

func decompressZlib(data []byte) ([]byte, error) {
	try := func(input []byte) ([]byte, error) {
		reader, err := zlib.NewReader(bytes.NewReader(input))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(io.LimitReader(reader, maxZlibReader))
	}
	if len(data) == 0 {
		return nil, ErrBadImageData
	}
	if result, err := try(data); err == nil {
		return result, nil
	}
	last := bytes.LastIndexByte(data, 0x78)
	if last >= 0 {
		if result, err := try(data[last:]); err == nil {
			return result, nil
		}
		if last+2 <= len(data) {
			if result, err := try(append(append([]byte(nil), data[last:last+2]...), data...)); err == nil {
				return result, nil
			}
		}
	}
	return nil, fmt.Errorf("%w: zlib 解压失败", ErrBadImageData)
}

const maxZlibReader = 256 << 20

func decodePixels(data []byte, format int32, width, height int32, palette []Color) (image.Image, error) {
	if width <= 0 || height <= 0 || width > maxDimension || height > maxDimension || int64(width)*int64(height) > maxPixels {
		return nil, fmt.Errorf("%w: 尺寸 %dx%d", ErrBadImageData, width, height)
	}
	if len(palette) > 0 {
		if int64(width)*int64(height) > int64(len(data)) {
			return nil, fmt.Errorf("%w: 调色板索引数据过短", ErrBadImageData)
		}
		out := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))
		for y := 0; y < int(height); y++ {
			for x := 0; x < int(width); x++ {
				index := data[y*int(width)+x]
				if int(index) >= len(palette) {
					return nil, fmt.Errorf("%w: 调色板索引 %d", ErrBadImageData, index)
				}
				out.SetNRGBA(x, y, colorNRGBA(palette[index]))
			}
		}
		return out, nil
	}
	if format == FormatDXT1 || format == FormatDXT3 || format == FormatDXT5 {
		return decodeDXT(data, format, int(width), int(height))
	}
	pixelBytes := pixelSize(format)
	if pixelBytes == 0 || int64(width)*int64(height)*int64(pixelBytes) > int64(len(data)) {
		return nil, fmt.Errorf("%w: 原始像素数据过短", ErrBadImageData)
	}
	out := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))
	for y := 0; y < int(height); y++ {
		for x := 0; x < int(width); x++ {
			base := (y*int(width) + x) * int(pixelBytes)
			var pixel Color
			switch format {
			case Format1555:
				pixel = decode1555(data[base], data[base+1])
			case Format4444:
				pixel = decode4444(data[base], data[base+1])
			case Format8888:
				pixel = Color{data[base+2], data[base+1], data[base], data[base+3]}
			default:
				return nil, fmt.Errorf("%w: format=%d", ErrUnsupported, format)
			}
			out.SetNRGBA(x, y, colorNRGBA(pixel))
		}
	}
	return out, nil
}

func colorNRGBA(value Color) color.NRGBA {
	return color.NRGBA{R: value[0], G: value[1], B: value[2], A: value[3]}
}

func decode1555(first, second byte) Color {
	b := first & 0x1f
	b = (b << 3) | (b >> 2)
	g := (first >> 5) | ((second & 3) << 3)
	g = (g << 3) | (g >> 2)
	r := (second >> 2) & 0x1f
	r = (r << 3) | (r >> 2)
	a := uint8(0)
	if second>>7 != 0 {
		a = 0xff
	}
	return Color{r, g, b, a}
}

func decode4444(first, second byte) Color {
	return Color{(second & 0x0f) << 4, first & 0xf0, (first & 0x0f) << 4, second & 0xf0}
}

func cropRotate(src image.Image, left, top, right, bottom, rotate int32) (image.Image, error) {
	bounds := src.Bounds()
	if left < 0 || top < 0 || right <= left || bottom <= top || right > int32(bounds.Dx()) || bottom > int32(bounds.Dy()) {
		return nil, fmt.Errorf("%w: sprite 裁剪框 [%d,%d,%d,%d]", ErrBadImageData, left, top, right, bottom)
	}
	cropped := image.NewNRGBA(image.Rect(0, 0, int(right-left), int(bottom-top)))
	for y := int32(0); y < bottom-top; y++ {
		for x := int32(0); x < right-left; x++ {
			cropped.Set(int(x), int(y), src.At(int(left+x), int(top+y)))
		}
	}
	if rotate != 1 {
		return cropped, nil
	}
	rotated := image.NewNRGBA(image.Rect(0, 0, cropped.Bounds().Dy(), cropped.Bounds().Dx()))
	for y := 0; y < cropped.Bounds().Dy(); y++ {
		for x := 0; x < cropped.Bounds().Dx(); x++ {
			rotated.Set(y, cropped.Bounds().Dx()-1-x, cropped.At(x, y))
		}
	}
	return rotated, nil
}

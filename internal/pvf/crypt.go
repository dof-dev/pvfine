package pvf

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// Section keys and LCG magic constants.
//
// crypt (magic 0x269EC3): "HeaD", "HASH", "GRPI", "BodY".
// crypt2 (magic 0x269EC9): "sTrA", "sTrW" name-pool sections.
const (
	magicMain uint32 = 0x269EC3
	magicAlt  uint32 = 0x269EC9
)

const (
	keyHead = "HeaD"
	keyHash = "HASH"
	keyGrpi = "GRPI"
	keyBody = "BodY"
	keyStrA = "sTrA"
	keyStrW = "sTrW"
)

// XOR name-pool section size obfuscation constants.
const (
	xorStrA uint32 = 0xAA74472E
	xorStrW uint32 = 0x9A82F037
)

// crypt XORs b in place with the LCG keystream derived from key.
// All arithmetic is mod 2^32 (C# unchecked int semantics); because it is a
// pure XOR stream, the same call both encrypts and decrypts.
func crypt(key string, magic uint32, b []byte) {
	k := []byte(key)
	if len(k) < 4 || len(b) == 0 {
		return
	}
	seed := uint32(0x76826701)*uint32(k[0]) +
		0x1C1*(uint32(k[3])+0x1C1*(uint32(k[2])+0x1C1*uint32(k[1])))

	n := len(b)
	nq := n >> 2
	for i := 0; i < nq; i++ {
		t1 := 0x343FD*seed + magic
		seed = 0x343FD*t1 + magic
		// C#: (uint)(((seed >> 16) & 0xFFFF) + (t1 & 0xFFFF0000))
		xk := (t1 & 0xFFFF0000) | (seed >> 16)
		off := i << 2
		v := binary.LittleEndian.Uint32(b[off:]) ^ xk
		binary.LittleEndian.PutUint32(b[off:], v)
	}
	if tail := n - nq<<2; tail > 0 {
		t1 := 0x343FD*seed + magic
		t2 := 0x343FD*t1 + magic
		// C#: (uint)((t1 & 0xFFFF0000) + ((t2 >> 16) & 0xFFFF))
		fk := (t1 & 0xFFFF0000) | (t2 >> 16)
		var kb [4]byte
		binary.LittleEndian.PutUint32(kb[:], fk)
		start := nq << 2
		for i := 0; i < tail; i++ {
			b[start+i] ^= kb[i]
		}
	}
}

// applyGuard XORs header bytes [24:28] with 0x55 (the "guard" variant).
func applyGuard(b []byte) {
	for i := 24; i < 28; i++ {
		b[i] ^= 0x55
	}
}

func zlibCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func zlibDecompress(data []byte) ([]byte, error) {
	if len(data) < 6 || data[0] != 0x78 {
		return nil, fmt.Errorf("pvf: bad zlib header % X", data[:min(2, len(data))])
	}
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	return out, nil
}

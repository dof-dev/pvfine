// Package pvf parses and packs DNF script archives ("Script.pvf", S4A21 variant).
//
// Layout:
//
//	Header   0x30 bytes  — encrypted with key "HeaD" (+ optional guard XOR on [24:28])
//	FileTable FileCount*24 — plaintext entries: name/path pool offsets, chunk, offset, size, type
//	HashTable  HashTableSize — encrypted with key "HASH" (path lookup aid)
//	NameTable  NameTableSize — 8B prefix + "sTrA"/"sTrW" encrypted zlib string pools
//	GRPI       GroupCount*8  — encrypted chunk index (cumulative compressed sizes)
//	Body       BodySize      — zlib chunks, each encrypted whole with key "BodY"
//
// All encryption is a deterministic LCG keystream XOR (see crypt.go), so
// encryption and decryption are the same operation.
//
// Basic use:
//
//	a, err := pvf.Open("Script.pvf")
//	i, _ := a.Find("equipment/character/common/amulet/100300001.equ")
//	text, _ := a.Text(i)          // decompiled script / localized text
//	a.SetText(i, newText)         // queue an edit
//	a.AddFile("etc/hello.txt", data, pvf.TypeScript)
//	err = a.SaveAs("Script_new.pvf")
//
// Unmodified archives are saved byte-identical; edits rebuild only the touched
// chunks plus the table/hash/name/grpi sections.
package pvf

import "errors"

// Content data types (File.DataType).
const (
	TypeScript   int32 = 1 // 5-byte token stream: u8 kind + i32 value
	TypeUnicode  int32 = 3 // UTF-16LE text (localized .str etc.)
)

// Header signature after decryption: bytes "nkpi" in file order.
const MagicSignature uint32 = 0x69706B6E

const headerSize = 0x30

var (
	ErrTruncated    = errors.New("pvf: data too short to contain a header")
	ErrBadSignature = errors.New("pvf: header decryption failed (unknown format or key)")
	ErrOverflow     = errors.New("pvf: header sections exceed the file boundary")
	ErrNotFound     = errors.New("pvf: file not found")
	ErrBadIndex     = errors.New("pvf: file index out of range")
)

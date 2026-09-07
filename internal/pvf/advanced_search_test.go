package pvf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

func putSearchToken(raw []byte, pos int, typ byte, value int32) {
	raw[pos] = typ
	binary.LittleEndian.PutUint32(raw[pos+1:], uint32(value))
}

func TestEncodeScriptQueryDoesNotMutateStringPool(t *testing.T) {
	a := New()
	index, err := a.AddFileText("dir/item.equ", "[name]\n`alpha`\n[grade]\n1", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	poolSize := len(a.strA)
	modified := a.ModifiedCount()

	pattern, err := a.EncodeScriptQuery("[name]\n`alpha`")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, pattern) {
		t.Fatalf("query bytes do not match payload: % X vs % X", pattern, raw[:len(pattern)])
	}
	if _, err := a.EncodeScriptQuery("`missing-from-pool`"); !errors.Is(err, ErrQueryStringNotInPool) {
		t.Fatalf("unknown query error = %v", err)
	}
	if len(a.strA) != poolSize || a.ModifiedCount() != modified {
		t.Fatalf("query compilation mutated archive: pool=%d/%d modified=%d/%d", len(a.strA), poolSize, a.ModifiedCount(), modified)
	}
}

func TestStringPoolIndexFindsAllReferenceKinds(t *testing.T) {
	a := New()
	shared := a.StringOffset("shared-value")
	duplicateStart := len(a.strA)
	a.strA = append(a.strA, []byte("shared-value\x00")...)
	a.strAIdx = nil
	a.strWIdx = nil
	duplicate := int32(duplicateStart << 1)
	unicode := a.UnicodeStringOffset("韩国值")

	firstRaw := make([]byte, 20)
	putSearchToken(firstRaw, 0, 3, shared)
	putSearchToken(firstRaw, 5, 5, duplicate)
	putSearchToken(firstRaw, 10, 6, unicode)
	putSearchToken(firstRaw, 15, 7, shared)
	first := a.AddFile("dir/first.equ", firstRaw, TypeScript)
	secondRaw := make([]byte, 5)
	putSearchToken(secondRaw, 0, 6, duplicate)
	second := a.AddFile("other/second.equ", secondRaw, TypeScript)

	index, err := a.BuildStringPoolIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	files, err := index.MatchFiles("shared-value", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != first || files[1] != second {
		t.Fatalf("shared refs = %#v, want [%d %d]", files, first, second)
	}

	files, err = index.MatchFiles("^韩国值$", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != first {
		t.Fatalf("unicode refs = %#v, want [%d]", files, first)
	}

	files, err = index.MatchFiles("first", false)
	if err != nil || len(files) != 1 || files[0] != first {
		t.Fatalf("file-table refs = %#v, err=%v", files, err)
	}

	if len(a.chunkCache) != 0 {
		t.Fatalf("streaming index populated chunk cache: %d chunks", len(a.chunkCache))
	}
}

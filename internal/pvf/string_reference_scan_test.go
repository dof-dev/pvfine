package pvf

import (
	"context"
	"reflect"
	"testing"
)

func TestStringReferenceScannerPreservesPoolIdentity(t *testing.T) {
	a := New()
	first := a.StringOffset("same")
	duplicate := int32(len(a.strA) << 1)
	a.strA = append(a.strA, []byte("same\x00")...)
	wide := a.UnicodeStringOffset("same")
	raw := make([]byte, 25)
	for i, token := range []struct {
		typ    byte
		offset int32
	}{{3, first}, {7, first}, {5, duplicate}, {6, wide}, {6, first + 2}} {
		putSearchToken(raw, i*5, token.typ, token.offset)
	}
	a.AddFile("dir/file.equ", raw, TypeScript)
	oracle, err := a.BuildStringPoolIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want, err := oracle.MatchDetails("same", false)
	if err != nil {
		t.Fatal(err)
	}
	pool := a.NewStringPoolScanner()
	entries := map[int32]StringPoolEntry{}
	for {
		entry, ok, err := pool.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		entries[entry.Offset] = entry
	}
	scan := a.NewStringReferenceScanner()
	var got []StringPoolMatch
	for {
		index, refs, ok, err := scan.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		for _, ref := range refs {
			entry, valid := entries[ref.Offset]
			if !valid || entry.Value != "same" {
				continue
			}
			got = append(got, StringPoolMatch{FileIndex: index, Pool: entry.Pool, Offset: entry.Offset, Value: entry.Value, Occurrences: ref.Occurrences, TokenTypes: ref.TokenTypes, FileFields: ref.FileFields})
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	if len(a.chunkCache) != 0 {
		t.Fatal("scanner polluted interactive cache")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := pool.Next(ctx); err != context.Canceled {
		t.Fatalf("pool cancellation: %v", err)
	}
	if _, _, _, err := scan.Next(ctx); err != context.Canceled {
		t.Fatalf("reference cancellation: %v", err)
	}
}

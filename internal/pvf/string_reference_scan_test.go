package pvf

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

// Files in a PVF table need not be ordered by their compressed body chunk.
func interleavedReferenceArchive(t testing.TB) *Archive {
	t.Helper()
	a := New()
	const filesPerChunk = 512
	raw := make([]byte, filesPerChunk*250)
	for pos := 0; pos < len(raw); pos += 5 {
		putSearchToken(raw, pos, 7, a.StringOffset(fmt.Sprintf("value-%d", pos%250)))
	}
	for chunk := 0; chunk < 2; chunk++ {
		compressed, err := zlibCompress(raw)
		if err != nil {
			t.Fatal(err)
		}
		cryptSeed(a.keys.body.seed, a.keys.body.magic, compressed)
		a.data = append(a.data, compressed...)
		a.groups = append(a.groups, groupItem{compSize: int32(len(a.data)), origSize: int32(len(raw))})
	}
	for i := 0; i < filesPerChunk*2; i++ {
		index := a.AddFile(fmt.Sprintf("dir/%d.equ", i), nil, TypeScript)
		a.items[index].chunk = int32(i % 2)
		a.items[index].off = int32(i/2) * 250
		a.items[index].size = 250
		delete(a.overlay, index)
	}
	return a
}

func BenchmarkStringReferenceScannerInterleaved(b *testing.B) {
	a := interleavedReferenceArchive(b)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		scanner := a.NewStringReferenceScanner()
		for {
			_, _, ok, err := scanner.Next(context.Background())
			if err != nil {
				b.Fatal(err)
			}
			if !ok {
				break
			}
		}
	}
}

func TestStringReferenceScannerInterleavedChunks(t *testing.T) {
	a := interleavedReferenceArchive(t)
	// Include edited, detached and non-script files in the traversal.
	raw := make([]byte, 5)
	putSearchToken(raw, 0, 3, a.StringOffset("overlay"))
	a.overlay[2] = raw
	a.AddFile("extra/added.equ", raw, TypeScript)
	a.AddFile("extra/plain.str", raw, TypeUnicode)
	want := make(map[int32][]byte)
	if err := a.ForEachRawFile(context.Background(), func(index int32, _ string, _ File, raw []byte) bool {
		want[index] = append([]byte(nil), raw...)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	scan := a.NewStringReferenceScanner()
	seen := make(map[int32]bool)
	chunks := make(map[int32]bool)
	previous := int32(-1)
	for {
		index, refs, ok, err := scan.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if seen[index] {
			t.Fatalf("duplicate file %d", index)
		}
		seen[index] = true
		if scan.chunkIndex != previous {
			if chunks[scan.chunkIndex] {
				t.Fatal("scanner decompressed a previously visited chunk again")
			}
			chunks[scan.chunkIndex] = true
			previous = scan.chunkIndex
		}
		// Compare with a one-file overlay scan to verify original identities,
		// payloads, file-field references, token types and occurrence counts.
		copyArchive := &Archive{
			items:   []fileItem{a.items[index]},
			overlay: map[int32][]byte{0: want[index]},
		}
		_, expected, _, err := copyArchive.NewStringReferenceScanner().Next(context.Background())
		if err != nil || !reflect.DeepEqual(refs, expected) {
			t.Fatalf("references differ for file %d: %v", index, err)
		}
	}
	if len(seen) != len(a.items) || len(chunks) != 2 || len(a.chunkCache) != 0 {
		t.Fatalf("files=%d chunks=%d cached=%d", len(seen), len(chunks), len(a.chunkCache))
	}
}

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

func TestStringReferenceInputSurvivesScannerAndEdits(t *testing.T) {
	for _, overlay := range []bool{false, true} {
		t.Run(fmt.Sprint(overlay), func(t *testing.T) {
			a := interleavedReferenceArchive(t)
			if overlay {
				raw := make([]byte, 5)
				putSearchToken(raw, 0, 3, a.StringOffset("edited"))
				a.overlay[0] = raw
			}
			scanner := a.NewStringReferenceScanner()
			index, input, ok, err := scanner.NextInput(context.Background())
			if err != nil || !ok || index != 0 {
				t.Fatalf("first input: index=%d ok=%t err=%v", index, ok, err)
			}
			want, err := input.References(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for ok {
				_, _, ok, err = scanner.NextInput(context.Background())
				if err != nil {
					t.Fatal(err)
				}
			}
			// Parsing snapshots must not read archive metadata, overlays or the
			// scanner's latest chunk, even while the archive is being edited.
			var wg sync.WaitGroup
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 100; j++ {
						got, err := input.References(context.Background())
						if err != nil || !reflect.DeepEqual(got, want) {
							t.Error("snapshot changed after scanner advance or archive edit")
							return
						}
					}
				}()
			}
			for i := 0; i < 100; i++ {
				a.items[0].nameOff = int32(i)
				clear(a.overlay[0])
			}
			wg.Wait()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := input.References(ctx); err != context.Canceled {
				t.Fatalf("snapshot cancellation: %v", err)
			}
		})
	}
}

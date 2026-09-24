package services

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"pvfine/internal/pvf"
)

func referencePipelineFixture(t testing.TB, files, tokens int) (*core, *advancedSQLite, []byte) {
	t.Helper()
	a := pvf.New()
	raw := make([]byte, tokens*5)
	for i := 0; i < tokens; i++ {
		raw[i*5] = []byte{3, 5, 6, 7}[i%4]
		binary.LittleEndian.PutUint32(raw[i*5+1:], uint32(a.StringOffset(fmt.Sprintf("value-%d", i%67))))
	}
	for i := 0; i < files; i++ {
		a.AddFile(fmt.Sprintf("dir/%d.equ", i), raw, pvf.TypeScript)
	}
	var buf bytes.Buffer
	if err := a.SaveTo(&buf); err != nil {
		t.Fatal(err)
	}
	a, err := pvf.Parse(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &advancedSQLite{ctx: ctx, cancel: cancel, closed: make(chan struct{})}
	t.Cleanup(d.close)
	return &core{archive: a, advancedDisk: d}, d, buf.Bytes()
}

func TestAdvancedReferencePipelineMatchesSequential(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			c, d, _ := referencePipelineFixture(t, 129, 1000)
			a := c.archive
			if err := a.SetRawBytes(0, []byte{7, 0, 0, 0, 0}); err != nil {
				t.Fatal(err)
			}
			a.AddFile("extra/new.equ", nil, pvf.TypeScript)
			a.AddFile("extra/text.str", []byte{7, 0, 0, 0, 0}, pvf.TypeUnicode)
			want := make(map[int32]advancedReferenceResult)
			scanner := a.NewStringReferenceScanner()
			for {
				index, refs, ok, err := scanner.Next(d.ctx)
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					break
				}
				want[index] = advancedReferenceResult{index: index, path: a.Path(index), file: a.File(index), refs: refs}
			}
			p := d.startReferencePipeline(c, a, a.NewStringReferenceScanner(), workers, nil)
			defer p.close()
			for got := range p.results {
				if !reflect.DeepEqual(got, want[got.index]) {
					t.Fatalf("different or duplicate result for file %d", got.index)
				}
				delete(want, got.index)
			}
			if err := p.err(); err != nil || len(want) != 0 {
				t.Fatalf("missing=%d err=%v", len(want), err)
			}
		})
	}
}

func TestAdvancedReferencePipelineStopsWithBlockedWriter(t *testing.T) {
	for _, action := range []string{"writer-error", "cancel", "archive-replaced"} {
		t.Run(action, func(t *testing.T) {
			c, d, _ := referencePipelineFixture(t, 129, 1000)
			a := c.archive
			p := d.startReferencePipeline(c, a, a.NewStringReferenceScanner(), 4, nil)
			defer p.close()
			select {
			case <-p.results:
			case <-time.After(5 * time.Second):
				t.Fatal("reader did not start")
			}
			switch action {
			case "writer-error":
				go p.close()
			case "cancel":
				d.cancel()
			case "archive-replaced":
				c.mu.Lock()
				c.archive = nil
				c.mu.Unlock()
				// Let the reader reach its next identity check.
				go func() {
					for range p.results {
					}
				}()
			}
			select {
			case <-p.done:
			case <-time.After(5 * time.Second):
				t.Fatal("pipeline goroutines did not exit")
			}
			if !errors.Is(p.err(), context.Canceled) {
				t.Fatalf("unexpected cancellation: %v", p.err())
			}
		})
	}
}

func TestAdvancedReferencePipelineReadError(t *testing.T) {
	c, d, source := referencePipelineFixture(t, 8, 100)
	// Deliberately damage retained source bytes after parsing to exercise a
	// decompression failure; ordinary callers must treat these bytes as read-only.
	clear(source)
	a := c.archive
	p := d.startReferencePipeline(c, a, a.NewStringReferenceScanner(), 4, nil)
	defer p.close()
	for range p.results {
		t.Fatal("corrupt chunk produced a result")
	}
	if err := p.err(); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("reader error was lost: %v", err)
	}
}

func TestAdvancedReferencePipelineWriteError(t *testing.T) {
	c, d, _ := referencePipelineFixture(t, 32, 100)
	a := c.archive
	want := errors.New("injected shard write failure")
	p := d.startReferencePipeline(c, a, a.NewStringReferenceScanner(), 4, func(_ context.Context, _ int, _ advancedReferenceResult) error {
		return want
	})
	defer p.close()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("shard failure left workers blocked")
	}
	if !errors.Is(p.err(), want) {
		t.Fatalf("lost shard failure: %v", p.err())
	}
}

func BenchmarkAdvancedReferencePipeline(b *testing.B) {
	for _, workers := range []int{1, 4} {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			c, d, _ := referencePipelineFixture(b, 256, 16000)
			a := c.archive
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				p := d.startReferencePipeline(c, a, a.NewStringReferenceScanner(), workers, nil)
				for range p.results {
				}
				err := p.err()
				p.close()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

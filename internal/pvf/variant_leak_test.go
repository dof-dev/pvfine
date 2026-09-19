package pvf

import (
	"bytes"
	"runtime"
	"testing"
	"time"
)

// TestRecoverZlibSeedNoGoroutineLeak checks that the parallel seed search leaves
// no goroutine blocked on its job channel. Workers stop as soon as a seed wins,
// so the producer must be released rather than left waiting on a send.
func TestRecoverZlibSeedNoGoroutineLeak(t *testing.T) {
	payload := bytes.Repeat([]byte("leak check; "), 2048)
	comp, err := zlibCompress(payload)
	if err != nil {
		t.Fatal(err)
	}
	const seed, magic = 0x12345678, magicMain
	enc := append([]byte(nil), comp...)
	cryptSeed(seed, magic, enc)

	before := runtime.NumGoroutine()
	for i := 0; i < 5; i++ {
		if _, ok := recoverZlibSeed(enc, len(payload)); !ok {
			t.Fatal("seed not recovered")
		}
	}
	// Give any leaked producer a moment to become observable.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(25 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Errorf("goroutine leak: %d -> %d", before, after)
	}
}

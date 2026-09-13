package services

import (
	"os"
	"runtime"
	"testing"
)

// assertPrivateFileMode verifies a credentials-style file was written with
// 0o600. Windows has no POSIX permission bits: os.CreateTemp's Chmod(0o600)
// reports success but the file keeps its 0666 mode, so the mode check only
// applies on platforms that implement those bits.
func assertPrivateFileMode(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode = %v, want 0600", perm)
	}
}

// assertIndexTimingsRecorded checks that index timings were measured. The
// Windows monotonic clock ticks at ~0.5ms, so index work that finishes inside
// a single tick legitimately records 0 and "> 0" would assert clock resolution
// rather than behaviour. Negative values remain a real bug everywhere.
func assertIndexTimingsRecorded(t *testing.T, status IndexStatus) {
	t.Helper()
	if status.OpenDurationMs < 0 || status.BuildDurationMs < 0 {
		t.Fatalf("timing = %#v, want non-negative durations", status)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if status.OpenDurationMs == 0 || status.BuildDurationMs == 0 {
		t.Fatalf("timing = %#v, want positive open and index durations", status)
	}
}

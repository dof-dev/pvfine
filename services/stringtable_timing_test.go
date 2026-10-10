package services

import (
	"strings"
	"testing"

	"pvfine/internal/buildmode"
	"pvfine/internal/pvf"
)

func TestStringTableIndexStatisticsAndLogs(t *testing.T) {
	before := applicationLogs.snapshot()
	lastID := uint64(0)
	if len(before) > 0 {
		lastID = before[len(before)-1].ID
	}
	c := newCore()
	t.Cleanup(c.closeArchive)
	a := pvf.New()
	if _, err := a.AddFileText("list/n_string.lst", "3 `String/Equipment.uv.str`", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	// A UTF-16 ASCII localization entry.
	raw := make([]byte, 0)
	for _, char := range "name>Item\r\n" {
		raw = append(raw, byte(char), 0)
	}
	a.AddFile("String/Equipment.uv.str", raw, pvf.TypeScript)
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	if value, ok := a.LookupStringTable(3, "name"); !ok || value != "Item" {
		t.Fatalf("lookup = %q, %t", value, ok)
	}
	status := NewArchiveService(c).IndexStatus()
	if status.StringTableIndex == nil || status.StringTableIndex.TableBuilds != 1 ||
		status.StringTableIndex.Entries != 1 || status.StringTableIndex.State != "ready" {
		t.Fatalf("index status = %#v", status.StringTableIndex)
	}
	found := false
	for _, entry := range applicationLogs.snapshot() {
		if entry.ID > lastID && entry.Level == "INFO" && entry.Source == "archive:string-table-index" &&
			strings.Contains(entry.Message, "String/Equipment.uv.str") &&
			strings.Contains(entry.Message, "本次耗时=") && strings.Contains(entry.Message, "累计耗时=") {
			found = true
		}
	}
	if found != buildmode.Development {
		t.Fatalf("string table timing log=%t development=%t", found, buildmode.Development)
	}
	level, message, _, _ := describeLogEvent("archive:string-table-index", pvf.StringTableIndexEvent{
		Kind: "table", Path: "missing.str", State: "error", DurationMs: 12.5, Error: "missing",
	})
	if level != "WARN" || !strings.Contains(message, "12.50 ms") || !strings.Contains(message, "missing") {
		t.Fatalf("failure log = %s, %s", level, message)
	}
	c.closeArchive()
	if status := NewArchiveService(c).IndexStatus(); status.StringTableIndex != nil {
		t.Fatalf("closed archive kept timing: %#v", status.StringTableIndex)
	}
}

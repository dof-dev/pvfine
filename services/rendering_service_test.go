package services

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func TestRenderingServiceReloadRulesUpdatesActiveArchive(t *testing.T) {
	c := NewCore()
	a := pvf.New()
	index, err := a.AddFileText("skills/test.skl", "[records]\n1 2 3 4 5\n[/records]", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	before, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	configPath := writeRenderingTestConfig(t, 2)
	service := newRenderingService(c, configPath)
	result, err := service.ReloadRules()
	if err != nil {
		t.Fatalf("ReloadRules() error = %v", err)
	}
	if result.RuleCount != 1 {
		t.Fatalf("rule count = %d, want 1", result.RuleCount)
	}
	after, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	if before == after || !strings.Contains(after, "\t1\t2\n\t3\t4") {
		t.Fatalf("active rendering did not change: before=%q after=%q", before, after)
	}
	if c.batchPlan != nil {
		t.Fatal("batch plan was not invalidated")
	}
}

func TestRenderingServiceReloadRulesKeepsOldEngineOnFailure(t *testing.T) {
	c := NewCore()
	a := pvf.New()
	index, err := a.AddFileText("skills/test.skl", "[records]\n1 2 3 4 5\n[/records]", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	configPath := writeRenderingTestConfig(t, 2)
	service := newRenderingService(c, configPath)
	if _, err := service.ReloadRules(); err != nil {
		t.Fatal(err)
	}
	active, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"version":1,"rules":[{"id":"broken","match":{},"target":{"kind":"section"},"format":{"tokensPerLine":2}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReloadRules(); err == nil {
		t.Fatal("ReloadRules() unexpectedly accepted invalid configuration")
	}
	unchanged, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged != active {
		t.Fatalf("failed reload changed active rendering: got %q, want %q", unchanged, active)
	}
}

func TestRenderingEngineSurvivesBatchClone(t *testing.T) {
	c := NewCore()
	a := pvf.New()
	index, err := a.AddFileText("skills/test.skl", "[records]\n1 2 3 4 5\n[/records]", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	service := newRenderingService(c, writeRenderingTestConfig(t, 2))
	if _, err := service.ReloadRules(); err != nil {
		t.Fatal(err)
	}

	stage := a.CloneForBatch()
	got, err := stage.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	want, err := a.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("batch clone rendering = %q, want %q", got, want)
	}
}

func TestRenderingEngineSurvivesPayloadReplacement(t *testing.T) {
	c := NewCore()
	a := pvf.New()
	if _, err := a.AddFileText("skills/test.skl", "[records]\n1 2 3 4 5\n[/records]", pvf.TypeScript); err != nil {
		t.Fatal(err)
	}
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	if _, err := newRenderingService(c, writeRenderingTestConfig(t, 2)).ReloadRules(); err != nil {
		t.Fatal(err)
	}

	next := pvf.New()
	index, err := next.AddFileText("skills/test.skl", "[records]\n1 2 3 4 5\n[/records]", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	err = c.replaceArchivePayloadLocked(next, map[int32]struct{}{index: {}})
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	text, err := next.Text(index)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "\t1\t2\n\t3\t4") {
		t.Fatalf("payload replacement lost renderer: %q", text)
	}
}

func writeRenderingTestConfig(t *testing.T, tokensPerLine int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rendering.json")
	data := []byte(`{"version":1,"rules":[{"id":"skl.records","match":{"extensions":[".skl"]},"target":{"kind":"section","section":"records"},"format":{"tokensPerLine":` +
		strconv.Itoa(tokensPerLine) + `}}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

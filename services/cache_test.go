package services

import (
	"os"
	"path/filepath"
	"testing"
)

// newCacheFixture builds a fake cache root with one file per cache family plus
// a configuration directory that must never be touched.
func newCacheFixture(t *testing.T) (root, configDir string, settings *SettingsService) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "pvfine")
	writeFixtureFile(t, filepath.Join(root, "search-index", "abcdef.json.gz"), 128)
	writeFixtureFile(t, filepath.Join(root, "archive-index", "deadbeef.db"), 256)
	writeFixtureFile(t, filepath.Join(root, "archive-index", "deadbeef.db-wal"), 32)
	writeFixtureFile(t, filepath.Join(root, "advanced-search", "session-1", "session.db"), 64)
	writeFixtureFile(t, filepath.Join(root, "advanced-search", "refs-v1-abc.db"), 512)
	writeFixtureFile(t, filepath.Join(root, "npk-image-index.json"), 16)
	// Webview-owned state: reported by neither usage nor clear.
	writeFixtureFile(t, filepath.Join(root, "WebKit", "WebsiteData", "local.db"), 4096)

	configDir = filepath.Join(t.TempDir(), "config")
	writeFixtureFile(t, filepath.Join(configDir, "settings.json"), 8)
	writeFixtureFile(t, filepath.Join(configDir, "bookmarks.json"), 8)

	settings = newSettingsService(filepath.Join(configDir, "settings.json"))
	appSettings := DefaultAppSettings()
	appSettings.AutosavePath = filepath.Join(root, "autosave.pvf")
	if err := settings.SaveSettings(appSettings); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(root, "autosave.pvf"), 1024)
	writeFixtureFile(t, filepath.Join(root, "autosave.pvf.json"), 4)
	return root, configDir, settings
}

func writeFixtureFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCacheServiceUsageSumsEveryFamily(t *testing.T) {
	root, _, settings := newCacheFixture(t)
	service := newCacheServiceWithRoot(NewCore(), settings, root)

	usage, err := service.Usage()
	if err != nil {
		t.Fatal(err)
	}
	const want = 128 + 256 + 32 + 64 + 512 + 16 + 1024 + 4
	if usage.TotalBytes != want {
		t.Fatalf("total = %d, want %d", usage.TotalBytes, want)
	}
	if usage.Files != 8 {
		t.Fatalf("files = %d, want 8", usage.Files)
	}
	if usage.Path != root {
		t.Fatalf("path = %q, want %q", usage.Path, root)
	}
	if usage.InUseBytes != 0 {
		t.Fatalf("in-use = %d, want 0 without an open archive", usage.InUseBytes)
	}
}

func TestCacheServiceClearRemovesCachesAndKeepsConfig(t *testing.T) {
	root, configDir, settings := newCacheFixture(t)
	service := newCacheServiceWithRoot(NewCore(), settings, root)

	result, err := service.Clear()
	if err != nil {
		t.Fatal(err)
	}
	const want = 128 + 256 + 32 + 64 + 512 + 16 + 1024 + 4
	if result.FreedBytes != want {
		t.Fatalf("freed = %d, want %d", result.FreedBytes, want)
	}
	if result.Usage.TotalBytes != 0 || result.Usage.Files != 0 {
		t.Fatalf("usage after clear = %#v", result.Usage)
	}
	if _, err := os.Stat(filepath.Join(root, "autosave.pvf")); !os.IsNotExist(err) {
		t.Fatalf("backup payload survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "autosave.pvf.json")); !os.IsNotExist(err) {
		t.Fatalf("backup metadata survived: %v", err)
	}
	// The cache root stays, and the configuration directory is untouched.
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("cache root removed: %v", err)
	}
	// Webview-owned state is neither reported nor deleted.
	if _, err := os.Stat(filepath.Join(root, "WebKit", "WebsiteData", "local.db")); err != nil {
		t.Fatalf("webview cache removed: %v", err)
	}
	for _, name := range []string{"settings.json", "bookmarks.json"} {
		if _, err := os.Stat(filepath.Join(configDir, name)); err != nil {
			t.Fatalf("config file %s touched: %v", name, err)
		}
	}
}

func TestCacheServiceKeepsIndexesInUse(t *testing.T) {
	root, _, settings := newCacheFixture(t)
	// The index handles are fakes: there is nothing to close afterwards.
	core := NewCore()
	core.diskIndex = &sqliteArchiveIndex{path: filepath.Join(root, "archive-index", "deadbeef.db")}
	core.advancedDisk = &advancedSQLite{
		dir:       filepath.Join(root, "advanced-search", "session-1"),
		cachePath: filepath.Join(root, "advanced-search", "refs-v1-abc.db"),
	}
	service := newCacheServiceWithRoot(core, settings, root)

	usage, err := service.Usage()
	if err != nil {
		t.Fatal(err)
	}
	const inUse = 256 + 32 + 64 + 512
	if usage.InUseBytes != inUse {
		t.Fatalf("in-use = %d, want %d", usage.InUseBytes, inUse)
	}

	result, err := service.Clear()
	if err != nil {
		t.Fatal(err)
	}
	if result.FreedBytes != usage.TotalBytes-inUse {
		t.Fatalf("freed = %d, want %d", result.FreedBytes, usage.TotalBytes-inUse)
	}
	for _, keep := range []string{
		filepath.Join(root, "archive-index", "deadbeef.db"),
		filepath.Join(root, "archive-index", "deadbeef.db-wal"),
		filepath.Join(root, "advanced-search", "session-1", "session.db"),
		filepath.Join(root, "advanced-search", "refs-v1-abc.db"),
	} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("in-use cache %s removed: %v", keep, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "search-index", "abcdef.json.gz")); !os.IsNotExist(err) {
		t.Fatalf("search index cache survived: %v", err)
	}
}

func TestCacheServiceCountsAutosaveOutsideCacheRoot(t *testing.T) {
	root, configDir, settings := newCacheFixture(t)
	external := filepath.Join(t.TempDir(), "backup", "workspace.pvf")
	writeFixtureFile(t, external, 2048)
	writeFixtureFile(t, external+".json", 8)
	appSettings := DefaultAppSettings()
	appSettings.AutosavePath = external
	if err := settings.SaveSettings(appSettings); err != nil {
		t.Fatal(err)
	}
	_ = configDir
	service := newCacheServiceWithRoot(NewCore(), settings, root)

	usage, err := service.Usage()
	if err != nil {
		t.Fatal(err)
	}
	// Eight files inside the cache root plus the two files of the external slot.
	if usage.Files != 10 {
		t.Fatalf("files = %d, want 10", usage.Files)
	}
	if usage.TotalBytes != 128+256+32+64+512+16+1024+4+2048+8 {
		t.Fatalf("total = %d", usage.TotalBytes)
	}

	if _, err := service.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(external); !os.IsNotExist(err) {
		t.Fatalf("external backup survived: %v", err)
	}
	if _, err := os.Stat(external + ".json"); !os.IsNotExist(err) {
		t.Fatalf("external backup metadata survived: %v", err)
	}
}

func TestCacheServiceClearOnMissingRoot(t *testing.T) {
	settings := newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	// Pin the backup slot to a temporary path so the report never looks at the
	// machine's real cache directory.
	appSettings := DefaultAppSettings()
	appSettings.AutosavePath = filepath.Join(t.TempDir(), "backup.pvf")
	if err := settings.SaveSettings(appSettings); err != nil {
		t.Fatal(err)
	}
	service := newCacheServiceWithRoot(NewCore(), settings, filepath.Join(t.TempDir(), "absent"))

	usage, err := service.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if usage.Files != 0 || usage.TotalBytes != 0 {
		t.Fatalf("usage = %#v, want empty", usage)
	}
	result, err := service.Clear()
	if err != nil {
		t.Fatal(err)
	}
	if result.FreedBytes < 0 {
		t.Fatalf("freed = %d", result.FreedBytes)
	}
}

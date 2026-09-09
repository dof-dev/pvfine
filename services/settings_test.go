package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsServiceDefaultsAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	service := newSettingsService(path)
	settings, err := service.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.AnnotationTagPlacement != AnnotationTagAfterTarget {
		t.Fatalf("default placement = %q", settings.AnnotationTagPlacement)
	}
	if settings.ExplorerOpenMode != ExplorerOpenSingleClick {
		t.Fatalf("default explorer open mode = %q", settings.ExplorerOpenMode)
	}
	if settings.VimMode {
		t.Fatal("default vim mode = true, want false")
	}
	if !settings.BackupSourceOnSave {
		t.Fatal("default backup source on save = false, want true")
	}
	if settings.Theme != ThemeDark {
		t.Fatalf("default theme = %q, want %q", settings.Theme, ThemeDark)
	}

	settings.AnnotationTagPlacement = AnnotationTagLineEnd
	settings.ExplorerOpenMode = ExplorerOpenDoubleClick
	settings.VimMode = true
	settings.BackupSourceOnSave = false
	settings.Theme = ThemeLight
	if err := service.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != settings {
		t.Fatalf("loaded = %#v, want %#v", loaded, settings)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings mode = %v, err = %v", info.Mode().Perm(), err)
	}
}

func TestSettingsServiceRejectsInvalidPlacement(t *testing.T) {
	service := newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	err := service.SaveSettings(AppSettings{
		AnnotationTagPlacement: "floating",
		ExplorerOpenMode:       ExplorerOpenSingleClick,
		Theme:                  ThemeDark,
	})
	if err == nil {
		t.Fatal("expected invalid placement error")
	}
}

func TestSettingsServiceEnablesBackupForLegacySettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := []byte(`{"annotationTagPlacement":"after-target","explorerOpenMode":"single-click","vimMode":false}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := newSettingsService(path).GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.BackupSourceOnSave {
		t.Fatal("legacy settings disabled source backup, want true")
	}
}

func TestSettingsServiceRejectsInvalidExplorerOpenMode(t *testing.T) {
	service := newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	err := service.SaveSettings(AppSettings{
		AnnotationTagPlacement: AnnotationTagAfterTarget,
		ExplorerOpenMode:       "middle-click",
		Theme:                  ThemeDark,
	})
	if err == nil {
		t.Fatal("expected invalid explorer open mode error")
	}
}

func TestSettingsServiceSupportsSystemTheme(t *testing.T) {
	service := newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	settings := DefaultAppSettings()
	settings.Theme = ThemeSystem
	if err := service.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Theme != ThemeSystem {
		t.Fatalf("loaded theme = %q, want %q", loaded.Theme, ThemeSystem)
	}
}

func TestSettingsServiceLegacySettingsUseDarkTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := []byte(`{"annotationTagPlacement":"after-target","explorerOpenMode":"single-click","vimMode":false}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := newSettingsService(path).GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Theme != ThemeDark {
		t.Fatalf("legacy theme = %q, want %q", settings.Theme, ThemeDark)
	}
}

func TestSettingsServiceRejectsInvalidTheme(t *testing.T) {
	service := newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	settings := DefaultAppSettings()
	settings.Theme = "solarized"
	if err := service.SaveSettings(settings); err == nil {
		t.Fatal("expected invalid theme error")
	}
}

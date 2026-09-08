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

	settings.AnnotationTagPlacement = AnnotationTagLineEnd
	settings.ExplorerOpenMode = ExplorerOpenDoubleClick
	settings.VimMode = true
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
	})
	if err == nil {
		t.Fatal("expected invalid placement error")
	}
}

func TestSettingsServiceRejectsInvalidExplorerOpenMode(t *testing.T) {
	service := newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	err := service.SaveSettings(AppSettings{
		AnnotationTagPlacement: AnnotationTagAfterTarget,
		ExplorerOpenMode:       "middle-click",
	})
	if err == nil {
		t.Fatal("expected invalid explorer open mode error")
	}
}

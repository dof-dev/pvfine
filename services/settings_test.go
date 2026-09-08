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

	settings.AnnotationTagPlacement = AnnotationTagLineEnd
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
	err := service.SaveSettings(AppSettings{AnnotationTagPlacement: "floating"})
	if err == nil {
		t.Fatal("expected invalid placement error")
	}
}

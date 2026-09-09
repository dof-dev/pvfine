package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealImageServiceWithImagePacks2(t *testing.T) {
	root := os.Getenv("NPK_TESTDIR")
	if root == "" {
		t.Skip("NPK_TESTDIR 未设置")
	}
	if !filepath.IsAbs(root) {
		candidate := root
		for index := 0; index < 4; index++ {
			if _, err := os.Stat(candidate); err == nil {
				root, _ = filepath.Abs(candidate)
				break
			}
			candidate = filepath.Join("..", candidate)
		}
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	settings := newSettingsService(filepath.Join(t.TempDir(), "settings.json"))
	configured := DefaultAppSettings()
	configured.NPKDirectory = root
	if err := settings.SaveSettings(configured); err != nil {
		t.Fatal(err)
	}
	service := &ImageService{settings: settings, cachePath: filepath.Join(t.TempDir(), "npk-image-index.json"), cache: make(map[string]imageCacheEntry)}
	cachePath := service.cachePath
	service.Initialize()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		status := service.IndexStatus()
		if status.State == ImageIndexStateReady {
			if status.NPKFiles != 120 || status.IMGFiles != 1692 || status.ImageCount != 145816 {
				t.Fatalf("image status = %#v", status)
			}
			if status.BuildDurationMs <= 0 {
				t.Fatalf("image timing = %#v, want positive build duration", status)
			}
			data, err := service.GetImage("Item/new_equipment/08_necklace/necklace.img", 69)
			if err != nil {
				t.Fatal(err)
			}
			if data == nil || data.Width <= 0 || data.Height <= 0 || len(data.DataURL) < len("data:image/png;base64,") {
				t.Fatalf("sample image data = %#v", data)
			}
			if missing, err := service.GetImage("missing.img", 0); err != nil || missing != nil {
				t.Fatalf("missing image = %#v, %v", missing, err)
			}
			if outOfRange, err := service.GetImage("Item/new_equipment/08_necklace/necklace.img", 1000000); err != nil || outOfRange != nil {
				t.Fatalf("out-of-range image = %#v, %v", outOfRange, err)
			}
			cached := &ImageService{settings: settings, cachePath: cachePath, cache: make(map[string]imageCacheEntry)}
			cached.Initialize()
			cachedStatus := cached.IndexStatus()
			if cachedStatus.State != ImageIndexStateReady || cachedStatus.ImageCount != 145816 {
				t.Fatalf("persistent image status = %#v", cachedStatus)
			}
			return
		}
		if status.State == ImageIndexStateError {
			t.Fatalf("image index failed: %s", status.Error)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("image index did not finish: %#v", service.IndexStatus())
}

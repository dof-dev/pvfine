package services

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImageIndexScansNPKsConcurrentlyAndKeepsOrder(t *testing.T) {
	root := t.TempDir()
	writeImageTestNPK(t, filepath.Join(root, "01.npk"), "sprite/item/shared.img")
	writeImageTestNPK(t, filepath.Join(root, "02.npk"), "sprite/item/shared.img")
	writeImageTestNPK(t, filepath.Join(root, "03.npk"), "sprite/item/unique.img")

	service := &ImageService{
		directory:  root,
		cachePath:  filepath.Join(t.TempDir(), "npk-image-index.json"),
		cache:      make(map[string]imageCacheEntry),
		generation: 1,
	}
	service.buildIndex(context.Background(), root, 1, time.Now())

	status := service.IndexStatus()
	if status.State != ImageIndexStateReady {
		t.Fatalf("image index status = %#v", status)
	}
	if status.NPKFiles != 3 || status.IMGFiles != 3 || status.ImageCount != 3 || status.Duplicates != 1 {
		t.Fatalf("image index counts = %#v", status)
	}
	if len(service.snapshot.records) != 2 {
		t.Fatalf("image records = %d, want 2", len(service.snapshot.records))
	}
	if got := service.snapshot.byPath["sprite/item/shared.img"].npkRelative; got != "01.npk" {
		t.Fatalf("duplicate winner = %q, want 01.npk", got)
	}
}

func writeImageTestNPK(t *testing.T, path, memberName string) {
	t.Helper()
	const npkHeaderSize = 16 + 4 + 264
	member := imageTestIMG()
	data := make([]byte, 0, npkHeaderSize+len(member))
	data = append(data, []byte("NeoplePack_Bill")...)
	data = append(data, 0)
	putImageTestInt32(&data, 1)
	putImageTestInt32(&data, npkHeaderSize, len(member))
	key := []byte("puchikon@neople dungeon and fighter " + strings.Repeat("DNF", 73) + "\x00")
	encrypted := make([]byte, 256)
	for index := range encrypted {
		var value byte
		if index < len(memberName) {
			value = memberName[index]
		}
		encrypted[index] = value ^ key[index%len(key)]
	}
	data = append(data, encrypted...)
	data = append(data, member...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func imageTestIMG() []byte {
	data := make([]byte, 0, 72)
	data = append(data, []byte("Neople Img File")...)
	data = append(data, 0)
	putImageTestInt32(&data, 36, 0, 2, 1)
	putImageTestInt32(&data, 16, 5, 1, 1, 4, 0, 0, 1, 1)
	data = append(data, 3, 2, 1, 255)
	return data
}

func putImageTestInt32(data *[]byte, values ...int) {
	for _, value := range values {
		var raw [4]byte
		binary.LittleEndian.PutUint32(raw[:], uint32(value))
		*data = append(*data, raw[:]...)
	}
}

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

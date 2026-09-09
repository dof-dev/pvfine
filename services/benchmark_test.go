package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkRealResources measures the three user-visible startup stages with
// the real Script.pvf and ImagePacks2 resources. Run it through
// scripts/benchmark.sh, which uses -benchtime=1x so one run produces one
// comparable set of numbers.
func BenchmarkRealResources(b *testing.B) {
	pvfPath := resolveBenchmarkPath(b, "PVF_TESTFILE", "Script.pvf")
	npkDirectory := resolveBenchmarkPath(b, "NPK_TESTDIR", "ImagePacks2")

	for iteration := 0; iteration < b.N; iteration++ {
		core := NewCore()
		archiveService := NewArchiveService(core)
		openStartedAt := time.Now()
		if _, err := archiveService.Open(pvfPath); err != nil {
			b.Fatalf("打开 PVF 失败: %v", err)
		}
		openElapsedMs := durationMilliseconds(time.Since(openStartedAt))
		indexStatus := waitBenchmarkSearchIndex(b, archiveService)
		b.Logf("打开 Script.pvf 到可操作: %.3f ms", openElapsedMs)
		b.Logf("构建 PVF 索引: %.3f ms", indexStatus.BuildDurationMs)
		core.closeArchive()

		settings := newSettingsService(filepath.Join(b.TempDir(), "settings.json"))
		configured := DefaultAppSettings()
		configured.NPKDirectory = npkDirectory
		if err := settings.SaveSettings(configured); err != nil {
			b.Fatalf("写入基准测试设置失败: %v", err)
		}
		imageService := &ImageService{
			settings:  settings,
			cachePath: filepath.Join(b.TempDir(), "npk-image-index.json"),
			cache:     make(map[string]imageCacheEntry),
		}
		imageService.Initialize()
		imageStatus := waitBenchmarkImageIndex(b, imageService)
		b.Logf("构建 NPK 索引: %.3f ms", imageStatus.BuildDurationMs)
	}
}

func resolveBenchmarkPath(b *testing.B, environment, fallback string) string {
	b.Helper()
	value := os.Getenv(environment)
	if value == "" {
		value = fallback
	}
	if filepath.IsAbs(value) {
		if _, err := os.Stat(value); err == nil {
			return value
		}
	} else {
		candidates := []string{value, filepath.Join("..", value), filepath.Join("..", "..", value)}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				absolute, err := filepath.Abs(candidate)
				if err == nil {
					return absolute
				}
			}
		}
	}
	b.Skipf("%s 未找到: %s", environment, value)
	return ""
}

func waitBenchmarkSearchIndex(b *testing.B, service *ArchiveService) IndexStatus {
	b.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		status := service.IndexStatus()
		switch status.State {
		case IndexStateReady:
			return status
		case IndexStateError:
			b.Fatalf("PVF 索引失败: %s", status.Error)
		}
		time.Sleep(time.Millisecond)
	}
	b.Fatalf("PVF 索引超时: %#v", service.IndexStatus())
	return IndexStatus{}
}

func waitBenchmarkImageIndex(b *testing.B, service *ImageService) ImageIndexStatus {
	b.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		status := service.IndexStatus()
		switch status.State {
		case ImageIndexStateReady:
			return status
		case ImageIndexStateError:
			b.Fatalf("NPK 索引失败: %s", status.Error)
		}
		time.Sleep(time.Millisecond)
	}
	b.Fatalf("NPK 索引超时: %#v", service.IndexStatus())
	return ImageIndexStatus{}
}

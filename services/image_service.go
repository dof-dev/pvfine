package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"pvfine/internal/npk"
)

const imageIndexVersion = 1

// NPK packages are independent scan units. Keep the pool bounded so a large
// resource directory does not create one goroutine and file descriptor per
// package or overwhelm slower disks with unbounded random reads.
const maxImageScanWorkers = 8

var (
	errImageDirectoryMissing = errors.New("NPK 目录不存在或不是目录")
	imageCacheMaxEntries     = 4096
	imageCacheMaxBytes       = 64 << 20
)

type imageManifest struct {
	Relative string `json:"relative"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

type persistedImageRecord struct {
	Path        string       `json:"path"`
	NPK         string       `json:"npk"`
	EntryOffset int64        `json:"entryOffset"`
	EntrySize   int64        `json:"entrySize"`
	IMG         *npk.IMGFile `json:"img"`
}

type persistedImageIndex struct {
	Version    int                    `json:"version"`
	Directory  string                 `json:"directory"`
	Manifest   []imageManifest        `json:"manifest"`
	Images     []persistedImageRecord `json:"images"`
	NPKFiles   int                    `json:"npkFiles"`
	IMGFiles   int                    `json:"imgFiles"`
	ImageCount int                    `json:"imageCount"`
	Skipped    int                    `json:"skipped"`
	Duplicates int                    `json:"duplicates"`
}

type imageRecord struct {
	path        string
	npkRelative string
	offset      int64
	size        int64
	img         *npk.IMGFile
}

type imageSnapshot struct {
	directory  string
	manifest   []imageManifest
	byPath     map[string]*imageRecord
	records    []*imageRecord
	npkFiles   int
	imgFiles   int
	imageCount int
	skipped    int
	duplicates int
}

type imageCacheEntry struct {
	data  ImageData
	bytes int
}

type imageScanResult struct {
	index      int
	npkFiles   int
	imgFiles   int
	imageCount int
	skipped    int
	records    []*imageRecord
}

// ImageService owns the NPK/IMG metadata snapshot and on-demand PNG cache.
// It does not expose source NPK paths to the frontend.
type ImageService struct {
	mu        sync.RWMutex
	settings  *SettingsService
	cachePath string // test override; production uses os.UserCacheDir()

	directory  string
	snapshot   *imageSnapshot
	status     ImageIndexStatus
	cancel     context.CancelFunc
	generation uint64

	cache      map[string]imageCacheEntry
	cacheOrder []string
	cacheBytes int
}

func NewImageService(_ *core, settings *SettingsService) *ImageService {
	return &ImageService{settings: settings, cache: make(map[string]imageCacheEntry), status: ImageIndexStatus{State: ImageIndexStateIdle, Stage: "idle"}}
}

// Initialize loads a valid persistent snapshot, or starts an asynchronous
// scan for the configured directory.
func (s *ImageService) Initialize() ImageIndexStatus {
	if s.settings == nil {
		return s.IndexStatus()
	}
	settings, err := s.settings.GetSettings()
	if err != nil {
		s.setError("读取 NPK 设置失败", err)
		return s.IndexStatus()
	}
	directory, err := canonicalImageDirectory(settings.NPKDirectory)
	if settings.NPKDirectory == "" {
		s.mu.Lock()
		if s.cancel != nil {
			s.cancel()
			s.cancel = nil
		}
		s.generation++
		s.directory = ""
		s.snapshot = nil
		s.clearCacheLocked()
		s.status = ImageIndexStatus{State: ImageIndexStateIdle, Stage: "idle", Generation: s.generation}
		s.mu.Unlock()
		return s.IndexStatus()
	}
	if err != nil {
		s.setErrorForDirectory("NPK 目录不可用", settings.NPKDirectory, err)
		return s.IndexStatus()
	}

	s.mu.Lock()
	if s.directory == directory && (s.snapshot != nil || s.status.State == ImageIndexStateBuilding) {
		status := s.status
		s.mu.Unlock()
		return status
	}
	s.directory = directory
	s.snapshot = nil
	s.clearCacheLocked()
	if s.cancel != nil {
		s.cancel()
	}
	s.generation++
	generation := s.generation
	s.mu.Unlock()

	if snapshot, err := loadImageIndexCacheAt(directory, s.cachePath); err == nil {
		s.mu.Lock()
		if s.generation == generation && s.directory == directory {
			s.snapshot = snapshot
			s.status = readyImageStatus(snapshot, generation)
			s.mu.Unlock()
			emitEvent("image:index-ready", s.IndexStatus())
			return s.IndexStatus()
		}
		s.mu.Unlock()
	}
	s.startBuild(directory, generation)
	return s.IndexStatus()
}

// SelectNPKDirectory opens the native directory chooser, persists the
// selected path and starts a new asynchronous index generation.
func (s *ImageService) SelectNPKDirectory() (ImageIndexStatus, error) {
	app := application.Get()
	if app == nil {
		return s.IndexStatus(), errors.New("当前运行环境不支持目录选择")
	}
	path, err := app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(false).
		SetTitle("选择 NPK 目录").
		PromptForSingleSelection()
	if err != nil {
		return s.IndexStatus(), err
	}
	if strings.TrimSpace(path) == "" {
		return s.IndexStatus(), nil
	}
	directory, err := canonicalImageDirectory(path)
	if err != nil {
		return s.IndexStatus(), err
	}
	if s.settings != nil {
		if _, err := s.settings.UpdateNPKDirectory(directory); err != nil {
			return s.IndexStatus(), err
		}
	}
	s.switchDirectory(directory)
	return s.IndexStatus(), nil
}

// RebuildIndex starts a manual rebuild. Existing image data remains available
// through the old immutable snapshot until the new one is ready.
func (s *ImageService) RebuildIndex() (ImageIndexStatus, error) {
	s.mu.Lock()
	directory := s.directory
	if directory == "" {
		s.mu.Unlock()
		return s.status, nil
	}
	s.generation++
	generation := s.generation
	if s.cancel != nil {
		s.cancel()
	}
	s.clearCacheLocked()
	s.mu.Unlock()
	s.startBuild(directory, generation)
	return s.IndexStatus(), nil
}

// IndexStatus returns a copy of the current asynchronous scan state.
func (s *ImageService) IndexStatus() ImageIndexStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// GetImage decodes one indexed frame and encodes it as a PNG data URL.
// Missing configuration, unfinished indexes, missing paths and out-of-range
// frames intentionally return (nil, nil) so callers can render a fallback.
func (s *ImageService) GetImage(path string, index int32) (*ImageData, error) {
	keyPath := normalizeImagePath(path)
	if keyPath == "" || index < 0 {
		return nil, nil
	}
	s.mu.RLock()
	snapshot := s.snapshot
	generation := s.generation
	if snapshot == nil || s.directory == "" {
		s.mu.RUnlock()
		return nil, nil
	}
	record := snapshot.byPath[keyPath]
	if record == nil || record.img == nil || index >= int32(len(record.img.Images)) {
		s.mu.RUnlock()
		return nil, nil
	}
	cacheKey := fmt.Sprintf("%d\x00%s\x00%d", generation, keyPath, index)
	if cached, ok := s.cache[cacheKey]; ok {
		s.mu.RUnlock()
		return &cached.data, nil
	}
	directory := snapshot.directory
	npkPath := filepath.Join(directory, filepath.FromSlash(record.npkRelative))
	if !isImagePathWithin(directory, npkPath) {
		s.mu.RUnlock()
		return nil, errors.New("NPK 索引路径越界")
	}
	npkOffset, npkSize, img := record.offset, record.size, record.img
	s.mu.RUnlock()

	file, err := os.Open(npkPath)
	if err != nil {
		return nil, fmt.Errorf("读取 NPK 文件失败: %w", err)
	}
	decoded, err := npk.DecodeImage(file, npkOffset, npkSize, img, index)
	_ = file.Close()
	if err != nil {
		return nil, fmt.Errorf("解码 IMG 图片失败: %w", err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, decoded); err != nil {
		return nil, fmt.Errorf("编码 PNG 失败: %w", err)
	}
	bounds := decoded.Bounds()
	result := ImageData{
		DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()),
		Width:   int32(bounds.Dx()),
		Height:  int32(bounds.Dy()),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation || s.snapshot != snapshot {
		return nil, nil
	}
	s.putCacheLocked(cacheKey, result, encoded.Len())
	return &result, nil
}

func (s *ImageService) switchDirectory(directory string) {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.directory = directory
	s.snapshot = nil
	s.clearCacheLocked()
	s.generation++
	generation := s.generation
	s.status = ImageIndexStatus{State: ImageIndexStateBuilding, Stage: "scanning", Directory: directory, Generation: generation}
	s.mu.Unlock()
	emitEvent("image:index-progress", s.IndexStatus())
	s.startBuild(directory, generation)
}

func (s *ImageService) startBuild(directory string, generation uint64) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if s.generation != generation || s.directory != directory {
		s.mu.Unlock()
		cancel()
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.cancel = cancel
	s.status = ImageIndexStatus{State: ImageIndexStateBuilding, Stage: "scanning", Directory: directory, Generation: generation}
	s.mu.Unlock()
	emitEvent("image:index-progress", s.IndexStatus())
	go s.buildIndex(ctx, directory, generation, time.Now())
}

func (s *ImageService) buildIndex(ctx context.Context, directory string, generation uint64, startedAt time.Time) {
	manifest, err := scanImageManifest(directory)
	if err != nil {
		s.failBuild(directory, generation, startedAt, err)
		return
	}
	status := ImageIndexStatus{State: ImageIndexStateBuilding, Stage: "scanning", Directory: directory, Total: len(manifest), Generation: generation, BuildDurationMs: elapsedMilliseconds(startedAt)}
	s.publishProgress(directory, generation, status)

	byPath := make(map[string]*imageRecord)
	records := make([]*imageRecord, 0)
	duplicates := 0
	skipped := 0
	npkCount := 0
	imgCount := 0
	imageCount := 0
	scanResults, err := scanImageNPKs(ctx, directory, manifest, 0, func(result imageScanResult, done int) {
		npkCount += result.npkFiles
		imgCount += result.imgFiles
		imageCount += result.imageCount
		skipped += result.skipped
		status.Done = done
		status.NPKFiles = npkCount
		status.IMGFiles = imgCount
		status.ImageCount = imageCount
		status.Skipped = skipped
		status.BuildDurationMs = elapsedMilliseconds(startedAt)
		s.publishProgress(directory, generation, status)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		s.failBuild(directory, generation, startedAt, err)
		return
	}
	for _, result := range scanResults {
		for _, record := range result.records {
			if _, exists := byPath[record.path]; exists {
				duplicates++
				continue
			}
			byPath[record.path] = record
			for _, alias := range imageIndexAliases(record.path) {
				if _, exists := byPath[alias]; !exists {
					byPath[alias] = record
				}
			}
			records = append(records, record)
		}
	}
	status.NPKFiles = len(manifest)
	status.IMGFiles = imgCount
	status.ImageCount = imageCount
	status.Skipped = skipped
	status.Duplicates = duplicates
	status.Done = len(manifest)
	status.BuildDurationMs = elapsedMilliseconds(startedAt)
	s.publishProgress(directory, generation, status)
	if len(records) == 0 {
		s.failBuild(directory, generation, startedAt, fmt.Errorf("NPK 目录中没有可用的 IMG 文件"))
		return
	}
	snapshot := &imageSnapshot{
		directory: directory, manifest: manifest, byPath: byPath, records: records,
		npkFiles: len(manifest), imgFiles: imgCount, imageCount: imageCount,
		skipped: skipped, duplicates: duplicates,
	}
	if err := saveImageIndexCacheAt(snapshot, s.cachePath); err != nil {
		// Cache persistence should not make otherwise valid image data unusable.
		status.Error = "索引已生成，但缓存保存失败: " + err.Error()
	}
	s.mu.Lock()
	if s.generation != generation || s.directory != directory || ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	s.snapshot = snapshot
	s.cancel = nil
	s.status = ImageIndexStatus{State: ImageIndexStateReady, Stage: "ready", Directory: directory, Done: len(manifest), Total: len(manifest), NPKFiles: len(manifest), IMGFiles: imgCount, ImageCount: imageCount, Skipped: skipped, Duplicates: duplicates, Generation: generation, BuildDurationMs: elapsedMilliseconds(startedAt)}
	ready := s.status
	s.mu.Unlock()
	emitEvent("image:index-ready", ready)
}

func imageScanWorkerCount(fileCount int) int {
	if fileCount <= 0 {
		return 0
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	if workers > maxImageScanWorkers {
		workers = maxImageScanWorkers
	}
	if workers > fileCount {
		workers = fileCount
	}
	return workers
}

func scanImageNPKs(ctx context.Context, directory string, manifest []imageManifest, workers int, onResult func(imageScanResult, int)) ([]imageScanResult, error) {
	if len(manifest) == 0 {
		return []imageScanResult{}, nil
	}
	if workers <= 0 {
		workers = imageScanWorkerCount(len(manifest))
	}
	if workers > len(manifest) {
		workers = len(manifest)
	}

	type scanJob int
	jobs := make(chan scanJob)
	results := make(chan imageScanResult)
	var group sync.WaitGroup
	group.Add(workers)
	for index := 0; index < workers; index++ {
		go func() {
			defer group.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					result := scanImageNPK(ctx, directory, int(job), manifest[int(job)])
					if ctx.Err() != nil {
						return
					}
					select {
					case results <- result:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for index := range manifest {
			select {
			case jobs <- scanJob(index):
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		group.Wait()
		close(results)
	}()

	scanned := make([]imageScanResult, len(manifest))
	completed := 0
	for result := range results {
		scanned[result.index] = result
		completed++
		if onResult != nil {
			onResult(result, completed)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if completed != len(manifest) {
		return nil, fmt.Errorf("NPK 并发扫描未完成: %d/%d", completed, len(manifest))
	}
	return scanned, nil
}

func scanImageNPK(ctx context.Context, directory string, index int, item imageManifest) imageScanResult {
	result := imageScanResult{index: index}
	if ctx.Err() != nil {
		return result
	}
	npkPath := filepath.Join(directory, filepath.FromSlash(item.Relative))
	npkFile, err := npk.Open(npkPath)
	if err != nil {
		result.skipped++
		return result
	}
	if ctx.Err() != nil {
		return result
	}
	reader, err := os.Open(npkPath)
	if err != nil {
		result.skipped++
		return result
	}
	defer reader.Close()
	result.npkFiles = 1
	for _, entry := range npkFile.Entries {
		if ctx.Err() != nil {
			return result
		}
		if !strings.EqualFold(filepath.Ext(entry.Name), ".img") {
			continue
		}
		img, parseErr := npk.ParseIMGAt(reader, entry.Offset, entry.Size)
		if parseErr != nil {
			result.skipped++
			continue
		}
		result.imgFiles++
		result.imageCount += len(img.Images)
		record := &imageRecord{path: normalizeImagePath(entry.Name), npkRelative: item.Relative, offset: entry.Offset, size: entry.Size, img: img}
		if record.path == "" {
			result.skipped++
			continue
		}
		result.records = append(result.records, record)
	}
	return result
}

func (s *ImageService) publishProgress(directory string, generation uint64, status ImageIndexStatus) {
	status.Directory = directory
	status.Generation = generation
	s.mu.Lock()
	if s.generation != generation || s.directory != directory {
		s.mu.Unlock()
		return
	}
	s.status = status
	s.mu.Unlock()
	emitEvent("image:index-progress", status)
}

func (s *ImageService) failBuild(directory string, generation uint64, startedAt time.Time, err error) {
	s.mu.Lock()
	if s.generation != generation || s.directory != directory {
		s.mu.Unlock()
		return
	}
	s.cancel = nil
	s.status = ImageIndexStatus{State: ImageIndexStateError, Stage: "error", Directory: directory, Error: err.Error(), Generation: generation, BuildDurationMs: elapsedMilliseconds(startedAt)}
	status := s.status
	s.mu.Unlock()
	emitEvent("image:index-error", status)
}

func (s *ImageService) setError(stage string, err error) {
	s.setErrorForDirectory(stage, "", err)
}

func (s *ImageService) setErrorForDirectory(stage, directory string, err error) {
	s.mu.Lock()
	s.status = ImageIndexStatus{State: ImageIndexStateError, Stage: stage, Directory: directory, Error: err.Error(), Generation: s.generation}
	status := s.status
	s.mu.Unlock()
	emitEvent("image:index-error", status)
}

func (s *ImageService) clearCacheLocked() {
	s.cache = make(map[string]imageCacheEntry)
	s.cacheOrder = nil
	s.cacheBytes = 0
}

func (s *ImageService) putCacheLocked(key string, data ImageData, size int) {
	if size > imageCacheMaxBytes {
		return
	}
	if s.cache == nil {
		s.cache = make(map[string]imageCacheEntry)
	}
	if old, ok := s.cache[key]; ok {
		s.cacheBytes -= old.bytes
	}
	s.cache[key] = imageCacheEntry{data: data, bytes: size}
	s.cacheOrder = append(s.cacheOrder, key)
	s.cacheBytes += size
	for len(s.cacheOrder) > imageCacheMaxEntries || s.cacheBytes > imageCacheMaxBytes {
		oldKey := s.cacheOrder[0]
		s.cacheOrder = s.cacheOrder[1:]
		old, ok := s.cache[oldKey]
		if !ok {
			continue
		}
		delete(s.cache, oldKey)
		s.cacheBytes -= old.bytes
	}
}

func readyImageStatus(snapshot *imageSnapshot, generation uint64) ImageIndexStatus {
	return ImageIndexStatus{State: ImageIndexStateReady, Stage: "ready-cache", Directory: snapshot.directory, Done: len(snapshot.manifest), Total: len(snapshot.manifest), NPKFiles: snapshot.npkFiles, IMGFiles: snapshot.imgFiles, ImageCount: snapshot.imageCount, Skipped: snapshot.skipped, Duplicates: snapshot.duplicates, Generation: generation}
}

func canonicalImageDirectory(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	abs, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", errImageDirectoryMissing
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", errImageDirectoryMissing
	}
	if !info.IsDir() {
		return "", errImageDirectoryMissing
	}
	return filepath.Clean(resolved), nil
}

func scanImageManifest(directory string) ([]imageManifest, error) {
	result := make([]imageManifest, 0)
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".npk") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		result = append(result, imageManifest{Relative: filepath.ToSlash(relative), Size: info.Size(), Modified: info.ModTime().UnixNano()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Relative < result[j].Relative })
	return result, nil
}

func normalizeImagePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.Trim(value, "/")
	for strings.Contains(value, "//") {
		value = strings.ReplaceAll(value, "//", "/")
	}
	value = pathpkg.Clean(value)
	if value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return ""
	}
	return strings.ToLower(value)
}

func imageIndexAliases(value string) []string {
	value = normalizeImagePath(value)
	if value == "" {
		return nil
	}
	result := []string{value}
	if strings.HasPrefix(value, "sprite/") {
		result = append(result, strings.TrimPrefix(value, "sprite/"))
	} else {
		result = append(result, "sprite/"+value)
	}
	return result
}

func isImagePathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func imageCachePath() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "pvfine", "npk-image-index.json"), nil
}

func loadImageIndexCache(directory string) (*imageSnapshot, error) {
	return loadImageIndexCacheAt(directory, "")
}

func loadImageIndexCacheAt(directory, customPath string) (*imageSnapshot, error) {
	cachePath := customPath
	var err error
	if cachePath == "" {
		cachePath, err = imageCachePath()
		if err != nil {
			return nil, err
		}
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}
	var persisted persistedImageIndex
	if err := json.Unmarshal(data, &persisted); err != nil || persisted.Version != imageIndexVersion || filepath.Clean(persisted.Directory) != filepath.Clean(directory) {
		return nil, errors.New("图像索引缓存版本或目录不匹配")
	}
	manifest, err := scanImageManifest(directory)
	if err != nil || !sameImageManifest(manifest, persisted.Manifest) {
		return nil, errors.New("图像索引缓存已过期")
	}
	byPath := make(map[string]*imageRecord)
	records := make([]*imageRecord, 0, len(persisted.Images))
	for _, saved := range persisted.Images {
		if saved.IMG == nil || saved.Path == "" || saved.NPK == "" {
			continue
		}
		record := &imageRecord{path: saved.Path, npkRelative: saved.NPK, offset: saved.EntryOffset, size: saved.EntrySize, img: saved.IMG}
		records = append(records, record)
		for _, alias := range imageIndexAliases(saved.Path) {
			if _, exists := byPath[alias]; !exists {
				byPath[alias] = record
			}
		}
	}
	if len(records) == 0 {
		return nil, errors.New("图像索引缓存没有图片")
	}
	imgFiles := persisted.IMGFiles
	imageCount := persisted.ImageCount
	if imgFiles == 0 {
		imgFiles = len(records)
	}
	if imageCount == 0 {
		for _, record := range records {
			imageCount += len(record.img.Images)
		}
	}
	npkFiles := persisted.NPKFiles
	if npkFiles == 0 {
		npkFiles = len(manifest)
	}
	return &imageSnapshot{directory: directory, manifest: manifest, byPath: byPath, records: records, npkFiles: npkFiles, imgFiles: imgFiles, imageCount: imageCount, skipped: persisted.Skipped, duplicates: persisted.Duplicates}, nil
}

func saveImageIndexCache(snapshot *imageSnapshot) error {
	return saveImageIndexCacheAt(snapshot, "")
}

func saveImageIndexCacheAt(snapshot *imageSnapshot, customPath string) error {
	cachePath := customPath
	var err error
	if cachePath == "" {
		cachePath, err = imageCachePath()
		if err != nil {
			return err
		}
	}
	manifest := append([]imageManifest(nil), snapshot.manifest...)
	persisted := persistedImageIndex{Version: imageIndexVersion, Directory: snapshot.directory, Manifest: manifest, Images: make([]persistedImageRecord, 0, len(snapshot.records)), NPKFiles: snapshot.npkFiles, IMGFiles: snapshot.imgFiles, ImageCount: snapshot.imageCount, Skipped: snapshot.skipped, Duplicates: snapshot.duplicates}
	for _, record := range snapshot.records {
		persisted.Images = append(persisted.Images, persistedImageRecord{Path: record.path, NPK: record.npkRelative, EntryOffset: record.offset, EntrySize: record.size, IMG: record.img})
	}
	data, err := json.Marshal(persisted)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(cachePath), ".npk-image-index-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, cachePath)
}

func sameImageManifest(a, b []imageManifest) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

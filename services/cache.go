package services

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CacheService reports and clears the caches the application keeps for itself:
// the semantic search index, the SQLite archive index, the advanced search
// index, the NPK icon index and the timed workspace backup. Everything below
// the cache root is either rebuilt on demand or disposable, so clearing can
// never remove configuration, bookmarks or file sets (those live in the user
// config directory).
type CacheService struct {
	c        *core
	settings *SettingsService

	// rootOverride is only set by tests.
	rootOverride string
}

// NewCacheService creates the cache maintenance service.
func NewCacheService(c *core, settings *SettingsService) *CacheService {
	return &CacheService{c: c, settings: settings}
}

func newCacheServiceWithRoot(c *core, settings *SettingsService, root string) *CacheService {
	return &CacheService{c: c, settings: settings, rootOverride: root}
}

// CacheUsage is the disk footprint reported to the settings page.
type CacheUsage struct {
	Path       string `json:"path"`
	Files      int    `json:"files"`
	TotalBytes int64  `json:"totalBytes"`
	// InUseBytes covers the indexes the running session holds open. They are
	// rebuilt once the archive is closed, so a clear pass has to skip them.
	InUseBytes int64 `json:"inUseBytes"`
}

// CacheClearResult reports what one clear pass removed.
type CacheClearResult struct {
	FreedBytes int64      `json:"freedBytes"`
	Usage      CacheUsage `json:"usage"`
}

// Usage sums the cache without changing anything.
func (s *CacheService) Usage() (CacheUsage, error) {
	root, err := s.cacheRoot()
	if err != nil {
		return CacheUsage{}, err
	}
	usage := CacheUsage{Path: root}
	inUse := s.inUsePaths()
	if err := walkCacheRoot(root, func(path string, info fs.FileInfo) {
		usage.Files++
		usage.TotalBytes += info.Size()
		if pathInUse(path, inUse) {
			usage.InUseBytes += info.Size()
		}
	}); err != nil {
		return CacheUsage{}, err
	}
	// The backup slot can be configured outside the cache root; it still counts
	// as application cache.
	for _, path := range s.outerAutosaveFiles(root) {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		usage.Files++
		usage.TotalBytes += info.Size()
	}
	return usage, nil
}

// Clear deletes every cache entry it can, leaving indexes that are still in use
// by the running session in place.
func (s *CacheService) Clear() (CacheClearResult, error) {
	root, err := s.cacheRoot()
	if err != nil {
		return CacheClearResult{}, err
	}
	inUse := s.inUsePaths()
	freed := int64(0)
	if err := walkCacheRoot(root, func(path string, info fs.FileInfo) {
		if pathInUse(path, inUse) {
			return
		}
		if err := os.Remove(path); err == nil {
			freed += info.Size()
		}
	}); err != nil {
		return CacheClearResult{}, err
	}
	pruneEmptyCacheDirs(root, inUse)
	for _, path := range s.outerAutosaveFiles(root) {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if err := os.Remove(path); err == nil {
			freed += info.Size()
		}
	}
	usage, err := s.Usage()
	if err != nil {
		return CacheClearResult{}, err
	}
	return CacheClearResult{FreedBytes: freed, Usage: usage}, nil
}

func (s *CacheService) cacheRoot() (string, error) {
	if s == nil {
		return "", fmt.Errorf("缓存服务不可用")
	}
	if s.rootOverride != "" {
		return s.rootOverride, nil
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("获取用户缓存目录失败: %w", err)
	}
	return filepath.Join(root, "pvfine"), nil
}

// inUsePaths lists the cache files the live session keeps open. SQLite sidecars
// are listed as well: removing a -wal file under an open connection would
// corrupt that database.
func (s *CacheService) inUsePaths() []string {
	if s == nil || s.c == nil {
		return nil
	}
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	var paths []string
	add := func(path string) {
		if path == "" {
			return
		}
		paths = append(paths, path, path+"-wal", path+"-shm", path+"-journal")
	}
	if s.c.diskIndex != nil {
		add(s.c.diskIndex.path)
	}
	if s.c.advancedDisk != nil {
		add(s.c.advancedDisk.dir)
		add(s.c.advancedDisk.cachePath)
	}
	return paths
}

// outerAutosaveFiles returns the backup payload and metadata files that sit
// outside the cache root, so a custom location is reported and cleared too.
func (s *CacheService) outerAutosaveFiles(root string) []string {
	var result []string
	for _, path := range s.autosaveSlotFiles() {
		if !pathWithin(root, path) {
			result = append(result, path)
		}
	}
	return result
}

// autosaveSlotFiles returns the backup payload and its metadata file.
func (s *CacheService) autosaveSlotFiles() []string {
	if s == nil || s.settings == nil {
		return nil
	}
	settings, err := s.settings.GetSettings()
	if err != nil {
		return nil
	}
	path := strings.TrimSpace(settings.AutosavePath)
	if path == "" {
		if path, err = DefaultAutosavePath(); err != nil {
			return nil
		}
	}
	return []string{path, path + ".json"}
}

// webviewCacheEntries are directories under the cache root that belong to the
// embedded webview instead of the application. They hold live webview state
// (localStorage, IndexedDB), so their size is not reported and a clear pass must
// never delete them while the window is running.
var webviewCacheEntries = map[string]struct{}{"WebKit": {}}

// walkCacheRoot calls fn for every regular file below root. A missing root is
// not an error: it just means nothing has been cached yet.
func walkCacheRoot(root string, fn func(path string, info fs.FileInfo)) error {
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == root && os.IsNotExist(err) {
				return filepath.SkipAll
			}
			// A cache entry that cannot be read is not worth failing the whole
			// report for.
			return nil
		}
		if entry.IsDir() {
			if isWebviewCacheEntry(root, path) {
				return filepath.SkipDir
			}
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		fn(path, info)
		return nil
	})
	if err != nil {
		return fmt.Errorf("统计缓存占用失败: %w", err)
	}
	return nil
}

// isWebviewCacheEntry reports whether path is a webview-owned directory directly
// below the cache root.
func isWebviewCacheEntry(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	_, skip := webviewCacheEntries[relative]
	return skip
}

func pathInUse(path string, inUse []string) bool {
	for _, candidate := range inUse {
		if pathWithin(candidate, path) {
			return true
		}
	}
	return false
}

// pathWithin reports whether candidate is root itself or sits inside it.
func pathWithin(root, candidate string) bool {
	if root == "" || candidate == "" {
		return false
	}
	cleanedRoot := filepath.Clean(root)
	cleanedCandidate := filepath.Clean(candidate)
	if cleanedRoot == cleanedCandidate {
		return true
	}
	return strings.HasPrefix(cleanedCandidate, cleanedRoot+string(filepath.Separator))
}

// pruneEmptyCacheDirs removes the directories a clear pass emptied, keeping the
// cache root and anything that still holds files (in-use indexes).
func pruneEmptyCacheDirs(root string, inUse []string) {
	directories := make([]string, 0, 8)
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && path != root {
			if isWebviewCacheEntry(root, path) {
				return filepath.SkipDir
			}
			directories = append(directories, path)
		}
		return nil
	})
	// Deepest first so a parent can become empty by its children leaving.
	for i := len(directories) - 1; i >= 0; i-- {
		path := directories[i]
		if pathInUse(path, inUse) {
			continue
		}
		_ = os.Remove(path) // fails while the directory still has entries
	}
}

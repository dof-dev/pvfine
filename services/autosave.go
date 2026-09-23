package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

// autosaveMetaVersion guards the metadata layout. A slot written by an
// incompatible version is reported as unusable instead of restored wrongly.
const autosaveMetaVersion = 1

// autosaveVersionWait bounds the wait for the background version session,
// which has to be attached before recovered edits can be recorded against it.
const autosaveVersionWait = 30 * time.Second

// AutosaveStatus is the UI-facing state of the backup slot.
type AutosaveStatus struct {
	Enabled     bool   `json:"enabled"`
	Path        string `json:"path"`
	DefaultPath string `json:"defaultPath"`
	Exists      bool   `json:"exists"`
	CachedAt    int64  `json:"cachedAt"`
	SourcePath  string `json:"sourcePath"`
	SizeBytes   int64  `json:"sizeBytes"`
	Running     bool   `json:"running"`
	LastError   string `json:"lastError"`
}

// RecoveryInfo describes a backup that can be restored on startup.
type RecoveryInfo struct {
	SourcePath    string `json:"sourcePath"`
	SourceName    string `json:"sourceName"`
	CachedAt      int64  `json:"cachedAt"`
	SizeBytes     int64  `json:"sizeBytes"`
	PendingFiles  int    `json:"pendingFiles"`
	SourceExists  bool   `json:"sourceExists"`
	SourceChanged bool   `json:"sourceChanged"`
}

// autosaveMeta records the source file and the entries that were pending when
// the backup was written. Removed files are not listed: their paths are gone
// from the archive, so the restore derives them by comparing path sets.
type autosaveMeta struct {
	Version        int      `json:"version"`
	SourcePath     string   `json:"sourcePath"`
	CachedAt       int64    `json:"cachedAt"`
	SourceSize     int64    `json:"sourceSize"`
	SourceModified int64    `json:"sourceModified"`
	ModifiedPaths  []string `json:"modifiedPaths"`
	AddedPaths     []string `json:"addedPaths"`
}

// pendingPaths returns the modified and added entries in a stable order.
func (m autosaveMeta) pendingPaths() []string {
	seen := make(map[string]struct{}, len(m.ModifiedPaths)+len(m.AddedPaths))
	result := make([]string, 0, len(m.ModifiedPaths)+len(m.AddedPaths))
	for _, path := range append(append([]string{}, m.ModifiedPaths...), m.AddedPaths...) {
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

// AutosaveService keeps a single overwriting snapshot of the current workspace
// so an abnormal exit (crash, killed process) leaves something to recover. The
// snapshot never touches the live archive: it is written from an isolated clone
// and the workspace stays "unsaved" after the write.
type AutosaveService struct {
	c        *core
	settings *SettingsService

	running atomic.Bool

	mu      sync.Mutex
	lastErr string
}

// NewAutosaveService creates the backup service and wires it into the shared
// core so save/close handlers can drop a stale slot.
func NewAutosaveService(c *core, settings *SettingsService) *AutosaveService {
	service := &AutosaveService{c: c, settings: settings}
	if c != nil {
		c.attachAutosave(service)
	}
	return service
}

// Status reports the backup slot without writing anything.
func (s *AutosaveService) Status() AutosaveStatus {
	status := AutosaveStatus{Running: s.running.Load()}
	if s == nil || s.c == nil {
		return status
	}
	status.DefaultPath, _ = DefaultAutosavePath()
	if s.settings != nil {
		if settings, err := s.settings.GetSettings(); err == nil {
			status.Enabled = settings.AutosaveEnabled
		}
	}
	cachePath, err := s.cachePath()
	if err != nil {
		status.LastError = err.Error()
		return status
	}
	status.Path = cachePath
	if info, err := os.Stat(cachePath); err == nil {
		status.Exists = true
		status.SizeBytes = info.Size()
	}
	if meta, ok := readAutosaveMeta(cachePath); ok {
		status.CachedAt = meta.CachedAt
		status.SourcePath = meta.SourcePath
	}
	status.LastError = s.lastError()
	return status
}

// CreateSnapshot writes the pending edits to the backup slot. The live archive
// is left untouched: the write goes through an isolated clone, so the workspace
// keeps its unsaved state and the source file is never modified.
func (s *AutosaveService) CreateSnapshot() error {
	if s == nil || s.c == nil {
		return errors.New("定时缓存服务不可用")
	}
	if !s.running.CompareAndSwap(false, true) {
		return errors.New("定时缓存正在写入,请稍后再试")
	}
	defer s.running.Store(false)

	cachePath, err := s.cachePath()
	if err != nil {
		s.recordError(err)
		return err
	}

	s.c.mu.RLock()
	a := s.c.archive
	if a == nil {
		s.c.mu.RUnlock()
		s.recordError(ErrNoArchive)
		return ErrNoArchive
	}
	clone := a.CloneForBatch()
	sourcePath := a.SourcePath()
	edits := a.PendingEdits()
	s.c.mu.RUnlock()

	if !clone.Modified() {
		// Nothing pending (a save can win the race against the timer). Keep the
		// existing slot instead of overwriting it with a clean copy.
		s.recordError(nil)
		return nil
	}
	if sourcePath == "" {
		// An archive without a source file has nowhere to recover into, so a
		// backup would only produce a prompt that cannot be fulfilled.
		s.recordError(nil)
		return nil
	}

	meta := autosaveMeta{
		Version:    autosaveMetaVersion,
		SourcePath: sourcePath,
		CachedAt:   time.Now().Unix(),
	}
	for _, edit := range edits {
		path := pvfversion.DisplayPath(edit.Path)
		if path == "" {
			continue
		}
		if edit.Kind == pvf.MutationAdded {
			meta.AddedPaths = append(meta.AddedPaths, path)
			continue
		}
		meta.ModifiedPaths = append(meta.ModifiedPaths, path)
	}
	if len(meta.ModifiedPaths)+len(meta.AddedPaths) == 0 {
		// Only string-pool state differs (no entry the recovery could replay).
		// Writing a slot without restorable paths would just produce an empty
		// recovery prompt.
		s.recordError(nil)
		return nil
	}
	if info, statErr := os.Stat(sourcePath); statErr == nil {
		meta.SourceSize = info.Size()
		meta.SourceModified = info.ModTime().Unix()
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		s.recordError(err)
		return fmt.Errorf("创建备份缓存目录失败: %w", err)
	}
	// The payload is replaced first and the metadata second: a crash between the
	// two renames leaves an older, more conservative path list next to a newer
	// payload, which restores a subset of the edits instead of failing.
	if err := clone.SaveAs(cachePath); err != nil {
		s.recordError(err)
		return fmt.Errorf("写入备份缓存失败: %w", err)
	}
	if err := writeAutosaveMeta(cachePath, meta); err != nil {
		s.recordError(err)
		return err
	}

	s.recordError(nil)
	return nil
}

// PendingRecovery reports the backup left behind by an interrupted session, or
// nil when the slot is empty.
func (s *AutosaveService) PendingRecovery() (*RecoveryInfo, error) {
	if s == nil || s.c == nil {
		return nil, nil
	}
	cachePath, err := s.cachePath()
	if err != nil {
		return nil, err
	}
	meta, ok := readAutosaveMeta(cachePath)
	if !ok {
		return nil, nil
	}
	payload, err := os.Stat(cachePath)
	if err != nil {
		// Metadata without a payload is a torn write, not a usable backup.
		return nil, nil
	}
	info := &RecoveryInfo{
		SourcePath:   meta.SourcePath,
		CachedAt:     meta.CachedAt,
		SizeBytes:    payload.Size(),
		PendingFiles: len(meta.pendingPaths()),
	}
	if meta.SourcePath != "" {
		info.SourceName = filepath.Base(meta.SourcePath)
		if source, err := os.Stat(meta.SourcePath); err == nil {
			info.SourceExists = true
			info.SourceChanged = meta.SourceModified != 0 &&
				(source.ModTime().Unix() != meta.SourceModified || source.Size() != meta.SourceSize)
		}
	}
	return info, nil
}

// Restore reopens the recorded source file and replays the backed-up edits into
// the in-memory overlay, so the workspace ends up dirty exactly like the
// session that was interrupted: modified markers, version changes and the save
// target all stay consistent. The backup itself is kept until the next save or
// an explicit discard.
func (s *AutosaveService) Restore() (ArchiveInfo, error) {
	if s == nil || s.c == nil {
		return ArchiveInfo{}, errors.New("定时缓存服务不可用")
	}
	cachePath, err := s.cachePath()
	if err != nil {
		return ArchiveInfo{}, err
	}
	meta, ok := readAutosaveMeta(cachePath)
	if !ok {
		return ArchiveInfo{}, errors.New("没有可恢复的备份缓存")
	}
	if meta.SourcePath == "" {
		return ArchiveInfo{}, errors.New("备份缺少原文件路径,无法恢复")
	}
	// A clean workspace can be replaced, but unsaved edits of another archive
	// must never be dropped by a recovery the user did not aim at them.
	s.c.mu.RLock()
	dirty := s.c.archive != nil &&
		(s.c.archive.ModifiedCount() > 0 || len(s.c.versionArtifactDirty) > 0)
	s.c.mu.RUnlock()
	if dirty {
		return ArchiveInfo{}, errors.New("当前工作区有未保存的修改,请先保存或关闭后再恢复备份")
	}
	if _, err := os.Stat(meta.SourcePath); err != nil {
		return ArchiveInfo{}, fmt.Errorf("原文件不存在,请先找回文件再恢复备份: %s", meta.SourcePath)
	}

	info, err := s.c.openArchive(meta.SourcePath)
	if err != nil {
		return ArchiveInfo{}, fmt.Errorf("打开原文件失败: %w", err)
	}
	if err := s.waitForVersionSession(); err != nil {
		return ArchiveInfo{}, err
	}
	// Paged110 containers keep their page keys next to the original file, so the
	// backup borrows the sidecar directory instead of carrying a copy.
	backup, err := pvf.OpenWithSidecars(cachePath, filepath.Dir(meta.SourcePath))
	if err != nil {
		return ArchiveInfo{}, fmt.Errorf("打开备份缓存失败: %w", err)
	}

	a, summary, err := s.replayBackup(meta, backup)
	if err != nil {
		return ArchiveInfo{}, err
	}

	s.c.mu.RLock()
	info, _ = s.c.archiveInfo()
	s.c.mu.RUnlock()
	s.c.scheduleArchiveMutations(a, summary)
	emitEvent("archive:reloaded", info)
	emitVersionState(s.c, "recovered")
	return info, nil
}

// Discard deletes the backup slot. It reports whether a cache file was removed.
func (s *AutosaveService) Discard() (bool, error) {
	if s == nil || s.c == nil {
		return false, errors.New("定时缓存服务不可用")
	}
	cachePath, err := s.cachePath()
	if err != nil {
		return false, err
	}
	return deleteAutosaveFiles(cachePath)
}

// DropForSource deletes the backup only when it belongs to sourcePath. Saving or
// closing another archive must never destroy a backup that still protects a
// different workspace.
func (s *AutosaveService) DropForSource(sourcePath string) {
	if s == nil || s.c == nil {
		return
	}
	cachePath, err := s.cachePath()
	if err != nil {
		return
	}
	meta, ok := readAutosaveMeta(cachePath)
	if !ok {
		return
	}
	if !sameSourcePath(meta.SourcePath, sourcePath) {
		return
	}
	_, _ = deleteAutosaveFiles(cachePath)
}

// DropForWorkspace drops the backup of the currently open workspace. It is the
// exit path's "the user gave up on these edits" cleanup.
func (s *AutosaveService) DropForWorkspace() {
	if s == nil || s.c == nil {
		return
	}
	s.c.mu.RLock()
	sourcePath := ""
	if s.c.archive != nil {
		sourcePath = s.c.archive.SourcePath()
	}
	s.c.mu.RUnlock()
	s.DropForSource(sourcePath)
}

// ChooseCachePath asks the user for a backup file location.
func (s *AutosaveService) ChooseCachePath() (string, error) {
	if s == nil {
		return "", errors.New("定时缓存服务不可用")
	}
	name := "autosave.pvf"
	if current, err := s.cachePath(); err == nil && current != "" {
		name = filepath.Base(current)
	}
	path, err := application.Get().Dialog.SaveFile().
		SetFilename(name).
		AddFilter("PVF 备份缓存", "*.pvf").
		SetMessage("选择定时缓存文件").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return path, nil // 空字符串表示用户取消
}

// replayBackup applies the backed-up contents on top of the freshly opened
// source archive. It returns the archive and the mutation summary so the caller
// can refresh the derived indexes.
func (s *AutosaveService) replayBackup(meta autosaveMeta, backup *pvf.Archive) (*pvf.Archive, pvf.MutationSummary, error) {
	backupPaths := make(map[string]struct{}, backup.FileCount())
	for i := int32(0); i < backup.FileCount(); i++ {
		if key := pvfversion.CanonicalPath(backup.Path(i)); key != "" {
			backupPaths[key] = struct{}{}
		}
	}
	recovered := make(map[string]struct{})
	for _, path := range meta.pendingPaths() {
		key := pvfversion.CanonicalPath(path)
		if key == "" {
			continue
		}
		// A pending entry the backup does not contain was deleted before the
		// snapshot was taken; the path-set comparison below handles it.
		if _, exists := backupPaths[key]; exists {
			recovered[key] = struct{}{}
		}
	}

	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.archive == nil {
		return nil, pvf.MutationSummary{}, ErrNoArchive
	}
	a := s.c.archive
	// Entries the interrupted session deleted are present in the file but
	// missing from the backup.
	paths := make([]string, 0, len(recovered)+8)
	for key := range recovered {
		paths = append(paths, key)
	}
	for i := int32(0); i < a.FileCount(); i++ {
		key := pvfversion.CanonicalPath(a.Path(i))
		if key == "" {
			continue
		}
		if _, exists := backupPaths[key]; !exists {
			paths = append(paths, key)
		}
	}
	if len(paths) == 0 {
		return a, pvf.MutationSummary{}, nil
	}
	sort.Strings(paths)

	before, err := pvfversion.ContentSnapshotFromArchive(a, paths)
	if err != nil {
		return nil, pvf.MutationSummary{}, err
	}
	desired, err := pvfversion.ContentSnapshotFromArchive(backup, paths)
	if err != nil {
		return nil, pvf.MutationSummary{}, err
	}
	// Paths the session never touched keep the file's current content: only the
	// recovered and the deleted entries are materialized. The version working
	// snapshot is deliberately not consulted, so a recovered payload is never
	// skipped on account of another snapshot agreeing with it.
	for key := range desired {
		if _, exists := recovered[key]; !exists {
			delete(desired, key)
		}
	}
	if err := applyContentPathsLocked(s.c, paths, desired, false); err != nil {
		return nil, pvf.MutationSummary{}, err
	}
	after, err := pvfversion.ContentSnapshotFromArchive(a, paths)
	if err != nil {
		return nil, pvf.MutationSummary{}, err
	}
	if err := s.c.recordVersionMutationLocked("从定时缓存恢复", before, after); err != nil {
		return nil, pvf.MutationSummary{}, err
	}
	summary := a.MutationsSince(0)
	a.ClearMutations()
	return a, summary, nil
}

// waitForVersionSession waits until the background version load settles. The
// replayed edits have to be recorded against an attached session; applying them
// earlier would leave the version panel reporting a clean workspace.
func (s *AutosaveService) waitForVersionSession() error {
	deadline := time.Now().Add(autosaveVersionWait)
	for {
		s.c.mu.RLock()
		loading := s.c.versionLoading
		s.c.mu.RUnlock()
		if !loading {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("版本库仍在后台加载,请稍后重试恢复")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// cachePath resolves the configured snapshot file, falling back to the
// platform cache directory.
func (s *AutosaveService) cachePath() (string, error) {
	if s == nil || s.settings == nil {
		return "", errors.New("定时缓存服务不可用")
	}
	settings, err := s.settings.GetSettings()
	if err != nil {
		return "", err
	}
	if settings.AutosavePath != "" {
		return settings.AutosavePath, nil
	}
	return DefaultAutosavePath()
}

func (s *AutosaveService) recordError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.lastErr = ""
		return
	}
	s.lastErr = err.Error()
}

func (s *AutosaveService) lastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

func readAutosaveMeta(cachePath string) (autosaveMeta, bool) {
	data, err := os.ReadFile(cachePath + ".json")
	if err != nil {
		return autosaveMeta{}, false
	}
	var meta autosaveMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return autosaveMeta{}, false
	}
	if meta.Version != autosaveMetaVersion {
		return autosaveMeta{}, false
	}
	return meta, true
}

func writeAutosaveMeta(cachePath string, meta autosaveMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	metaPath := cachePath + ".json"
	temp, err := os.CreateTemp(filepath.Dir(metaPath), ".autosave-*.tmp")
	if err != nil {
		return fmt.Errorf("创建备份元数据临时文件失败: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("写入备份元数据失败: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("同步备份元数据失败: %w", err)
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, metaPath); err != nil {
		return fmt.Errorf("替换备份元数据失败: %w", err)
	}
	return nil
}

// deleteAutosaveFiles removes the payload and its metadata. Missing files are
// not an error: the caller only cares that no backup is left.
func deleteAutosaveFiles(cachePath string) (bool, error) {
	removed := false
	if err := os.Remove(cachePath); err == nil {
		removed = true
	} else if !os.IsNotExist(err) {
		return removed, fmt.Errorf("删除备份缓存失败: %w", err)
	}
	if err := os.Remove(cachePath + ".json"); err == nil {
		removed = true
	} else if !os.IsNotExist(err) {
		return removed, fmt.Errorf("删除备份元数据失败: %w", err)
	}
	return removed, nil
}

// sameSourcePath compares two archive paths, tolerating separator and case
// differences reported by different platforms.
func sameSourcePath(left, right string) bool {
	if left == "" || right == "" {
		return left == right
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

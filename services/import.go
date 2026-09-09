package services

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wailsapp/wails/v3/pkg/application"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

const (
	ImportModeText          = "text"
	ImportModeRaw           = "raw"
	maxImportPreviewEntries = 500
)

// ImportResult describes one atomically applied import operation.
type ImportResult struct {
	TargetDir        string   `json:"targetDir"`
	Mode             string   `json:"mode"`
	ImportedCount    int      `json:"importedCount"`
	OverwrittenCount int      `json:"overwrittenCount"`
	ChangedPaths     []string `json:"changedPaths,omitempty"`
}

// ImportPreview describes the validated changes without modifying the live
// archive. Entries is capped for large directory imports; the counters remain
// complete.
type ImportPreview struct {
	TargetDir        string                `json:"targetDir"`
	Mode             string                `json:"mode"`
	TotalFiles       int                   `json:"totalFiles"`
	ImportedCount    int                   `json:"importedCount"`
	OverwrittenCount int                   `json:"overwrittenCount"`
	Entries          []*ImportPreviewEntry `json:"entries"`
	EntriesTruncated bool                  `json:"entriesTruncated"`
}

// ImportPreviewEntry is one validated source-to-archive mapping.
type ImportPreviewEntry struct {
	SourcePath string `json:"sourcePath"`
	TargetPath string `json:"targetPath"`
	DataType   int32  `json:"dataType"`
	Size       int64  `json:"size"`
	Overwrite  bool   `json:"overwrite"`
}

type importFile struct {
	sourcePath string
	targetPath string
	data       []byte
	text       string
	dataType   int32
}

type importSelection struct {
	path  string
	isDir bool
}

// ImportFilesDialog opens a native multi-selection picker and imports the
// selected files or directories into targetDir.
func (s *ArchiveService) ImportFilesDialog(targetDir, mode string) (*ImportResult, error) {
	paths, err := s.SelectImportFilesDialog()
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	return s.ImportFiles(paths, targetDir, mode)
}

// SelectImportFilesDialog opens the native multi-selection picker without
// changing the current archive. The returned paths can be previewed first.
func (s *ArchiveService) SelectImportFilesDialog() ([]string, error) {
	return application.Get().Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(true).
		AddFilter("所有文件", "*").
		SetTitle("导入文件").
		PromptForMultipleSelection()
}

// PreviewImport validates and maps sourcePaths without modifying the live
// archive. It also stages the files on an isolated archive to catch encoding
// and index-building errors before the user confirms the operation.
func (s *ArchiveService) PreviewImport(sourcePaths []string, targetDir, mode string) (*ImportPreview, error) {
	files, targetDir, mode, err := prepareImportFiles(sourcePaths, targetDir, mode)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errorsNoImportFiles()
	}

	s.c.mu.RLock()
	a := s.c.archive
	if a == nil {
		s.c.mu.RUnlock()
		return nil, ErrNoArchive
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.RUnlock()
		return nil, err
	}

	preview := &ImportPreview{
		TargetDir:  targetDir,
		Mode:       mode,
		TotalFiles: len(files),
		Entries:    []*ImportPreviewEntry{},
	}
	for _, file := range files {
		_, overwrite := a.Find(file.targetPath)
		if overwrite {
			preview.OverwrittenCount++
		} else {
			preview.ImportedCount++
		}
		if len(preview.Entries) < maxImportPreviewEntries {
			preview.Entries = append(preview.Entries, &ImportPreviewEntry{
				SourcePath: file.sourcePath,
				TargetPath: file.targetPath,
				DataType:   file.dataType,
				Size:       int64(len(file.data)),
				Overwrite:  overwrite,
			})
		} else {
			preview.EntriesTruncated = true
		}
	}

	stage := a.CloneForBatch()
	if _, err := applyPreparedImport(stage, files, mode); err != nil {
		s.c.mu.RUnlock()
		return nil, err
	}
	if _, _, err := buildIndex(stage); err != nil {
		s.c.mu.RUnlock()
		return nil, err
	}
	if s.c.versionRepo != nil {
		targetPaths := make([]string, 0, len(files))
		for _, file := range files {
			targetPaths = append(targetPaths, file.targetPath)
		}
		if _, err := pvfversion.ContentSnapshotFromArchive(stage, targetPaths); err != nil {
			s.c.mu.RUnlock()
			return nil, err
		}
	}
	s.c.mu.RUnlock()
	return preview, nil
}

// ImportFiles imports sourcePaths into targetDir. The source paths may be
// files or directories. All source data is read and staged before the live
// archive is replaced, so an error leaves the current archive unchanged.
func (s *ArchiveService) ImportFiles(sourcePaths []string, targetDir, mode string) (*ImportResult, error) {
	files, targetDir, mode, err := prepareImportFiles(sourcePaths, targetDir, mode)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errorsNoImportFiles()
	}

	s.c.mu.Lock()
	a := s.c.archive
	if a == nil {
		s.c.mu.Unlock()
		return nil, ErrNoArchive
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}

	targetPaths := make([]string, 0, len(files))
	for _, file := range files {
		targetPaths = append(targetPaths, file.targetPath)
	}

	var before pvfversion.ContentSnapshot
	if s.c.versionRepo != nil {
		before, err = pvfversion.ContentSnapshotFromArchive(a, targetPaths)
		if err != nil {
			s.c.mu.Unlock()
			return nil, err
		}
	}

	stage := a.CloneForBatch()
	result, err := applyPreparedImport(stage, files, mode)
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	result.TargetDir = targetDir
	result.Mode = mode

	children, paths, err := buildIndex(stage)
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if s.c.versionRepo != nil {
		after, snapshotErr := pvfversion.ContentSnapshotFromArchive(stage, targetPaths)
		if snapshotErr != nil {
			s.c.mu.Unlock()
			return nil, snapshotErr
		}
		if err := s.c.recordVersionMutationLocked("导入文件", before, after); err != nil {
			s.c.mu.Unlock()
			return nil, err
		}
	}

	s.c.installArchiveIndexesLocked(stage, children, paths)
	info := stage.Info()
	versioned := s.c.versionRepo != nil
	s.c.mu.Unlock()

	s.c.startSearchIndex()
	emitEvent("archive:changed", info)
	emitEvent("archive:advanced-search-stale", map[string]any{"import": true})
	if versioned {
		emitVersionState(s.c, "files-imported")
	}
	return result, nil
}

func errorsNoImportFiles() error {
	return fmt.Errorf("没有可导入的文件")
}

func prepareImportFiles(sourcePaths []string, targetDir, mode string) ([]importFile, string, string, error) {
	var err error
	targetDir, err = normalizeImportTargetDir(targetDir)
	if err != nil {
		return nil, "", "", err
	}
	mode, err = normalizeImportMode(mode)
	if err != nil {
		return nil, "", "", err
	}
	files, err := collectImportFiles(sourcePaths, targetDir)
	if err != nil {
		return nil, "", "", err
	}
	if len(files) == 0 {
		return files, targetDir, mode, nil
	}
	if err := readImportFiles(files, mode); err != nil {
		return nil, "", "", err
	}
	return files, targetDir, mode, nil
}

func normalizeImportMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = ImportModeText
	}
	if mode != ImportModeText && mode != ImportModeRaw {
		return "", fmt.Errorf("不支持的导入模式: %q", mode)
	}
	return mode, nil
}

func normalizeImportTargetDir(raw string) (string, error) {
	dir := strings.Trim(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"), "/")
	if dir == "" {
		return "", nil
	}
	return normalizeNewFilePath(dir)
}

func collectImportFiles(sourcePaths []string, targetDir string) ([]importFile, error) {
	selections := make([]importSelection, 0, len(sourcePaths))
	seenSelections := make(map[string]struct{}, len(sourcePaths))
	for _, rawPath := range sourcePaths {
		rawPath = strings.TrimSpace(rawPath)
		if rawPath == "" {
			continue
		}
		path, err := filepath.Abs(filepath.Clean(rawPath))
		if err != nil {
			return nil, fmt.Errorf("解析来源路径 %q 失败: %w", rawPath, err)
		}
		if _, exists := seenSelections[path]; exists {
			continue
		}
		seenSelections[path] = struct{}{}

		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("读取来源路径 %q 失败: %w", rawPath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("不支持符号链接来源: %q", rawPath)
		}
		if info.IsDir() {
			selections = append(selections, importSelection{path: path, isDir: true})
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("来源不是普通文件或目录: %q", rawPath)
		}
		selections = append(selections, importSelection{path: path})
	}

	sort.Slice(selections, func(i, j int) bool {
		return selections[i].path < selections[j].path
	})
	selectedDirs := make([]string, 0)
	filtered := selections[:0]
	for _, selection := range selections {
		redundant := false
		for _, dir := range selectedDirs {
			if selection.path != dir && isPathWithin(dir, selection.path) {
				redundant = true
				break
			}
		}
		if redundant {
			continue
		}
		filtered = append(filtered, selection)
		if selection.isDir {
			selectedDirs = append(selectedDirs, selection.path)
		}
	}

	files := make([]importFile, 0)
	seenSources := make(map[string]struct{})
	appendFile := func(sourcePath, relativePath string) error {
		if _, exists := seenSources[sourcePath]; exists {
			return nil
		}
		seenSources[sourcePath] = struct{}{}
		targetPath := targetDir
		if targetPath != "" {
			targetPath += "/"
		}
		targetPath += filepath.ToSlash(relativePath)
		targetPath, err := normalizeNewFilePath(targetPath)
		if err != nil {
			return fmt.Errorf("来源 %q 映射到归档路径失败: %w", sourcePath, err)
		}
		files = append(files, importFile{sourcePath: sourcePath, targetPath: targetPath})
		return nil
	}

	for _, selection := range filtered {
		if !selection.isDir {
			if err := appendFile(selection.path, filepath.Base(selection.path)); err != nil {
				return nil, err
			}
			continue
		}

		directoryName := filepath.Base(selection.path)
		err := filepath.WalkDir(selection.path, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			relative, relErr := filepath.Rel(selection.path, path)
			if relErr != nil {
				return relErr
			}
			return appendFile(path, filepath.Join(directoryName, relative))
		})
		if err != nil {
			return nil, fmt.Errorf("扫描目录 %q 失败: %w", selection.path, err)
		}
	}

	if len(files) == 0 {
		return []importFile{}, nil
	}
	sort.Slice(files, func(i, j int) bool {
		left := strings.ToLower(strings.ReplaceAll(files[i].targetPath, "\\", "/"))
		right := strings.ToLower(strings.ReplaceAll(files[j].targetPath, "\\", "/"))
		if left != right {
			return left < right
		}
		return files[i].sourcePath < files[j].sourcePath
	})
	seenTargets := make(map[string]string, len(files))
	for _, file := range files {
		key := strings.ToLower(file.targetPath)
		if previous, exists := seenTargets[key]; exists {
			return nil, fmt.Errorf("批次内归档路径冲突: %q 与 %q 都映射到 %q", previous, file.sourcePath, file.targetPath)
		}
		seenTargets[key] = file.sourcePath
	}
	return files, nil
}

func isPathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) {
		return false
	}
	prefix := ".." + string(filepath.Separator)
	return !strings.HasPrefix(relative, prefix)
}

func readImportFiles(files []importFile, mode string) error {
	for index := range files {
		file := &files[index]
		data, err := os.ReadFile(file.sourcePath)
		if err != nil {
			return fmt.Errorf("读取 %q 失败: %w", file.sourcePath, err)
		}
		file.data = data
		file.dataType = importDataType(file.targetPath)
		if mode == ImportModeText {
			if !utf8.Valid(data) {
				return fmt.Errorf("文本文件 %q 不是有效的 UTF-8", file.sourcePath)
			}
			file.text = strings.TrimPrefix(string(data), "\uFEFF")
		}
	}
	return nil
}

func importDataType(path string) int32 {
	if strings.EqualFold(filepath.Ext(path), ".str") {
		return pvf.TypeUnicode
	}
	return pvf.TypeScript
}

func applyImportFile(a *pvf.Archive, file importFile, mode string, index int32) error {
	if mode == ImportModeText {
		return a.SetText(index, file.text)
	}
	return a.SetRawBytes(index, file.data)
}

func addImportFile(a *pvf.Archive, file importFile, mode string) error {
	if mode == ImportModeText {
		_, err := a.AddFileText(file.targetPath, file.text, file.dataType)
		return err
	}
	a.AddFile(file.targetPath, file.data, file.dataType)
	return nil
}

func applyPreparedImport(a *pvf.Archive, files []importFile, mode string) (*ImportResult, error) {
	result := &ImportResult{ChangedPaths: []string{}}
	for _, file := range files {
		index, exists := a.Find(file.targetPath)
		if exists {
			if err := a.SetDataType(index, file.dataType); err != nil {
				return nil, fmt.Errorf("设置 %q 类型失败: %w", file.targetPath, err)
			}
			if err := applyImportFile(a, file, mode, index); err != nil {
				return nil, fmt.Errorf("导入 %q 到 %q 失败: %w", file.sourcePath, file.targetPath, err)
			}
			result.OverwrittenCount++
			result.ChangedPaths = append(result.ChangedPaths, a.Path(index))
			continue
		}

		if err := addImportFile(a, file, mode); err != nil {
			return nil, fmt.Errorf("导入 %q 到 %q 失败: %w", file.sourcePath, file.targetPath, err)
		}
		result.ImportedCount++
	}
	return result, nil
}

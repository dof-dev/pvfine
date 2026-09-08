package services

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"pvfine/internal/pvf"
)

// maxEditableBytes:超过该大小的文件仅提供只读占位,避免编辑器载入超大文本。
const maxEditableBytes = 8 << 20 // 8MB

// EditorService: 文件内容读取、内存编辑、保存/另存为、导出与整包解包。
type EditorService struct {
	c        *core
	settings *SettingsService
}

func NewEditorService(c *core, settings ...*SettingsService) *EditorService {
	service := &EditorService{c: c}
	if len(settings) > 0 {
		service.settings = settings[0]
	}
	return service
}

// FileMeta 返回给前端的单个文件视图。
type FileMeta struct {
	Index       int32              `json:"index"`
	Path        string             `json:"path"`
	DataType    int32              `json:"dataType"`
	Size        int32              `json:"size"`
	Editable    bool               `json:"editable"`
	Text        string             `json:"text"`
	Modified    bool               `json:"modified"`
	Annotations []EditorAnnotation `json:"annotations,omitempty"`
}

// GetFile 返回文件的反编译文本(内存编辑视图)。
func (s *EditorService) GetFile(index int32) (*FileMeta, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	if err := validateAnnotationIndex(a, index); err != nil {
		return nil, err
	}
	f := a.File(index)
	meta := &FileMeta{
		Index:    index,
		Path:     a.Path(index),
		DataType: f.DataType,
		Size:     f.DataSize,
		Editable: false,
	}
	switch f.DataType {
	case pvf.TypeScript, pvf.TypeUnicode:
		if f.DataSize > maxEditableBytes {
			meta.Text = fmt.Sprintf("; 文件过大(%d 字节),超过文本编辑上限 %d 字节", f.DataSize, maxEditableBytes)
			return meta, nil
		}
		text, ok := s.c.editorText[index]
		if !ok {
			var err error
			text, err = a.Text(index)
			if err != nil {
				return nil, err
			}
		}
		meta.Editable = true
		meta.Text = text
		if f.DataType == pvf.TypeScript {
			annotations, err := s.c.editorAnnotationsLocked(index, text)
			if err != nil {
				return nil, err
			}
			meta.Annotations = annotations
		}
	default:
		meta.Text = fmt.Sprintf("; 不支持的类型 %d(v1 仅支持脚本/文本编辑)", f.DataType)
	}
	meta.Modified = a.IsModified(index)
	return meta, nil
}

// GetAnnotations recomputes annotations against the exact current editor text.
func (s *EditorService) GetAnnotations(index int32) ([]EditorAnnotation, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	if err := validateAnnotationIndex(s.c.archive, index); err != nil {
		return nil, err
	}
	if s.c.archive.File(index).DataType != pvf.TypeScript {
		return []EditorAnnotation{}, nil
	}
	text, ok := s.c.editorText[index]
	if !ok {
		var err error
		text, err = s.c.archive.Text(index)
		if err != nil {
			return nil, err
		}
	}
	return s.c.editorAnnotationsLocked(index, text)
}

// SetText 把编辑后的文本写入内存 overlay(不落盘)。
func (s *EditorService) SetText(index int32, text string) error {
	_, _, err := s.c.setText(index, text)
	return err
}

// Save 把全部内存修改写回源文件(原子写:临时文件 + rename)。
func (s *EditorService) Save() (ArchiveInfo, error) {
	s.c.mu.Lock()
	a := s.c.archive
	if a == nil {
		s.c.mu.Unlock()
		return ArchiveInfo{}, ErrNoArchive
	}
	if a.SourcePath() == "" {
		s.c.mu.Unlock()
		return ArchiveInfo{}, fmt.Errorf("归档没有源文件,请使用另存为")
	}
	if s.shouldBackupSource() {
		if err := backupSourceFile(a.SourcePath()); err != nil {
			s.c.mu.Unlock()
			return ArchiveInfo{}, err
		}
	}
	if err := a.Save(); err != nil {
		s.c.mu.Unlock()
		return ArchiveInfo{}, err
	}
	info := a.Info()
	s.c.mu.Unlock()
	emitEvent("archive:saved", info)
	return info, nil
}

func (s *EditorService) shouldBackupSource() bool {
	if s.settings == nil {
		return true
	}
	settings, err := s.settings.GetSettings()
	if err != nil {
		// 读取设置失败时使用安全默认值,不要因为配置文件问题阻止保存。
		return true
	}
	return settings.BackupSourceOnSave
}

func backupSourceFile(sourcePath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("打开源文件以创建备份失败: %w", err)
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return fmt.Errorf("读取源文件信息失败: %w", err)
	}

	backupPath := sourcePath + ".bak"
	temp, err := os.CreateTemp(filepath.Dir(backupPath), "."+filepath.Base(backupPath)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("创建源文件备份临时文件失败: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("设置源文件备份权限失败: %w", err)
	}
	if _, err := io.CopyBuffer(temp, source, make([]byte, 1<<20)); err != nil {
		_ = temp.Close()
		return fmt.Errorf("写入源文件备份失败: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("同步源文件备份失败: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("关闭源文件备份失败: %w", err)
	}
	if err := os.Rename(tempPath, backupPath); err != nil {
		return fmt.Errorf("替换源文件备份失败: %w", err)
	}
	return nil
}

// SaveAsDialog 弹出保存对话框并另存为新 PVF。返回保存路径。
func (s *EditorService) SaveAsDialog() (string, error) {
	s.c.mu.RLock()
	a := s.c.archive
	if a == nil {
		s.c.mu.RUnlock()
		return "", ErrNoArchive
	}
	s.c.mu.RUnlock()
	defName := "Script_new.pvf"
	if src := a.SourcePath(); src != "" {
		defName = filepath.Base(src)
	}
	path, err := application.Get().Dialog.SaveFile().
		SetFilename(defName).
		AddFilter("PVF 归档", "*.pvf").
		SetMessage("另存为 PVF").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil // 用户取消
	}
	s.c.mu.Lock()
	if s.c.archive != a {
		s.c.mu.Unlock()
		return "", ErrNoArchive
	}
	if err := a.SaveAs(path); err != nil {
		s.c.mu.Unlock()
		return "", err
	}
	info := a.Info()
	s.c.mu.Unlock()
	emitEvent("archive:saved", info)
	return path, nil
}

type exportSelection struct {
	index int32
	path  string
}

// ExportFilesDialog 将选中的文件或目录导出到目标目录,文件内容使用渲染后的 UTF-8 文本。
// 目录会递归展开,并保留归档内的相对路径。
func (s *EditorService) ExportFilesDialog(scopes []string) (string, error) {
	s.c.mu.RLock()
	a := s.c.archive
	if a == nil {
		s.c.mu.RUnlock()
		return "", ErrNoArchive
	}
	selections := collectExportSelections(a, s.c.sortedPaths, scopes)
	s.c.mu.RUnlock()
	if len(selections) == 0 {
		return "", fmt.Errorf("没有可导出的文件")
	}
	dir, err := application.Get().Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(true).
		SetTitle("选择导出目标目录").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if s.c.archive != a {
		return "", ErrNoArchive
	}
	for _, selection := range selections {
		text, err := a.Text(selection.index)
		if err != nil {
			return "", fmt.Errorf("渲染 %q 失败: %w", selection.path, err)
		}
		dst, err := safeExportPath(dir, selection.path)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, []byte(text), 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func collectExportSelections(a *pvf.Archive, sortedPaths []pathEntry, scopes []string) []exportSelection {
	selections := make([]exportSelection, 0, len(scopes))
	seenPaths := make(map[string]struct{}, len(scopes))
	add := func(index int32, path string) {
		path = normalizeExportPath(path)
		if path == "" {
			return
		}
		if _, exists := seenPaths[path]; exists {
			return
		}
		seenPaths[path] = struct{}{}
		selections = append(selections, exportSelection{index: index, path: path})
	}

	for _, rawScope := range scopes {
		scope := normalizeExportPath(rawScope)
		if scope == "" {
			continue
		}
		if index, ok := a.Find(scope); ok {
			add(index, a.Path(index))
			continue
		}

		prefix := scope + "/"
		start := sort.Search(len(sortedPaths), func(i int) bool {
			return sortedPaths[i].path >= prefix
		})
		for _, entry := range sortedPaths[start:] {
			if !strings.HasPrefix(entry.path, prefix) {
				break
			}
			add(entry.idx, entry.path)
		}
	}
	return selections
}

func normalizeExportPath(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	return strings.Trim(path, "/")
}

func safeExportPath(base, internal string) (string, error) {
	parts := strings.Split(strings.ReplaceAll(internal, "\\", "/"), "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("导出路径越界: %q", internal)
		}
		cleaned = append(cleaned, sanitizeExportName(part))
	}
	if len(cleaned) == 0 {
		return "", fmt.Errorf("导出路径为空: %q", internal)
	}
	return filepath.Join(append([]string{base}, cleaned...)...), nil
}

func sanitizeExportName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f:
			return '_'
		case strings.ContainsRune(`<>:"|?*`, r):
			return '_'
		default:
			return r
		}
	}, name)
}

// UnpackDialog 选择目录后,在后台协程把整包解包到该目录。
// 进度通过事件 "unpack:progress" {done,total} 推送,结束发 "unpack:done"。
func (s *EditorService) UnpackDialog() (bool, error) {
	if !s.c.unpackRunning.CompareAndSwap(false, true) {
		return false, fmt.Errorf("解包正在进行中")
	}
	s.c.mu.RLock()
	a := s.c.archive
	s.c.mu.RUnlock()
	if a == nil {
		s.c.unpackRunning.Store(false)
		return false, ErrNoArchive
	}
	dir, err := application.Get().Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(true).
		SetTitle("选择解包目标目录").
		PromptForSingleSelection()
	if err != nil {
		s.c.unpackRunning.Store(false)
		return false, err
	}
	if dir == "" {
		s.c.unpackRunning.Store(false)
		return false, nil // 用户取消
	}

	s.c.unpackCancel.Store(false)
	go func() {
		defer s.c.unpackRunning.Store(false)
		emit := application.Get().Event.Emit
		total := int(a.FileCount())
		progress := func(done, tot int) { emit("unpack:progress", map[string]int{"done": done, "total": tot}) }
		cancel := func() bool { return s.c.unpackCancel.Load() }

		err := a.ExtractTo(dir, progress, cancel)
		if err == pvf.ErrCancelled {
			emit("unpack:done", map[string]any{"ok": false, "message": "解包已取消", "dir": dir})
			return
		}
		if err != nil {
			emit("unpack:done", map[string]any{"ok": false, "message": "解包失败: " + err.Error(), "dir": dir})
			return
		}
		emit("unpack:done", map[string]any{"ok": true, "message": fmt.Sprintf("已解包 %d 个文件到 %s", total, dir), "dir": dir})
	}()
	return true, nil
}

// CancelUnpack 请求中止进行中的解包。
func (s *EditorService) CancelUnpack() {
	s.c.unpackCancel.Store(true)
}

// IsUnpacking 报告解包是否进行中。
func (s *EditorService) IsUnpacking() bool { return s.c.unpackRunning.Load() }

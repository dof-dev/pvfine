package services

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"pvfine/internal/pvf"
)

// maxEditableBytes:超过该大小的文件仅提供只读占位,避免编辑器载入超大文本。
const maxEditableBytes = 8 << 20 // 8MB

// EditorService: 文件内容读取、内存编辑、保存/另存为、导出与整包解包。
type EditorService struct{ c *core }

func NewEditorService(c *core) *EditorService { return &EditorService{c: c} }

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
	if err := a.Save(); err != nil {
		s.c.mu.Unlock()
		return ArchiveInfo{}, err
	}
	info := a.Info()
	s.c.mu.Unlock()
	emitEvent("archive:saved", info)
	return info, nil
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

// ExportFileDialog 把单个文件导出到磁盘(保留原始字节)。
func (s *EditorService) ExportFileDialog(index int32) (string, error) {
	s.c.mu.RLock()
	a := s.c.archive
	if a == nil {
		s.c.mu.RUnlock()
		return "", ErrNoArchive
	}
	s.c.mu.RUnlock()
	if index < 0 || index >= a.FileCount() {
		return "", fmt.Errorf("文件索引越界: %d", index)
	}
	defName := filepath.Base(a.Path(index))
	path, err := application.Get().Dialog.SaveFile().
		SetFilename(defName).
		SetMessage("导出文件").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	s.c.mu.RLock()
	if s.c.archive != a {
		s.c.mu.RUnlock()
		return "", ErrNoArchive
	}
	raw, err := a.RawBytes(index)
	data := append([]byte(nil), raw...)
	s.c.mu.RUnlock()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
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

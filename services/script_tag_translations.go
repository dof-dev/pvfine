package services

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"sync"
)

// ScriptTagTranslation is one section -> comment mapping loaded from the
// read-only DCN86 script tag translation file. Only the section and comment
// fields are kept; every other attribute is intentionally ignored.
type ScriptTagTranslation struct {
	Section string `json:"section"`
	Comment string `json:"comment"`
}

// scriptTagItem mirrors a single <item> element of the translation document.
type scriptTagItem struct {
	CommentType string `xml:"commentType,attr"`
	Section     string `xml:"section,attr"`
	Comment     string `xml:"comment,attr"`
}

type scriptTagDocument struct {
	Items []scriptTagItem `xml:"item"`
}

// ScriptTagTranslationService loads the read-only PVF tag translation file
// (dcn86.xml) and serves hover annotations for [section] tags in the editor.
type ScriptTagTranslationService struct {
	mu        sync.Mutex
	path      string
	items     []ScriptTagTranslation
	bySection map[string]string
	loaded    bool
	initErr   error
}

const defaultScriptTagTranslationPath = `E:\DOF\PVFUT新\Options\ScriptTagTranslations\dcn86.xml`

func NewScriptTagTranslationService() *ScriptTagTranslationService {
	return &ScriptTagTranslationService{
		path: defaultScriptTagTranslationPath,
	}
}

// SetTranslationPath overrides the read-only source file. It is used by tests
// and is a no-op once the file has been loaded.
func (s *ScriptTagTranslationService) SetTranslationPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return
	}
	s.path = path
}

// SourcePath reports the configured read-only source file.
func (s *ScriptTagTranslationService) SourcePath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
}

func (s *ScriptTagTranslationService) loadLocked() error {
	if s.loaded {
		return s.initErr
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		s.loaded = true
		s.initErr = fmt.Errorf("读取标签翻译文件失败: %w", err)
		return s.initErr
	}
	var doc scriptTagDocument
	if err := xml.Unmarshal(data, &doc); err != nil {
		s.loaded = true
		s.initErr = fmt.Errorf("解析标签翻译文件失败: %w", err)
		return s.initErr
	}
	items := make([]ScriptTagTranslation, 0, len(doc.Items))
	bySection := make(map[string]string, len(doc.Items))
	for _, item := range doc.Items {
		if item.CommentType != "Section" {
			// Only [section] tags are relevant for hover annotations; plain
			// string translations are out of scope.
			continue
		}
		section := strings.TrimSpace(item.Section)
		if section == "" {
			continue
		}
		comment := decodeSectionComment(item.Comment)
		items = append(items, ScriptTagTranslation{Section: section, Comment: comment})
		if _, exists := bySection[section]; !exists {
			bySection[section] = comment
		}
	}
	s.items = items
	s.bySection = bySection
	s.loaded = true
	return nil
}

// decodeSectionComment keeps the comment text verbatim. XML entities such as
// &#10; have already been decoded into their runes by encoding/xml, so a
// comment containing "line one&#10;line two" arrives here with an embedded
// newline, which is exactly what the editor tooltip needs. Triple-backtick
// code fences are preserved as-is; the frontend renders them in a <pre> block.
func decodeSectionComment(comment string) string {
	return comment
}

// GetScriptTagTranslations returns every loaded section -> comment pair.
func (s *ScriptTagTranslationService) GetScriptTagTranslations() ([]ScriptTagTranslation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	out := make([]ScriptTagTranslation, len(s.items))
	copy(out, s.items)
	return out, nil
}

// GetScriptTagComment returns the comment for one section, or an empty string
// when the section is not present.
func (s *ScriptTagTranslationService) GetScriptTagComment(section string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return "", err
	}
	return s.bySection[section], nil
}

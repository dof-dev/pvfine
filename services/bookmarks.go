package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	appconfig "pvfine/config"
)

const (
	bookmarkDocumentVersion = 1
	builtinBookmarkBookID   = "builtin"
	bookmarkSourcePath      = "config/bookmarks.json"
)

// BookmarkEntry is one PVF-internal file bookmark.
type BookmarkEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// BookmarkGroup is a recursively nested bookmark group.
type BookmarkGroup struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Groups  []BookmarkGroup `json:"groups,omitempty"`
	Entries []BookmarkEntry `json:"entries,omitempty"`
}

// BookmarkBook is a runtime bookmark book. Builtin books are read-only in
// packaged applications and editable when running from the repository.
type BookmarkBook struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Builtin  bool            `json:"builtin"`
	Editable bool            `json:"editable"`
	Groups   []BookmarkGroup `json:"groups,omitempty"`
	Entries  []BookmarkEntry `json:"entries,omitempty"`
}

// BookmarkDocument is the complete runtime bookmark state.
type BookmarkDocument struct {
	Version      int            `json:"version"`
	ActiveBookID string         `json:"activeBookId"`
	Books        []BookmarkBook `json:"books"`
}

// BookmarkBookFile is the portable JSON representation of one bookmark book.
// IDs and runtime flags are intentionally omitted so imports always receive
// fresh local identities and are treated as editable books.
type BookmarkBookFile struct {
	Version int             `json:"version"`
	Name    string          `json:"name"`
	Groups  []BookmarkGroup `json:"groups,omitempty"`
	Entries []BookmarkEntry `json:"entries,omitempty"`
}

type storedBookmarkBook struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Groups  []BookmarkGroup `json:"groups,omitempty"`
	Entries []BookmarkEntry `json:"entries,omitempty"`
}

type storedBookmarkDocument struct {
	Version      int                  `json:"version"`
	ActiveBookID string               `json:"activeBookId"`
	Books        []storedBookmarkBook `json:"books"`
}

// BookmarkService persists user books and supplies the embedded system book.
type BookmarkService struct {
	mu         sync.Mutex
	path       string
	sourcePath string
	builtin    BookmarkBook
	initErr    error
}

// NewBookmarkService creates the application bookmark service. A repository
// source file takes precedence during local development, mirroring the
// existing annotation configuration behavior.
func NewBookmarkService() *BookmarkService {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return &BookmarkService{initErr: fmt.Errorf("获取用户配置目录失败: %w", err)}
	}
	sourcePath, _ := findBookmarkSourcePath()
	return newBookmarkServiceWithSource(filepath.Join(configDir, "pvfine", "bookmarks.json"), sourcePath)
}

func newBookmarkService(path string) *BookmarkService {
	return newBookmarkServiceWithSource(path, "")
}

func newBookmarkServiceWithSource(path, sourcePath string) *BookmarkService {
	service := &BookmarkService{path: path, sourcePath: sourcePath}
	data := appconfig.BookmarksJSON
	if sourcePath != "" {
		var err error
		data, err = os.ReadFile(sourcePath)
		if err != nil {
			service.initErr = fmt.Errorf("读取内置书签配置失败: %w", err)
			return service
		}
	}
	builtin, err := parseBuiltinBookmarkBook(data)
	if err != nil {
		service.initErr = err
		return service
	}
	builtin.Editable = sourcePath != ""
	service.builtin = builtin
	return service
}

// LoadBookmarks loads custom books and injects the current built-in book.
func (s *BookmarkService) LoadBookmarks() (BookmarkDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return BookmarkDocument{}, s.initErr
	}

	stored := storedBookmarkDocument{Version: bookmarkDocumentVersion, ActiveBookID: builtinBookmarkBookID}
	data, err := os.ReadFile(s.path)
	if err == nil {
		if err := decodeBookmarkJSON(data, &stored, "书签簿"); err != nil {
			return BookmarkDocument{}, err
		}
	} else if !os.IsNotExist(err) {
		return BookmarkDocument{}, fmt.Errorf("读取书签簿失败: %w", err)
	}

	custom, activeID, err := normalizeStoredBookmarkDocument(stored, s.builtin)
	if err != nil {
		return BookmarkDocument{}, err
	}
	books := make([]BookmarkBook, 0, len(custom)+1)
	books = append(books, cloneBookmarkBook(s.builtin))
	for _, storedBook := range custom {
		book := BookmarkBook{
			ID:       storedBook.ID,
			Name:     storedBook.Name,
			Editable: true,
			Groups:   cloneBookmarkGroups(storedBook.Groups),
			Entries:  cloneBookmarkEntries(storedBook.Entries),
		}
		books = append(books, book)
	}
	if !bookmarkBookIDExists(books, activeID) {
		activeID = builtinBookmarkBookID
	}
	return BookmarkDocument{
		Version:      bookmarkDocumentVersion,
		ActiveBookID: activeID,
		Books:        books,
	}, nil
}

// SaveBookmarks persists custom books and, in development mode, writes the
// built-in book back to config/bookmarks.json.
func (s *BookmarkService) SaveBookmarks(document BookmarkDocument) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return s.initErr
	}
	if document.Version == 0 {
		document.Version = bookmarkDocumentVersion
	}
	if document.Version != bookmarkDocumentVersion {
		return fmt.Errorf("不支持的书签簿版本: %d", document.Version)
	}

	builtin := cloneBookmarkBook(s.builtin)
	custom := make([]storedBookmarkBook, 0, len(document.Books))
	seenBookIDs := make(map[string]struct{}, len(document.Books))
	seenBookNames := map[string]struct{}{builtin.Name: {}}
	var suppliedBuiltin *BookmarkBook
	for index := range document.Books {
		book := document.Books[index]
		book.ID = strings.TrimSpace(book.ID)
		if book.ID == builtinBookmarkBookID {
			if !book.Builtin {
				return fmt.Errorf("保留的系统书签簿 id 不能用于自定义书签簿")
			}
			if suppliedBuiltin != nil {
				return fmt.Errorf("系统书签簿 id 重复")
			}
			suppliedBuiltin = &book
			continue
		}
		if book.Builtin {
			return fmt.Errorf("只有固定 id 的书签簿可以标记为内置")
		}
		if _, exists := seenBookIDs[book.ID]; exists {
			return fmt.Errorf("书签簿 id 重复: %q", book.ID)
		}
		seenBookIDs[book.ID] = struct{}{}
		book.Editable = true
		if err := normalizeBookmarkBook(&book, false, book.ID); err != nil {
			return err
		}
		if _, exists := seenBookNames[book.Name]; exists {
			return fmt.Errorf("书签簿名称不能重复: %q", book.Name)
		}
		seenBookNames[book.Name] = struct{}{}
		custom = append(custom, storedBookmarkBook{
			ID:      book.ID,
			Name:    book.Name,
			Groups:  cloneBookmarkGroups(book.Groups),
			Entries: cloneBookmarkEntries(book.Entries),
		})
	}

	if suppliedBuiltin != nil {
		candidate := cloneBookmarkBook(*suppliedBuiltin)
		candidate.ID = builtinBookmarkBookID
		candidate.Builtin = true
		if err := normalizeBookmarkBook(&candidate, true, "builtin"); err != nil {
			return err
		}
		if s.sourcePath == "" {
			if !sameBookmarkContent(candidate, builtin) {
				return fmt.Errorf("生产模式下系统书签簿不可编辑")
			}
		} else if !sameBookmarkContent(candidate, builtin) {
			if _, exists := seenBookNames[candidate.Name]; exists && candidate.Name != builtin.Name {
				return fmt.Errorf("书签簿名称不能重复: %q", candidate.Name)
			}
			if err := writeBuiltinBookmarkFile(s.sourcePath, toBookmarkBookFile(candidate)); err != nil {
				return err
			}
			builtin = candidate
			builtin.Builtin = true
			builtin.Editable = true
		}
	}

	activeID := strings.TrimSpace(document.ActiveBookID)
	if activeID != builtinBookmarkBookID && !storedBookmarkBookIDExists(custom, activeID) {
		activeID = builtinBookmarkBookID
	}
	storedDocument := storedBookmarkDocument{
		Version:      bookmarkDocumentVersion,
		ActiveBookID: activeID,
		Books:        custom,
	}
	if len(custom) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除空书签簿文件失败: %w", err)
		}
	} else if err := writeBookmarkJSON(s.path, storedDocument, 0o600, ".bookmarks-*.tmp"); err != nil {
		return fmt.Errorf("保存书签簿失败: %w", err)
	}
	s.builtin = builtin
	return nil
}

// ImportBookmarkBookDialog opens and parses one portable bookmark book.
func (s *BookmarkService) ImportBookmarkBookDialog() (*BookmarkBookFile, error) {
	paths, err := application.Get().Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("书签簿 JSON", "*.json").
		SetTitle("导入书签簿").
		PromptForSingleSelection()
	if err != nil {
		return nil, err
	}
	if paths == "" {
		return nil, nil
	}
	data, err := os.ReadFile(paths)
	if err != nil {
		return nil, fmt.Errorf("读取书签簿导入文件失败: %w", err)
	}
	book, err := parseBookmarkBookFile(data)
	if err != nil {
		return nil, err
	}
	return &book, nil
}

// ExportBookmarkBookDialog saves one portable bookmark book selected by the
// frontend. Built-in books are intentionally exportable.
func (s *BookmarkService) ExportBookmarkBookDialog(book BookmarkBookFile) (string, error) {
	if err := normalizeBookmarkBookFile(&book); err != nil {
		return "", err
	}
	filename := safeBookmarkFilename(book.Name) + ".json"
	path, err := application.Get().Dialog.SaveFile().
		SetFilename(filename).
		AddFilter("书签簿 JSON", "*.json").
		SetMessage("导出书签簿").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	data, err := marshalBookmarkJSON(book)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("写入书签簿导出文件失败: %w", err)
	}
	return path, nil
}

func parseBuiltinBookmarkBook(data []byte) (BookmarkBook, error) {
	file, err := parseBookmarkBookFile(data)
	if err != nil {
		return BookmarkBook{}, fmt.Errorf("解析内置书签配置失败: %w", err)
	}
	book := BookmarkBook{
		ID:      builtinBookmarkBookID,
		Name:    file.Name,
		Builtin: true,
		Groups:  file.Groups,
		Entries: file.Entries,
	}
	if err := normalizeBookmarkBook(&book, true, "builtin"); err != nil {
		return BookmarkBook{}, fmt.Errorf("内置书签配置无效: %w", err)
	}
	return book, nil
}

func parseBookmarkBookFile(data []byte) (BookmarkBookFile, error) {
	var file BookmarkBookFile
	if err := decodeBookmarkJSON(data, &file, "书签簿导入文件"); err != nil {
		return BookmarkBookFile{}, err
	}
	if err := normalizeBookmarkBookFile(&file); err != nil {
		return BookmarkBookFile{}, err
	}
	return file, nil
}

func normalizeBookmarkBookFile(file *BookmarkBookFile) error {
	if file.Version == 0 {
		file.Version = bookmarkDocumentVersion
	}
	if file.Version != bookmarkDocumentVersion {
		return fmt.Errorf("不支持的书签簿版本: %d", file.Version)
	}
	book := BookmarkBook{
		ID:      "portable",
		Name:    file.Name,
		Groups:  file.Groups,
		Entries: file.Entries,
	}
	if err := normalizeBookmarkBook(&book, false, "portable"); err != nil {
		return err
	}
	file.Name = book.Name
	file.Groups = book.Groups
	file.Entries = book.Entries
	return nil
}

func normalizeStoredBookmarkDocument(document storedBookmarkDocument, builtin BookmarkBook) ([]storedBookmarkBook, string, error) {
	if document.Version == 0 {
		document.Version = bookmarkDocumentVersion
	}
	if document.Version != bookmarkDocumentVersion {
		return nil, "", fmt.Errorf("不支持的书签簿版本: %d", document.Version)
	}
	custom := make([]storedBookmarkBook, 0, len(document.Books))
	seenIDs := make(map[string]struct{}, len(document.Books))
	seenNames := map[string]struct{}{builtin.Name: {}}
	for index := range document.Books {
		stored := document.Books[index]
		stored.ID = strings.TrimSpace(stored.ID)
		if stored.ID == builtinBookmarkBookID {
			candidate := BookmarkBook{
				ID:      builtinBookmarkBookID,
				Name:    stored.Name,
				Builtin: true,
				Groups:  stored.Groups,
				Entries: stored.Entries,
			}
			if err := normalizeBookmarkBook(&candidate, true, "builtin"); err != nil {
				return nil, "", err
			}
			if !sameBookmarkContent(candidate, builtin) {
				return nil, "", fmt.Errorf("用户配置中的系统书签簿与内置配置不一致")
			}
			continue
		}
		if stored.ID == "" {
			return nil, "", fmt.Errorf("书签簿 id 为空")
		}
		if _, exists := seenIDs[stored.ID]; exists {
			return nil, "", fmt.Errorf("书签簿 id 重复: %q", stored.ID)
		}
		seenIDs[stored.ID] = struct{}{}
		book := BookmarkBook{ID: stored.ID, Name: stored.Name, Groups: stored.Groups, Entries: stored.Entries}
		if err := normalizeBookmarkBook(&book, false, stored.ID); err != nil {
			return nil, "", err
		}
		if _, exists := seenNames[book.Name]; exists {
			return nil, "", fmt.Errorf("书签簿名称不能重复: %q", book.Name)
		}
		seenNames[book.Name] = struct{}{}
		custom = append(custom, storedBookmarkBook{
			ID:      book.ID,
			Name:    book.Name,
			Groups:  book.Groups,
			Entries: book.Entries,
		})
	}
	activeID := strings.TrimSpace(document.ActiveBookID)
	if activeID == "" {
		activeID = builtinBookmarkBookID
	}
	return custom, activeID, nil
}

func normalizeBookmarkBook(book *BookmarkBook, builtin bool, idPrefix string) error {
	book.ID = strings.TrimSpace(book.ID)
	book.Name = strings.TrimSpace(book.Name)
	if book.ID == "" || book.Name == "" {
		return fmt.Errorf("书签簿名称或 id 为空")
	}
	if builtin && book.ID != builtinBookmarkBookID {
		return fmt.Errorf("内置书签簿 id 无效: %q", book.ID)
	}
	if !builtin {
		if book.ID == builtinBookmarkBookID {
			return fmt.Errorf("保留的系统书签簿 id 不能用于自定义书签簿")
		}
		book.Builtin = false
		book.Editable = true
	}
	usedGroupIDs := make(map[string]struct{})
	groupCounter := 0
	if err := normalizeBookmarkEntries(&book.Entries); err != nil {
		return fmt.Errorf("书签簿 %q: %w", book.Name, err)
	}
	if err := normalizeBookmarkGroups(book.Groups, usedGroupIDs, &groupCounter, idPrefix); err != nil {
		return fmt.Errorf("书签簿 %q: %w", book.Name, err)
	}
	return nil
}

func normalizeBookmarkGroups(groups []BookmarkGroup, usedIDs map[string]struct{}, counter *int, idPrefix string) error {
	seenNames := make(map[string]struct{}, len(groups))
	for index := range groups {
		group := &groups[index]
		group.Name = strings.TrimSpace(group.Name)
		if group.Name == "" {
			return fmt.Errorf("分组名称不能为空")
		}
		if _, exists := seenNames[group.Name]; exists {
			return fmt.Errorf("同级分组名称不能重复: %q", group.Name)
		}
		seenNames[group.Name] = struct{}{}
		group.ID = strings.TrimSpace(group.ID)
		if group.ID == "" {
			for {
				(*counter)++
				group.ID = fmt.Sprintf("%s-group-%d", idPrefix, *counter)
				if _, exists := usedIDs[group.ID]; !exists {
					break
				}
			}
		}
		if _, exists := usedIDs[group.ID]; exists {
			return fmt.Errorf("分组 id 重复: %q", group.ID)
		}
		usedIDs[group.ID] = struct{}{}
		if err := normalizeBookmarkEntries(&group.Entries); err != nil {
			return fmt.Errorf("分组 %q: %w", group.Name, err)
		}
		if err := normalizeBookmarkGroups(group.Groups, usedIDs, counter, idPrefix); err != nil {
			return fmt.Errorf("分组 %q: %w", group.Name, err)
		}
	}
	return nil
}

func normalizeBookmarkEntries(entries *[]BookmarkEntry) error {
	seenPaths := make(map[string]struct{}, len(*entries))
	result := make([]BookmarkEntry, 0, len(*entries))
	for _, entry := range *entries {
		path, err := normalizeBookmarkPath(entry.Path)
		if err != nil {
			return err
		}
		if _, exists := seenPaths[path]; exists {
			continue
		}
		seenPaths[path] = struct{}{}
		entry.Path = path
		entry.Name = strings.TrimSpace(entry.Name)
		if entry.Name == "" {
			entry.Name = bookmarkPathBase(path)
		}
		result = append(result, entry)
	}
	*entries = result
	return nil
}

func normalizeBookmarkPath(raw string) (string, error) {
	path := strings.Trim(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"), "/")
	if path == "" {
		return "", fmt.Errorf("书签路径不能为空")
	}
	parts := strings.Split(path, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if part == "." || part == ".." {
			return "", fmt.Errorf("书签路径包含非法分段: %q", raw)
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "", fmt.Errorf("书签路径不能为空")
	}
	return strings.Join(clean, "/"), nil
}

func bookmarkPathBase(path string) string {
	if slash := strings.LastIndexByte(path, '/'); slash >= 0 {
		return path[slash+1:]
	}
	return path
}

func cloneBookmarkEntries(entries []BookmarkEntry) []BookmarkEntry {
	if len(entries) == 0 {
		return nil
	}
	return append([]BookmarkEntry(nil), entries...)
}

func cloneBookmarkGroups(groups []BookmarkGroup) []BookmarkGroup {
	if len(groups) == 0 {
		return nil
	}
	result := make([]BookmarkGroup, len(groups))
	for index, group := range groups {
		result[index] = BookmarkGroup{
			ID:      group.ID,
			Name:    group.Name,
			Groups:  cloneBookmarkGroups(group.Groups),
			Entries: cloneBookmarkEntries(group.Entries),
		}
	}
	return result
}

func cloneBookmarkBook(book BookmarkBook) BookmarkBook {
	book.Groups = cloneBookmarkGroups(book.Groups)
	book.Entries = cloneBookmarkEntries(book.Entries)
	return book
}

func sameBookmarkContent(left, right BookmarkBook) bool {
	return left.Name == right.Name &&
		bookmarkGroupsEqual(left.Groups, right.Groups) &&
		bookmarkEntriesEqual(left.Entries, right.Entries)
}

func bookmarkGroupsEqual(left, right []BookmarkGroup) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].ID != right[index].ID || left[index].Name != right[index].Name ||
			!bookmarkEntriesEqual(left[index].Entries, right[index].Entries) ||
			!bookmarkGroupsEqual(left[index].Groups, right[index].Groups) {
			return false
		}
	}
	return true
}

func bookmarkEntriesEqual(left, right []BookmarkEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func bookmarkBookIDExists(books []BookmarkBook, id string) bool {
	for _, book := range books {
		if book.ID == id {
			return true
		}
	}
	return false
}

func storedBookmarkBookIDExists(books []storedBookmarkBook, id string) bool {
	for _, book := range books {
		if book.ID == id {
			return true
		}
	}
	return false
}

func toBookmarkBookFile(book BookmarkBook) BookmarkBookFile {
	return BookmarkBookFile{
		Version: bookmarkDocumentVersion,
		Name:    book.Name,
		Groups:  cloneBookmarkGroups(book.Groups),
		Entries: cloneBookmarkEntries(book.Entries),
	}
}

func decodeBookmarkJSON(data []byte, target any, label string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("解析%s失败: %w", label, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%s只能包含一个 JSON 文档", label)
		}
		return fmt.Errorf("解析%s失败: %w", label, err)
	}
	return nil
}

func marshalBookmarkJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("编码书签簿失败: %w", err)
	}
	return append(data, '\n'), nil
}

func writeBookmarkJSON(path string, value any, mode os.FileMode, pattern string) error {
	data, err := marshalBookmarkJSON(value)
	if err != nil {
		return err
	}
	return atomicWriteBookmarkFile(path, data, mode, pattern)
}

func writeBuiltinBookmarkFile(path string, book BookmarkBookFile) error {
	previous, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("读取内置书签配置备份失败: %w", err)
	}
	if err == nil {
		if err := atomicWriteBookmarkFile(path+".bak", previous, 0o644, ".bookmarks-backup-*.tmp"); err != nil {
			return fmt.Errorf("写入内置书签配置备份失败: %w", err)
		}
	}
	data, err := marshalBookmarkJSON(book)
	if err != nil {
		return err
	}
	if err := atomicWriteBookmarkFile(path, data, 0o644, ".bookmarks-source-*.tmp"); err != nil {
		return fmt.Errorf("写入内置书签配置失败: %w", err)
	}
	return nil
}

func atomicWriteBookmarkFile(path string, data []byte, mode os.FileMode, pattern string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
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
	return os.Rename(tempPath, path)
}

func safeBookmarkFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "bookmarks"
	}
	var builder strings.Builder
	for _, r := range name {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			builder.WriteByte('_')
		default:
			builder.WriteRune(r)
		}
	}
	result := strings.TrimSpace(builder.String())
	if result == "" || result == "." || result == ".." {
		return "bookmarks"
	}
	return result
}

func findBookmarkSourcePath() (string, bool) {
	starts := make([]string, 0, 2)
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(executable))
	}
	seen := make(map[string]struct{})
	for _, start := range starts {
		for dir := filepath.Clean(start); ; dir = filepath.Dir(dir) {
			if _, ok := seen[dir]; !ok {
				seen[dir] = struct{}{}
				candidate := filepath.Join(dir, filepath.FromSlash(bookmarkSourcePath))
				if bookmarkFileExists(filepath.Join(dir, "go.mod")) && bookmarkFileExists(candidate) {
					return candidate, true
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return "", false
}

func bookmarkFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

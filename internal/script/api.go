package script

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"pvfine/internal/pvf"
)

// BatchAPI is the Go-side host facade used by GojaRuntime. It owns no live
// archive; all mutations flow through tx.Stage().
type BatchAPI struct {
	ctx context.Context
	tx  *Transaction

	logFn      func(LogEntry)
	progressFn func(Progress)

	scannedFiles int
}

// NewBatchAPI creates a host facade for one transaction.
func NewBatchAPI(ctx context.Context, tx *Transaction, logFn func(LogEntry), progressFn func(Progress)) *BatchAPI {
	if ctx == nil {
		ctx = context.Background()
	}
	return &BatchAPI{ctx: ctx, tx: tx, logFn: logFn, progressFn: progressFn}
}

func (a *BatchAPI) checkContext() error {
	if a == nil || a.tx == nil || a.tx.Stage() == nil {
		return fmt.Errorf("脚本事务不可用")
	}
	select {
	case <-a.ctx.Done():
		return a.ctx.Err()
	default:
		return nil
	}
}

// Files returns every archive entry in deterministic path order.
func (a *BatchAPI) Files() ([]*FileHandle, error) {
	if err := a.checkContext(); err != nil {
		return nil, err
	}
	archive := a.tx.Stage()
	entries := make([]*FileHandle, 0, archive.FileCount())
	for index := int32(0); index < archive.FileCount(); index++ {
		if err := a.checkContext(); err != nil {
			return nil, err
		}
		entries = append(entries, a.newFileHandle(archive.Path(index)))
	}
	a.scannedFiles += int(archive.FileCount())
	sort.SliceStable(entries, func(left, right int) bool {
		leftPath := strings.ToLower(entries[left].Path())
		rightPath := strings.ToLower(entries[right].Path())
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return entries[left].Index() < entries[right].Index()
	})
	return entries, nil
}

// Find resolves one archive entry using the same normalized lookup semantics
// as pvf.Archive.Find.
func (a *BatchAPI) Find(filePath string) (*FileHandle, bool, error) {
	if err := a.checkContext(); err != nil {
		return nil, false, err
	}
	index, ok := a.tx.Stage().Find(filePath)
	if !ok {
		return nil, false, nil
	}
	return a.newFileHandle(a.tx.Stage().Path(index)), true, nil
}

// Glob resolves slash-normalized case-insensitive patterns. '*' and '?' do
// not cross '/', while '**' may cross directory boundaries.
func (a *BatchAPI) Glob(pattern string) ([]*FileHandle, error) {
	if err := a.checkContext(); err != nil {
		return nil, err
	}
	pattern = normalizeGlob(pattern)
	if pattern == "" {
		return nil, fmt.Errorf("glob 模式不能为空")
	}
	archive := a.tx.Stage()
	result := make([]*FileHandle, 0)
	for index := int32(0); index < archive.FileCount(); index++ {
		if err := a.checkContext(); err != nil {
			return nil, err
		}
		if globMatch(pattern, normalizeGlob(archive.Path(index))) {
			result = append(result, a.newFileHandle(archive.Path(index)))
		}
	}
	a.scannedFiles += int(archive.FileCount())
	return result, nil
}

func normalizeGlob(value string) string {
	return strings.ToLower(strings.Trim(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"), "/"))
}

func globMatch(pattern, value string) bool {
	patternRunes := []rune(pattern)
	valueRunes := []rune(value)
	states := make([][]bool, len(patternRunes)+1)
	for index := range states {
		states[index] = make([]bool, len(valueRunes)+1)
	}
	states[0][0] = true
	for pi, token := range patternRunes {
		for vi := 0; vi <= len(valueRunes); vi++ {
			if !states[pi][vi] {
				continue
			}
			switch token {
			case '*':
				if pi+1 < len(patternRunes) && patternRunes[pi+1] == '*' {
					if pi+2 < len(patternRunes) && patternRunes[pi+2] == '/' {
						// A recursive directory wildcard may match zero complete
						// path components, including the following slash.
						states[pi+3][vi] = true
					} else {
						states[pi+2][vi] = true
					}
					if vi < len(valueRunes) {
						states[pi][vi+1] = true
					}
				} else {
					states[pi+1][vi] = true
					if vi < len(valueRunes) && valueRunes[vi] != '/' {
						states[pi][vi+1] = true
					}
				}
			case '?':
				if vi < len(valueRunes) && valueRunes[vi] != '/' {
					states[pi+1][vi+1] = true
				}
			default:
				if vi < len(valueRunes) && token == valueRunes[vi] {
					states[pi+1][vi+1] = true
				}
			}
		}
	}
	return states[len(patternRunes)][len(valueRunes)]
}

// ScannedCount reports how many archive entries host queries inspected.
func (a *BatchAPI) ScannedCount() int {
	if a == nil {
		return 0
	}
	return a.scannedFiles
}

// ModifiedCount reports the number of unique final changes, including created
// and deleted entries.
func (a *BatchAPI) ModifiedCount() int {
	if a == nil || a.tx == nil {
		return 0
	}
	changes, err := a.tx.Changes()
	if err != nil {
		return 0
	}
	return len(changes)
}

// CreateFile stages a new entry and returns its handle. text is the initial
// decompiled content; an empty text creates an empty file.
func (a *BatchAPI) CreateFile(filePath string, dataType int32, text string) (*FileHandle, error) {
	if err := a.checkContext(); err != nil {
		return nil, err
	}
	path, err := a.tx.CreateFile(filePath, dataType, nil)
	if err != nil {
		return nil, err
	}
	if text != "" {
		if err := a.tx.SetText(path, text); err != nil {
			return nil, err
		}
	}
	return a.newFileHandle(path), nil
}

// CopyFile stages a copy of an existing entry.
func (a *BatchAPI) CopyFile(from, to string, overwrite bool) (*FileHandle, error) {
	if err := a.checkContext(); err != nil {
		return nil, err
	}
	path, err := a.tx.CopyFile(from, to, overwrite)
	if err != nil {
		return nil, err
	}
	return a.newFileHandle(path), nil
}

// DeleteFile stages removal of one entry.
func (a *BatchAPI) DeleteFile(filePath string) (bool, error) {
	if err := a.checkContext(); err != nil {
		return false, err
	}
	return a.tx.DeleteFile(filePath)
}

// OpenList resolves a .lst file and returns its list facade.
func (a *BatchAPI) OpenList(filePath string) (*ListHandle, error) {
	if err := a.checkContext(); err != nil {
		return nil, err
	}
	index, ok := a.tx.Stage().Find(filePath)
	if !ok {
		return nil, fmt.Errorf("列表文件不存在: %s", filePath)
	}
	if a.tx.Stage().File(index).DataType != pvf.TypeScript {
		return nil, fmt.Errorf("文件 %s 不是 .lst 列表", a.tx.Stage().Path(index))
	}
	return &ListHandle{api: a, path: a.tx.Stage().Path(index)}, nil
}

// ListHandle is the narrow Go host object behind a JavaScript PVFList. Entry
// paths inside a .lst are relative to the list file's own directory.
type ListHandle struct {
	api  *BatchAPI
	path string
}

// Path returns the archive path of the list file.
func (l *ListHandle) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Get returns the id/path pairs in file order.
func (l *ListHandle) Get() ([]pvf.ListPair, error) {
	if err := l.api.checkContext(); err != nil {
		return nil, err
	}
	return l.api.tx.ListPairs(l.path)
}

// Set inserts or updates one id/path entry.
func (l *ListHandle) Set(id, entryPath string) error {
	if err := l.api.checkContext(); err != nil {
		return err
	}
	relative, err := l.storagePath(entryPath)
	if err != nil {
		return err
	}
	return l.api.tx.SetListPairs(l.path, []pvf.ListPair{{ID: id, Path: relative}})
}

// MSet inserts or updates several id/path entries at once.
func (l *ListHandle) MSet(pairs []pvf.ListPair) error {
	if err := l.api.checkContext(); err != nil {
		return err
	}
	if len(pairs) == 0 {
		return nil
	}
	resolved := make([]pvf.ListPair, 0, len(pairs))
	for _, pair := range pairs {
		relative, err := l.storagePath(pair.Path)
		if err != nil {
			return err
		}
		resolved = append(resolved, pvf.ListPair{ID: pair.ID, Path: relative})
	}
	return l.api.tx.SetListPairs(l.path, resolved)
}

// Unset removes every entry whose id matches and reports whether any was
// removed.
func (l *ListHandle) Unset(id string) (bool, error) {
	if err := l.api.checkContext(); err != nil {
		return false, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false, fmt.Errorf("列表 id 不能为空")
	}
	removed, err := l.api.tx.UnsetListIDs(l.path, []string{id})
	if err != nil {
		return false, err
	}
	return removed > 0, nil
}

// GetID returns the id registered for entryPath, or false when the list has no
// matching entry. Unlike Set, it neither validates nor requires the target file
// to exist: it normalizes the path and compares it with the list content.
func (l *ListHandle) GetID(entryPath string) (string, bool, error) {
	if err := l.api.checkContext(); err != nil {
		return "", false, err
	}
	raw, err := normalizeListPath(entryPath)
	if err != nil {
		return "", false, err
	}
	for _, candidate := range l.candidatePaths(raw) {
		id, found, err := l.api.tx.ListID(l.path, candidate)
		if err != nil {
			return "", false, err
		}
		if found {
			return id, true, nil
		}
	}
	return "", false, nil
}

// storagePath resolves an entry path to the list-relative form the .lst file
// stores. Both spellings are accepted: the native relative form
// ("character/x.equ") and a full archive path ("equipment/character/x.equ").
// The target must exist in the archive so a typo cannot register a dangling
// entry.
func (l *ListHandle) storagePath(entryPath string) (string, error) {
	raw, err := normalizeListPath(entryPath)
	if err != nil {
		return "", err
	}
	for _, candidate := range l.candidatePaths(raw) {
		if l.targetExists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("列表条目指向的文件不存在: %s", raw)
}

// candidatePaths lists the list-relative spellings a user path may mean, most
// literal first: the path as written, then the same path interpreted as an
// archive path and rebased onto the list directory.
func (l *ListHandle) candidatePaths(raw string) []string {
	candidates := []string{raw}
	if rebased, ok := l.rebase(raw); ok && rebased != raw {
		candidates = append(candidates, rebased)
	}
	return candidates
}

// rebase converts a full archive path into the list-relative form, when the
// path lives under the list file's directory.
func (l *ListHandle) rebase(archivePath string) (string, bool) {
	dir := path.Dir(l.path)
	if dir == "." {
		return archivePath, true
	}
	prefix := strings.ToLower(dir) + "/"
	if !strings.HasPrefix(strings.ToLower(archivePath), prefix) {
		return "", false
	}
	relative := archivePath[len(prefix):]
	if relative == "" {
		return "", false
	}
	return relative, true
}

// targetExists reports whether a list-relative entry resolves to an archive
// entry, mirroring the service lookup that also accepts a "(r)" sibling.
func (l *ListHandle) targetExists(relative string) bool {
	directory := path.Dir(l.path)
	if _, ok := l.api.tx.Stage().Find(path.Join(directory, relative)); ok {
		return true
	}
	base := path.Base(relative)
	if strings.HasPrefix(strings.ToLower(base), "(r)") {
		return false
	}
	_, ok := l.api.tx.Stage().Find(path.Join(directory, path.Dir(relative), "(r)"+base))
	return ok
}

// normalizeListPath trims an incoming entry path to its archive spelling.
func normalizeListPath(entryPath string) (string, error) {
	raw := strings.TrimSpace(strings.ReplaceAll(entryPath, "\\", "/"))
	raw = strings.TrimPrefix(raw, "./")
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return "", fmt.Errorf("列表条目路径不能为空")
	}
	if raw == "." || raw == ".." || strings.HasPrefix(raw, "../") {
		return "", fmt.Errorf("列表条目路径无效: %q", entryPath)
	}
	return raw, nil
}

// Log records a message and forwards it to the service event sink.
func (a *BatchAPI) Log(level, message string) {
	if a == nil || a.logFn == nil {
		return
	}
	a.logFn(LogEntry{Level: level, Message: message})
}

// Progress forwards a user-provided progress update.
func (a *BatchAPI) Progress(done, total int, message string) error {
	if err := a.checkContext(); err != nil {
		return err
	}
	if done < 0 || total < 0 || (total > 0 && done > total) {
		return fmt.Errorf("进度范围无效: %d/%d", done, total)
	}
	if a.progressFn != nil {
		a.progressFn(Progress{Done: done, Total: total, Message: message})
	}
	return nil
}

func (a *BatchAPI) newFileHandle(path string) *FileHandle {
	return &FileHandle{api: a, path: path, deleteRevision: a.tx.DeleteRevision()}
}

// FileHandle is the narrow Go host object behind a JavaScript PVFFile. It is
// addressed by path because staged creates and deletes shift entry indexes.
type FileHandle struct {
	api            *BatchAPI
	path           string
	deleteRevision int
}

func (f *FileHandle) Index() int32 {
	index, ok := f.lookup()
	if !ok {
		return -1
	}
	return index
}

func (f *FileHandle) Path() string {
	if f == nil {
		return ""
	}
	return f.path
}

func (f *FileHandle) Type() int32 {
	index, ok := f.lookup()
	if !ok {
		return 0
	}
	return f.api.tx.Stage().File(index).DataType
}

func (f *FileHandle) Size() int32 {
	index, ok := f.lookup()
	if !ok {
		return 0
	}
	return f.api.tx.Stage().File(index).DataSize
}

// lookup resolves the handle and rejects handles captured before a staged
// removal, because that removal may have renumbered the entry.
func (f *FileHandle) lookup() (int32, bool) {
	if f == nil || f.api == nil || f.api.tx == nil || f.api.tx.Stage() == nil {
		return 0, false
	}
	if f.deleteRevision != f.api.tx.DeleteRevision() {
		return 0, false
	}
	return f.api.tx.Stage().Find(f.path)
}

// staleError explains why a handle is unusable.
func (f *FileHandle) staleError() error {
	if f != nil && f.api != nil && f.api.tx != nil && f.deleteRevision != f.api.tx.DeleteRevision() {
		return fmt.Errorf("文件 %s 的句柄已失效,请重新查询后再操作", f.path)
	}
	return fmt.Errorf("文件不存在: %s", f.Path())
}

func (f *FileHandle) Text() (string, error) {
	if err := f.api.checkContext(); err != nil {
		return "", err
	}
	index, ok := f.lookup()
	if !ok {
		return "", f.staleError()
	}
	return f.api.tx.Stage().Text(index)
}

func (f *FileHandle) SetText(text string) error {
	if err := f.api.checkContext(); err != nil {
		return err
	}
	if _, ok := f.lookup(); !ok {
		return f.staleError()
	}
	return f.api.tx.SetText(f.path, text)
}

func (f *FileHandle) Parse() (*pvf.ScriptDocument, error) {
	if err := f.api.checkContext(); err != nil {
		return nil, err
	}
	index, ok := f.lookup()
	if !ok {
		return nil, f.staleError()
	}
	if f.Type() != pvf.TypeScript {
		return nil, fmt.Errorf("文件 %s 不是 TypeScript", f.Path())
	}
	raw, err := f.api.tx.Stage().RawBytes(index)
	if err != nil {
		return nil, err
	}
	document, err := f.api.tx.Stage().ParseScriptDocument(raw)
	if err != nil {
		return nil, err
	}
	for _, warning := range document.Warnings() {
		f.api.Log(
			LogLevelWarn,
			fmt.Sprintf("%s: [%s] %s", f.Path(), warning.Code, warning.Message),
		)
	}
	return document, nil
}

func (f *FileHandle) Write(document *pvf.ScriptDocument) error {
	if err := f.api.checkContext(); err != nil {
		return err
	}
	if _, ok := f.lookup(); !ok {
		return f.staleError()
	}
	if f.Type() != pvf.TypeScript {
		return fmt.Errorf("文件 %s 不是 TypeScript", f.Path())
	}
	raw, err := f.api.tx.Stage().EncodeScriptDocument(document)
	if err != nil {
		return err
	}
	return f.api.tx.SetRawBytes(f.path, raw)
}

// PathPatternMatch is exported for focused unit tests and future callers that
// need to validate the same virtual path pattern grammar.
func PathPatternMatch(pattern, value string) bool {
	return globMatch(normalizeGlob(pattern), normalizeGlob(value))
}

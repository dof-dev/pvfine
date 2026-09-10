package script

import (
	"context"
	"fmt"
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
		entries = append(entries, a.newFileHandle(index))
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
	return a.newFileHandle(index), true, nil
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
			result = append(result, a.newFileHandle(index))
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

// ModifiedCount reports the number of unique final payload changes.
func (a *BatchAPI) ModifiedCount() int {
	if a == nil || a.tx == nil {
		return 0
	}
	indexes, err := a.tx.ChangedIndexes()
	if err != nil {
		return 0
	}
	return len(indexes)
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

func (a *BatchAPI) newFileHandle(index int32) *FileHandle {
	return &FileHandle{api: a, index: index}
}

// FileHandle is the narrow Go host object behind a JavaScript PVFFile.
type FileHandle struct {
	api   *BatchAPI
	index int32
}

func (f *FileHandle) Index() int32 { return f.index }

func (f *FileHandle) Path() string {
	if f == nil || f.api == nil || f.api.tx == nil || f.api.tx.Stage() == nil {
		return ""
	}
	return f.api.tx.Stage().Path(f.index)
}

func (f *FileHandle) Type() int32 {
	if f == nil || f.api == nil || f.api.tx == nil || f.api.tx.Stage() == nil {
		return 0
	}
	return f.api.tx.Stage().File(f.index).DataType
}

func (f *FileHandle) Size() int32 {
	if f == nil || f.api == nil || f.api.tx == nil || f.api.tx.Stage() == nil {
		return 0
	}
	return f.api.tx.Stage().File(f.index).DataSize
}

func (f *FileHandle) Text() (string, error) {
	if err := f.api.checkContext(); err != nil {
		return "", err
	}
	return f.api.tx.Stage().Text(f.index)
}

func (f *FileHandle) SetText(text string) error {
	if err := f.api.checkContext(); err != nil {
		return err
	}
	return f.api.tx.SetText(f.index, text)
}

func (f *FileHandle) Parse() (*pvf.ScriptDocument, error) {
	if err := f.api.checkContext(); err != nil {
		return nil, err
	}
	if f.Type() != pvf.TypeScript {
		return nil, fmt.Errorf("文件 %s 不是 TypeScript", f.Path())
	}
	raw, err := f.api.tx.Stage().RawBytes(f.index)
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
	if f.Type() != pvf.TypeScript {
		return fmt.Errorf("文件 %s 不是 TypeScript", f.Path())
	}
	raw, err := f.api.tx.Stage().EncodeScriptDocument(document)
	if err != nil {
		return err
	}
	return f.api.tx.SetRawBytes(f.index, raw)
}

// PathPatternMatch is exported for focused unit tests and future callers that
// need to validate the same virtual path pattern grammar.
func PathPatternMatch(pattern, value string) bool {
	return globMatch(normalizeGlob(pattern), normalizeGlob(value))
}

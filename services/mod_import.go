package services

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	modpkg "pvfine/internal/mod"
	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

type ModImportPair struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Previous  string `json:"previous"`
	Overwrite bool   `json:"overwrite"`
}
type ModImportFile struct {
	Path      string          `json:"path"`
	Operation string          `json:"operation"`
	Overwrite bool            `json:"overwrite"`
	Pairs     []ModImportPair `json:"pairs"`
}
type ModImportPreview struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Version          string          `json:"version"`
	Format           string          `json:"format"`
	ImportedCount    int             `json:"importedCount"`
	OverwrittenCount int             `json:"overwrittenCount"`
	Files            []ModImportFile `json:"files"`
}
type modImportPlan struct {
	id                           string
	created                      time.Time
	archive                      *pvf.Archive
	repo                         *pvfversion.Repository
	revision, generation, loadID uint64
	head                         string
	files                        []importFile
}

// ModImportService freezes decoded content so confirmation never re-reads disk.
type ModImportService struct {
	c        *core
	mu       sync.Mutex
	plan     *modImportPlan
	expiry   *time.Timer
	registry *modpkg.Registry
}

func NewModImportService(c *core) *ModImportService {
	return &ModImportService{c: c, registry: modpkg.DefaultRegistry()}
}

// Formats lists only formats with a registered reader.
func (s *ModImportService) Formats() []modpkg.Descriptor {
	formats := []modpkg.Descriptor{}
	for _, descriptor := range s.registry.List() {
		if descriptor.CanRead {
			formats = append(formats, descriptor)
		}
	}
	return formats
}

func (s *ModImportService) readableFormat(id string) (modpkg.Format, error) {
	format, err := s.registry.Get(id)
	if err != nil {
		return modpkg.Format{}, err
	}
	if format.Reader == nil {
		return modpkg.Format{}, fmt.Errorf("mod 格式不支持导入: %s", format.Descriptor.Name)
	}
	return format, nil
}

func (s *ModImportService) PrepareDialog(formatID string) (*ModImportPreview, error) {
	if _, err := s.readableFormat(formatID); err != nil {
		return nil, err
	}
	dir, err := application.Get().Dialog.OpenFile().CanChooseFiles(false).CanChooseDirectories(true).SetTitle("选择 mod 目录").PromptForSingleSelection()
	if err != nil || dir == "" {
		return nil, err
	}
	return s.Prepare(dir, formatID)
}

func (s *ModImportService) Prepare(dir, formatID string) (*ModImportPreview, error) {
	format, err := s.readableFormat(formatID)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	detected, err := format.Reader.Detect(root.FS())
	if err != nil {
		return nil, err
	}
	if !detected {
		return nil, fmt.Errorf("所选目录不符合 mod 格式 %s", format.Descriptor.Name)
	}
	pkg, err := format.Reader.Read(root.FS())
	if err != nil {
		return nil, err
	}

	finish := s.c.archiveTasks.begin()
	defer finish()
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		return nil, err
	}
	plan := &modImportPlan{id: mcpID(), created: time.Now(), archive: s.c.archive, repo: s.c.versionRepo, revision: s.c.batchRevision, generation: s.c.archiveGeneration, loadID: s.c.versionLoadID, head: s.c.versionHead.ID}
	preview := &ModImportPreview{ID: plan.id, Name: pkg.Metadata.Name, Version: pkg.Metadata.Version, Format: formatID, Files: []ModImportFile{}}
	for _, entry := range pkg.Entries {
		if err := modpkg.ValidatePath(entry.Path); err != nil {
			return nil, err
		}
		target, err := normalizeNewFilePath(entry.Path)
		if err != nil || target != entry.Path {
			return nil, fmt.Errorf("无效的 mod 归档路径: %s", entry.Path)
		}
		index, exists := s.c.archive.Find(target)
		file := importFile{targetPath: target, sourcePath: entry.Path, dataType: entry.DataType, text: string(entry.Data)}
		row := ModImportFile{Path: target, Operation: string(entry.Operation), Overwrite: exists, Pairs: []ModImportPair{}}
		switch entry.Operation {
		case modpkg.ReplaceFile:
			if entry.Encoding != modpkg.Text || (entry.DataType != pvf.TypeScript && entry.DataType != pvf.TypeUnicode) {
				return nil, fmt.Errorf("不支持的 mod 文件内容类型: %s", target)
			}
		case modpkg.MergeList, modpkg.MergeStrings:
			file.dataType = pvf.TypeScript
			if entry.Operation == modpkg.MergeStrings {
				file.dataType = pvf.TypeUnicode
			}
			base := ""
			if exists {
				if s.c.archive.File(index).DataType != file.dataType {
					return nil, fmt.Errorf("合并表类型不匹配: %s", target)
				}
				base, err = s.c.archive.Text(index)
				if err != nil {
					return nil, err
				}
			}
			file.text, row.Pairs, err = mergeModTable(base, entry)
			if err != nil {
				return nil, fmt.Errorf("合并 %s: %w", target, err)
			}
		default:
			return nil, fmt.Errorf("不支持的 mod 操作: %s", entry.Operation)
		}
		plan.files = append(plan.files, file)
		preview.Files = append(preview.Files, row)
		if exists {
			preview.OverwrittenCount++
		} else {
			preview.ImportedCount++
		}
	}
	stage := s.c.archive.CloneForBatch()
	if _, err := applyPreparedImport(stage, plan.files, ImportModeText); err != nil {
		return nil, err
	}
	if stage.FileCount() < largeArchiveIndexThreshold {
		if _, _, err := buildIndex(stage); err != nil {
			return nil, err
		}
	}
	if s.c.versionRepo != nil {
		paths := make([]string, 0, len(plan.files))
		for _, f := range plan.files {
			paths = append(paths, f.targetPath)
		}
		if _, err := pvfversion.ContentSnapshotFromArchive(stage, paths); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	if s.expiry != nil {
		s.expiry.Stop()
	}
	s.plan = plan
	s.expiry = time.AfterFunc(10*time.Minute, func() { s.Release(plan.id) })
	s.mu.Unlock()
	return preview, nil
}

func (s *ModImportService) Release(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plan != nil && s.plan.id == id {
		s.plan = nil
		if s.expiry != nil {
			s.expiry.Stop()
			s.expiry = nil
		}
	}
}

func (s *ModImportService) Execute(id string) (*ImportResult, error) {
	s.mu.Lock()
	plan := s.plan
	if plan != nil && plan.id == id {
		s.plan = nil
		if s.expiry != nil {
			s.expiry.Stop()
			s.expiry = nil
		}
	} else {
		plan = nil
	}
	s.mu.Unlock()
	stale := func() error { return fmt.Errorf("mod 导入预览已过期，请重新预览") }
	if plan == nil {
		return nil, stale()
	}
	finish := s.c.archiveTasks.begin()
	defer finish()
	return NewArchiveService(s.c).importPreparedChecked(plan.files, "", ImportModeText, "导入 Mod", func() error {
		if time.Since(plan.created) >= 10*time.Minute || plan.archive != s.c.archive || plan.repo != s.c.versionRepo || plan.revision != s.c.batchRevision || plan.generation != s.c.archiveGeneration || plan.loadID != s.c.versionLoadID || plan.head != s.c.versionHead.ID {
			return stale()
		}
		return nil
	})
}

func mergeModTable(base string, entry modpkg.Entry) (string, []ModImportPair, error) {
	bom := ""
	if strings.HasPrefix(base, "\ufeff") {
		base = strings.TrimPrefix(base, "\ufeff")
		bom = "\ufeff"
	}
	var pairs []modpkg.Pair
	var err error
	if entry.Operation == modpkg.MergeList {
		list, parseErr := pvf.ParseListText(base)
		err = parseErr
		for _, p := range list {
			pairs = append(pairs, modpkg.Pair{Key: p.ID, Value: p.Path})
		}
	} else {
		pairs, err = modpkg.ParseMergeText(base, modpkg.MergeStrings)
	}
	if err != nil {
		return "", nil, err
	}
	positions := map[string]int{}
	for i, p := range pairs {
		positions[p.Key] = i
	}
	rows := []ModImportPair{}
	for _, p := range entry.Pairs {
		i, exists := positions[p.Key]
		row := ModImportPair{Key: p.Key, Value: p.Value, Overwrite: exists}
		if exists {
			row.Previous = pairs[i].Value
			pairs[i] = p
		} else {
			positions[p.Key] = len(pairs)
			pairs = append(pairs, p)
		}
		rows = append(rows, row)
	}
	var text strings.Builder
	if entry.Operation == modpkg.MergeList {
		for _, p := range pairs {
			fmt.Fprintf(&text, "%s\t`%s`\n", p.Key, strings.ReplaceAll(p.Value, "`", "``"))
		}
	} else {
		// Preserve comments, spacing, and all untouched lines in existing string tables.
		text.WriteString(bom)
		incoming := map[string]string{}
		for _, p := range entry.Pairs {
			incoming[p.Key] = p.Value
		}
		for _, line := range strings.SplitAfter(base, "\n") {
			key, _, ok := strings.Cut(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), ">")
			if value, replace := incoming[key]; ok && replace && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				text.WriteString(key + ">" + value)
				if strings.HasSuffix(line, "\r\n") {
					text.WriteString("\r\n")
				} else if strings.HasSuffix(line, "\n") {
					text.WriteByte('\n')
				}
				delete(incoming, key)
			} else {
				text.WriteString(line)
			}
		}
		for _, p := range entry.Pairs {
			if _, exists := incoming[p.Key]; !exists {
				continue
			}
			if text.Len() > len(bom) && !strings.HasSuffix(text.String(), "\n") {
				text.WriteByte('\n')
			}
			fmt.Fprintf(&text, "%s>%s\n", p.Key, p.Value)
		}
	}
	return text.String(), rows, nil
}

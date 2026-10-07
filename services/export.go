package services

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	modpkg "pvfine/internal/mod"
	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

var ErrExportStale = errors.New("导出预览已过期，请重新准备导出")
var exportSequence atomic.Uint64

type ExportRequest struct {
	Source              string   `json:"source"`
	Scopes              []string `json:"scopes"`
	CommitID            string   `json:"commitId"`
	Mode                string   `json:"mode"`
	Format              string   `json:"format"`
	Name                string   `json:"name"`
	Version             string   `json:"version"`
	IncludeDependencies bool     `json:"includeDependencies"`
}

type ExportPreview struct {
	ID              string              `json:"id"`
	FileCount       int                 `json:"fileCount"`
	DependencyCount int                 `json:"dependencyCount"`
	SkippedDeletes  int                 `json:"skippedDeletes"`
	Warnings        []string            `json:"warnings"`
	Files           []ExportPreviewFile `json:"files"`
}

type ExportPreviewFile struct {
	Path            string `json:"path"`
	Required        bool   `json:"required"`
	DependencyCount int    `json:"dependencyCount"`
}

type exportFile struct {
	path string
	data []byte
}

type exportPlan struct {
	preview    ExportPreview
	request    ExportRequest
	archive    *pvf.Archive
	repo       *pvfversion.Repository
	revision   uint64
	generation uint64
	loadID     uint64
	headID     string
	created    time.Time
	files      []exportFile
	pkg        modpkg.Package
}

// ExportService owns one short-lived immutable preview, shared by all export
// entry points. Source adaptation and format serialization remain independent.
type ExportService struct {
	c        *core
	mu       sync.Mutex
	plan     *exportPlan
	registry *modpkg.Registry
	expiry   *time.Timer
}

func NewExportService(c *core) *ExportService {
	return &ExportService{c: c, registry: modpkg.DefaultRegistry()}
}

func (s *ExportService) Formats() []modpkg.Descriptor { return s.registry.List() }

func (s *ExportService) Prepare(request ExportRequest) (result *ExportPreview, resultErr error) {
	started := time.Now()
	defer func() {
		count := 0
		if result != nil {
			count = result.FileCount
		}
		log.Printf("[pvfine:export] prepare finished: source=%s mode=%s dependencies=%t files=%d elapsed=%s error=%v",
			request.Source, request.Mode, request.IncludeDependencies, count, time.Since(started).Round(time.Millisecond), resultErr)
	}()
	finish := s.c.archiveTasks.begin()
	defer finish()
	s.mu.Lock()
	defer s.mu.Unlock()
	if request.Mode != "direct" && request.Mode != "mod" {
		return nil, fmt.Errorf("未知导出模式: %s", request.Mode)
	}
	if request.Mode == "mod" {
		if err := modpkg.ValidateName(request.Name); err != nil {
			return nil, err
		}
		if err := modpkg.ValidateVersion(request.Version); err != nil {
			return nil, err
		}
		f, err := s.registry.Get(request.Format)
		if err != nil {
			return nil, err
		}
		if f.Writer == nil {
			return nil, fmt.Errorf("该 mod 格式不支持导出")
		}
	}
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	log.Printf("[pvfine:export] prepare phase=lock elapsed=%s", time.Since(started).Round(time.Millisecond))
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	// Metadata never affects scope, content or dependencies. Reuse only a
	// still-valid snapshot; every response gets its own task identity.
	if old := s.plan; s.validLocked(old) && sameExportContentRequest(old.request, request) {
		next := *old
		next.request = request
		next.pkg.Metadata = modpkg.Metadata{Name: request.Name, Version: request.Version}
		if request.Mode == "mod" {
			format, _ := s.registry.Get(request.Format)
			if err := format.Writer.Validate(next.pkg); err != nil {
				return nil, err
			}
		}
		next.preview.ID = fmt.Sprint(exportSequence.Add(1))
		s.storePlan(&next)
		log.Printf("[pvfine:export] prepare phase=content cache=hit")
		preview := next.preview
		return &preview, nil
	}
	s.plan = nil
	if s.expiry != nil {
		s.expiry.Stop()
		s.expiry = nil
	}
	phaseStarted := time.Now()
	source, changes, err := s.exportSourceLocked(request)
	if err != nil {
		return nil, err
	}
	log.Printf("[pvfine:export] prepare phase=source files=%d skipped_tables=%d elapsed=%s",
		len(changes), source.skippedTables, time.Since(phaseStarted).Round(time.Millisecond))
	p := &exportPlan{
		request: request, archive: s.c.archive, repo: s.c.versionRepo,
		revision: s.c.batchRevision, generation: s.c.archiveGeneration,
		loadID: s.c.versionLoadID, headID: s.c.versionHead.ID, created: time.Now(),
		preview: ExportPreview{ID: fmt.Sprint(exportSequence.Add(1)), Warnings: []string{}},
		pkg:     modpkg.Package{Metadata: modpkg.Metadata{Name: request.Name, Version: request.Version}},
	}
	format, _ := s.registry.Get(request.Format)
	supportsDelete := false
	for _, operation := range format.Descriptor.Operations {
		if operation == modpkg.DeleteFile {
			supportsDelete = true
		}
	}
	phaseStarted = time.Now()
	for _, change := range changes {
		after, before := change.after, change.before
		if request.Mode == "direct" {
			content := after
			if content == nil {
				content = before
			}
			if content != nil {
				p.files = append(p.files, exportFile{path: content.path, data: content.data})
			}
			continue
		}
		if after == nil {
			if supportsDelete && before != nil {
				p.pkg.Entries = append(p.pkg.Entries, modpkg.Entry{Path: before.path, Operation: modpkg.DeleteFile})
			} else {
				p.preview.SkippedDeletes++
			}
			continue
		}
		entry, removed, err := exportModEntryReported(*after, before, &source.diagnostics)
		if err != nil {
			if isExportMergeTable(after.path) {
				source.diagnostics.skip("无法提取合并表改动 %s: %v", after.path, err)
				continue
			}
			return nil, err
		}
		p.preview.SkippedDeletes += removed
		if entry != nil {
			p.pkg.Entries = append(p.pkg.Entries, *entry)
		}
	}
	log.Printf("[pvfine:export] prepare phase=diff elapsed=%s", time.Since(phaseStarted).Round(time.Millisecond))
	if request.Mode == "mod" {
		explicitPairs := make(map[string]int)
		for _, entry := range p.pkg.Entries {
			explicitPairs[entry.Path] = len(entry.Pairs)
		}
		if request.IncludeDependencies {
			phaseStarted = time.Now()
			count, err := s.addExportDependenciesLocked(source, &p.pkg, format.Writer)
			if err != nil {
				return nil, err
			}
			p.preview.DependencyCount = count
			log.Printf("[pvfine:export] prepare phase=dependencies entries=%d elapsed=%s", count, time.Since(phaseStarted).Round(time.Millisecond))
		}
		if source.skippedTables > 0 {
			p.preview.Warnings = append(p.preview.Warnings, fmt.Sprintf("缺少准确的改动基线，跳过 %d 个 lst/str 文件；补齐依赖仅添加匹配条目", source.skippedTables))
		}
		if source.diagnostics.skipped > 0 {
			p.preview.Warnings = append(p.preview.Warnings, fmt.Sprintf("跳过 %d 处无法解析或补齐的内容，详情已记录到日志；导出内容或依赖可能不完整", source.diagnostics.skipped))
		}
		p.preview.FileCount = len(p.pkg.Entries)
		if p.preview.SkippedDeletes > 0 {
			p.preview.Warnings = append(p.preview.Warnings, fmt.Sprintf("%s 无法应用删除，跳过 %d 个文件或表条目删除", format.Descriptor.Name, p.preview.SkippedDeletes))
		}
		if p.preview.FileCount == 0 && p.preview.SkippedDeletes > 0 {
			return nil, fmt.Errorf("没有可应用的 mod 内容；跳过 %d 个文件或表条目删除", p.preview.SkippedDeletes)
		}
		if p.preview.FileCount == 0 {
			return nil, fmt.Errorf("没有可应用的 mod 内容；跳过 %d 个无改动基线的 lst/str 文件、%d 处无法解析或补齐的内容",
				source.skippedTables, source.diagnostics.skipped)
		}
		if err := format.Writer.Validate(p.pkg); err != nil {
			return nil, err
		}
		dependencies := make(map[string]int)
		for _, entry := range p.pkg.Entries {
			dependencies[entry.Path] = len(entry.Pairs) - explicitPairs[entry.Path]
		}
		for _, file := range format.Writer.Files(p.pkg) {
			p.preview.Files = append(p.preview.Files, ExportPreviewFile{
				Path: file.Path, Required: file.EntryPath == "", DependencyCount: dependencies[file.EntryPath],
			})
		}
	} else {
		p.preview.FileCount = len(p.files)
		for _, file := range p.files {
			p.preview.Files = append(p.preview.Files, ExportPreviewFile{Path: file.path})
		}
	}
	if p.preview.FileCount == 0 {
		return nil, errors.New("没有可导出的文件或可应用的 mod 内容")
	}
	s.storePlan(p)
	preview := p.preview
	return &preview, nil
}

func sameExportContentRequest(a, b ExportRequest) bool {
	return a.Source == b.Source && a.CommitID == b.CommitID && a.Mode == b.Mode &&
		a.Format == b.Format && a.IncludeDependencies == b.IncludeDependencies &&
		slices.Equal(a.Scopes, b.Scopes)
}

// The service mutex is held by the caller.
func (s *ExportService) storePlan(p *exportPlan) {
	if s.expiry != nil {
		s.expiry.Stop()
	}
	s.plan = p
	id := p.preview.ID
	remaining := 10*time.Minute - time.Since(p.created)
	s.expiry = time.AfterFunc(remaining, func() { s.Release(id) })
}

func (s *ExportService) Release(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plan != nil && s.plan.preview.ID == id {
		s.plan = nil
		if s.expiry != nil {
			s.expiry.Stop()
			s.expiry = nil
		}
	}
}

func (s *ExportService) validLocked(p *exportPlan) bool {
	return p != nil && time.Since(p.created) < 10*time.Minute &&
		p.archive == s.c.archive && p.repo == s.c.versionRepo &&
		p.revision == s.c.batchRevision && p.generation == s.c.archiveGeneration &&
		p.loadID == s.c.versionLoadID && p.headID == s.c.versionHead.ID
}

// ExecuteDialog picks the parent directory only after the frontend confirms.
func (s *ExportService) ExecuteDialog(id string) (string, error) {
	return s.ExecuteSelectionDialog(id, nil)
}

// ExecuteSelectionDialog applies exclusions to the frozen preview. Dependencies
// are not resolved again, so an explicitly excluded file cannot reappear.
func (s *ExportService) ExecuteSelectionDialog(id string, excluded []string) (string, error) {
	s.mu.Lock()
	s.c.mu.RLock()
	valid := s.plan != nil && s.plan.preview.ID == id && s.validLocked(s.plan)
	var selectionErr error
	if valid {
		_, selectionErr = s.selectedPlan(s.plan, excluded)
	}
	s.c.mu.RUnlock()
	s.mu.Unlock()
	if !valid {
		return "", ErrExportStale
	}
	if selectionErr != nil {
		return "", selectionErr
	}
	dir, err := application.Get().Dialog.OpenFile().
		CanChooseFiles(false).CanChooseDirectories(true).CanCreateDirectories(true).
		SetTitle("选择导出目标目录").PromptForSingleSelection()
	if err != nil || dir == "" {
		return "", err
	}
	return s.executeSelectedTo(id, dir, excluded)
}

func (s *ExportService) executeTo(id, dir string) (string, error) {
	return s.executeSelectedTo(id, dir, nil)
}

func (s *ExportService) selectedPlan(original *exportPlan, excluded []string) (*exportPlan, error) {
	known := make(map[string]bool)
	for _, file := range original.preview.Files {
		known[file.Path] = file.Required
	}
	removed := make(map[string]bool)
	for _, p := range excluded {
		required, exists := known[p]
		if !exists || required {
			return nil, fmt.Errorf("不能移除未知文件或必需文件: %s", p)
		}
		removed[p] = true
	}
	plan := *original
	if plan.request.Mode == "mod" {
		format, err := s.registry.Get(plan.request.Format)
		if err != nil {
			return nil, err
		}
		removedEntries := make(map[string]bool)
		for _, file := range format.Writer.Files(plan.pkg) {
			if removed[file.Path] {
				removedEntries[file.EntryPath] = true
			}
		}
		plan.pkg.Entries = nil
		for _, entry := range original.pkg.Entries {
			if !removedEntries[entry.Path] {
				plan.pkg.Entries = append(plan.pkg.Entries, entry)
			}
		}
		if err := format.Writer.Validate(plan.pkg); err != nil {
			return nil, err
		}
	} else {
		plan.files = nil
		for _, file := range original.files {
			if !removed[file.path] {
				plan.files = append(plan.files, file)
			}
		}
		if len(plan.files) == 0 {
			return nil, errors.New("没有可导出的文件")
		}
	}
	return &plan, nil
}

func (s *ExportService) executeSelectedTo(id, dir string, excluded []string) (string, error) {
	finish := s.c.archiveTasks.begin()
	defer finish()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	p := s.plan
	if p == nil || p.preview.ID != id || !s.validLocked(p) {
		return "", ErrExportStale
	}
	p, err := s.selectedPlan(p, excluded)
	if err != nil {
		return "", err
	}
	defer func() {
		if s.plan == nil && s.expiry != nil {
			s.expiry.Stop()
			s.expiry = nil
		}
	}()
	if p.request.Mode == "mod" {
		f, err := s.registry.Get(p.request.Format)
		if err != nil {
			return "", err
		}
		result, err := writeModDirectory(dir, p.pkg, f.Writer)
		if err == nil {
			s.plan = nil
		}
		return result, err
	}
	destinations := make(map[string]bool)
	for _, file := range p.files {
		dest, err := safeExportPath(dir, file.path)
		if err != nil {
			return "", err
		}
		if destinations[canonicalExportDestination(dest)] {
			return "", fmt.Errorf("导出路径冲突: %s", file.path)
		}
		destinations[canonicalExportDestination(dest)] = true
	}
	for _, file := range p.files {
		dest, _ := safeExportPath(dir, file.path)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dest, file.data, 0o644); err != nil {
			return "", err
		}
	}
	s.plan = nil
	return dir, nil
}

func writeModDirectory(parent string, pkg modpkg.Package, writer modpkg.Writer) (string, error) {
	if err := writer.Validate(pkg); err != nil {
		return "", err
	}
	dest := filepath.Join(parent, pkg.Metadata.Name)
	// Windows directory identity is case-insensitive, including when tests
	// run on another host filesystem.
	siblings, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	for _, sibling := range siblings {
		if strings.EqualFold(sibling.Name(), pkg.Metadata.Name) {
			return "", fmt.Errorf("同名 mod 文件夹已存在，请修改名称或目标目录: %s", pkg.Metadata.Name)
		}
	}
	if _, err := os.Lstat(dest); err == nil {
		return "", fmt.Errorf("同名 mod 文件夹已存在，请修改名称或目标目录: %s", pkg.Metadata.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	temp, err := os.MkdirTemp(parent, ".pvfine-mod-")
	if err != nil {
		return "", err
	}
	// Only the exact directory returned by MkdirTemp is ever cleaned up.
	defer os.RemoveAll(temp)
	if err := writer.Write(temp, pkg); err != nil {
		return "", err
	}
	// Windows refuses to rename a directory over an existing directory.
	if runtime.GOOS == "windows" {
		if err := os.Rename(temp, dest); err != nil {
			return "", err
		}
		return dest, nil
	}
	// Other platforms reserve the name exclusively. Rename replaces only our
	// empty reservation; never delete an existing package to publish an export.
	if err := os.Mkdir(dest, 0o755); err != nil {
		return "", fmt.Errorf("无法创建 mod 目录（可能已存在）: %w", err)
	}
	if err := os.Rename(temp, dest); err != nil {
		_ = os.Remove(dest) // Remove refuses a nonempty directory.
		return "", err
	}
	return dest, nil
}

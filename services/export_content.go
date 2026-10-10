package services

import (
	"bytes"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	modpkg "pvfine/internal/mod"
	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

type exportContent struct {
	path     string
	dataType int32
	data     []byte
}

type exportChange struct{ before, after *exportContent }

type exportDiagnostics struct {
	skipped int
}

func (d *exportDiagnostics) skip(format string, args ...any) {
	d.skipped++
	log.Printf("[pvfine:export] skipped: "+format, args...)
}

func isExportMergeTable(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	return ext == ".lst" || ext == ".str"
}

// Historical reads resolve only requested logical objects, never checkout a
// commit into the live archive or re-encode an entire PVF.
type exportSource struct {
	archive       *pvf.Archive
	repo          *pvfversion.Repository
	base          *pvf.Archive
	snapshot      pvfversion.Snapshot
	canonical     bool
	skippedTables int
	diagnostics   exportDiagnostics
}

func (s *exportSource) lookup(p string) (string, bool) {
	if s.snapshot != nil {
		e, ok := s.snapshot[pvfversion.CanonicalPath(p)]
		return e.Path, ok
	}
	i, ok := s.archive.Find(p)
	if !ok {
		return "", false
	}
	return s.archive.Path(i), true
}

func (s *exportSource) read(p string) (*exportContent, error) {
	if s.snapshot != nil {
		e, ok := s.snapshot[pvfversion.CanonicalPath(p)]
		if !ok {
			return nil, fmt.Errorf("导出文件不存在: %s", p)
		}
		return s.versionContent(e)
	}
	i, ok := s.archive.Find(p)
	if !ok {
		return nil, fmt.Errorf("导出文件不存在: %s", p)
	}
	t := s.archive.File(i).DataType
	var data []byte
	if t == pvf.TypeScript || t == pvf.TypeUnicode {
		var text string
		var err error
		if s.canonical {
			text, err = s.archive.CanonicalText(i)
		} else {
			text, err = s.archive.Text(i)
		}
		if err != nil {
			return nil, err
		}
		data = []byte(text)
	} else {
		raw, err := s.archive.RawBytes(i)
		if err != nil {
			return nil, err
		}
		data = append([]byte(nil), raw...)
	}
	return &exportContent{path: s.archive.Path(i), dataType: t, data: data}, nil
}

func (s *exportSource) versionContent(e pvfversion.Entry) (*exportContent, error) {
	content, err := readVersionContent(s.repo, s.base, e)
	if err != nil {
		return nil, err
	}
	return &exportContent{path: e.Path, dataType: e.DataType, data: append([]byte(nil), content.Raw...)}, nil
}

func (s *ExportService) exportSourceLocked(r ExportRequest) (*exportSource, []exportChange, error) {
	a := s.c.archive
	source := &exportSource{archive: a}
	changes := []exportChange{}
	if r.Source == "memory" {
		for index := int32(0); index < a.FileCount(); index++ {
			if !a.IsModified(index) {
				continue
			}
			if r.Mode == "mod" && isExportMergeTable(a.Path(index)) {
				source.skippedTables++
				developmentLog("[pvfine:export] skip table without change baseline: %s", a.Path(index))
				continue
			}
			content, err := source.read(a.Path(index))
			if err != nil {
				return nil, nil, err
			}
			if r.Mode == "direct" && content.dataType != pvf.TypeScript && content.dataType != pvf.TypeUnicode {
				content.data = nil
			}
			changes = append(changes, exportChange{after: content})
		}
		return source, changes, nil
	}
	if r.Source == "selection" {
		var selections []exportSelection
		if s.c.diskIndex != nil {
			selections = collectDiskExportSelections(a, s.c.diskIndex, r.Scopes)
		} else {
			selections = collectExportSelections(a, s.c.sortedPaths, r.Scopes)
		}
		for _, selection := range selections {
			if r.Mode == "mod" && isExportMergeTable(selection.path) {
				source.skippedTables++
				developmentLog("[pvfine:export] skip table without change baseline: %s", selection.path)
				continue
			}
			content, err := source.read(selection.path)
			if err != nil {
				return nil, nil, err
			}
			// Existing selection export renders unknown types as empty text.
			if r.Mode == "direct" && content.dataType != pvf.TypeScript && content.dataType != pvf.TypeUnicode {
				content.data = nil
			}
			changes = append(changes, exportChange{after: content})
		}
		return source, changes, nil
	}
	if r.Source != "working" && r.Source != "commit" {
		return nil, nil, fmt.Errorf("未知导出来源: %s", r.Source)
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		return nil, nil, err
	}
	source.repo, source.base, source.canonical = s.c.versionRepo, s.c.versionBaseArchive, true
	if source.repo == nil {
		return nil, nil, ErrVersionNotEnabled
	}
	if source.base == nil {
		var err error
		source.base, err = pvf.Open(source.repo.BasePath())
		if err != nil {
			return nil, nil, err
		}
	}
	fileChanges := s.c.versionChanges
	if r.Source == "commit" {
		if strings.TrimSpace(r.CommitID) == "" {
			return nil, nil, fmt.Errorf("提交 ID 不能为空")
		}
		var err error
		fileChanges, err = source.repo.Changes(r.CommitID)
		if err != nil {
			return nil, nil, err
		}
		if r.IncludeDependencies && r.Mode == "mod" {
			source.snapshot, err = source.repo.Snapshot(r.CommitID)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	for _, change := range fileChanges {
		item := exportChange{}
		readState := func(hash string, typ int32, p string) (*exportContent, error) {
			if hash == "" {
				return nil, nil
			}
			return source.versionContent(pvfversion.Entry{Path: p, Hash: hash, DataType: typ})
		}
		var err error
		beforePath := change.DisplayPath
		if change.OldDisplayPath != "" {
			beforePath = change.OldDisplayPath
		}
		if r.Mode == "mod" && change.AfterHash == "" {
			item.before = &exportContent{path: beforePath, dataType: change.BeforeType}
		} else if (r.Mode == "mod" && isExportMergeTable(change.DisplayPath)) || change.AfterHash == "" {
			item.before, err = readState(change.BeforeHash, change.BeforeType, beforePath)
			if err != nil {
				if r.Mode == "mod" && isExportMergeTable(change.DisplayPath) {
					source.diagnostics.skip("无法读取合并表基线 %s: %v", beforePath, err)
					continue
				}
				return nil, nil, err
			}
		}
		if change.AfterHash != "" {
			if r.Source == "working" {
				item.after, err = source.read(change.DisplayPath)
			} else {
				item.after, err = readState(change.AfterHash, change.AfterType, change.DisplayPath)
			}
			if err != nil {
				if r.Mode == "mod" && isExportMergeTable(change.DisplayPath) {
					source.diagnostics.skip("无法读取合并表 %s: %v", change.DisplayPath, err)
					continue
				}
				return nil, nil, err
			}
		}
		changes = append(changes, item)
	}
	return source, changes, nil
}

func canonicalExportDestination(p string) string { return strings.ToLower(filepath.Clean(p)) }

func exportModEntry(after exportContent, before *exportContent) (*modpkg.Entry, int, error) {
	return exportModEntryReported(after, before, &exportDiagnostics{})
}

func exportModEntryReported(after exportContent, before *exportContent, diagnostics *exportDiagnostics) (*modpkg.Entry, int, error) {
	entry := modpkg.Entry{
		Path: after.path, Operation: modpkg.ReplaceFile, Encoding: modpkg.Text,
		DataType: after.dataType, Data: bytes.TrimPrefix(after.data, []byte("\xef\xbb\xbf")),
	}
	switch strings.ToLower(path.Ext(after.path)) {
	case ".lst":
		entry.Operation = modpkg.MergeList
	case ".str":
		entry.Operation = modpkg.MergeStrings
	default:
		return &entry, 0, nil
	}
	pairs, err := parseExportPairsReported(after, entry.Operation, diagnostics)
	if err != nil {
		return nil, 0, err
	}
	entry.Data = nil
	if before == nil {
		entry.Pairs = pairs
		if len(pairs) == 0 {
			return nil, 0, nil
		}
		return &entry, 0, nil
	}
	oldPairs, err := parseExportPairsReported(*before, entry.Operation, diagnostics)
	if err != nil {
		return nil, 0, err
	}
	old, current := pairMap(oldPairs), pairMap(pairs)
	for _, p := range pairs {
		if v, ok := old[p.Key]; !ok || v != p.Value {
			entry.Pairs = append(entry.Pairs, p)
		}
	}
	removed := 0
	for key := range old {
		if _, exists := current[key]; !exists {
			removed++
		}
	}
	if len(entry.Pairs) == 0 {
		return nil, removed, nil
	}
	return &entry, removed, nil
}

func pairMap(pairs []modpkg.Pair) map[string]string {
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		m[p.Key] = p.Value
	}
	return m
}

func parseExportPairs(content exportContent, operation modpkg.Operation) ([]modpkg.Pair, error) {
	return parseExportPairsReported(content, operation, &exportDiagnostics{})
}

func parseExportPairsReported(content exportContent, operation modpkg.Operation, diagnostics *exportDiagnostics) ([]modpkg.Pair, error) {
	if !utf8.Valid(content.data) || (content.dataType != pvf.TypeScript && content.dataType != pvf.TypeUnicode) {
		return nil, fmt.Errorf("合并表的内容类型或编码不支持: %s", content.path)
	}
	text := strings.TrimPrefix(string(content.data), "\ufeff")
	pairs := []modpkg.Pair{}
	if operation == modpkg.MergeStrings {
		if content.dataType != pvf.TypeUnicode {
			return nil, fmt.Errorf("文字表不是文本类型: %s", content.path)
		}
		positions := map[string]int{}
		number := 0
		for line := range strings.SplitSeq(text, "\n") {
			number++
			line = strings.TrimSuffix(line, "\r")
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			sep := strings.IndexByte(line, '>')
			if sep <= 0 || strings.ContainsRune(line, '\x00') {
				diagnostics.skip("无法解析文字表 %s 第 %d 行", content.path, number)
				continue
			}
			key, value := line[:sep], line[sep+1:]
			// Retain the first position and last value, without building a
			// second full table or allocating an array for every source line.
			if i, exists := positions[key]; exists {
				pairs[i].Value = value
			} else {
				positions[key] = len(pairs)
				pairs = append(pairs, modpkg.Pair{Key: key, Value: value})
			}
		}
		return pairs, nil
	}
	if content.dataType != pvf.TypeScript {
		return nil, fmt.Errorf("列表不是脚本类型: %s", content.path)
	}
	list, err := pvf.ParseListText(text)
	if err != nil {
		return nil, fmt.Errorf("无法解析列表 %s: %w", content.path, err)
	}
	for _, pair := range list {
		pairs = append(pairs, modpkg.Pair{Key: pair.ID, Value: strings.ReplaceAll(pair.Path, "\\", "/")})
	}
	return pairs, nil
}

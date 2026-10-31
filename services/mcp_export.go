package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *MCPService) exportPrepare(a mcpArgs) (any, error) {
	if err := s.check(a); err != nil {
		return nil, err
	}
	if a.Mode != "direct" && a.Mode != "mod" {
		return nil, fmt.Errorf("INVALID_ARGUMENT: mode=direct/mod")
	}
	if a.Source != "selection" && a.Source != "memory" && a.Source != "working" && a.Source != "commit" {
		return nil, fmt.Errorf("INVALID_ARGUMENT: source")
	}
	if a.Source == "selection" && len(a.Scopes) == 0 {
		return nil, fmt.Errorf("INVALID_ARGUMENT: scopes 必填，根目录使用空字符串")
	}
	s.export.mu.Lock()
	pending := s.export.plan != nil && s.export.validLockedSafe()
	s.export.mu.Unlock()
	if pending {
		return nil, fmt.Errorf("EXPORT_PLAN_PENDING")
	}
	preview, err := s.export.prepare(ExportRequest{Source: a.Source, Scopes: a.Scopes, Mode: a.Mode, Format: a.Format, Name: a.Name, Version: a.Version, CommitID: a.CommitID, IncludeDependencies: a.IncludeDependencies}, true)
	if err != nil {
		return nil, err
	}
	// Enforce limits before retaining an AI export plan, including binary content.
	s.export.mu.Lock()
	p := s.export.plan
	var total int
	for _, file := range p.files {
		total += len(file.data)
	}
	for _, entry := range p.pkg.Entries {
		total += len(entry.Data)
		for _, pair := range entry.Pairs {
			total += len(pair.Key) + len(pair.Value)
		}
	}
	s.export.mu.Unlock()
	if preview.FileCount > 10000 || total > 128<<20 {
		s.export.Release(preview.ID)
		return nil, fmt.Errorf("EXPORT_LIMIT: 最多 10000 文件 / 128 MiB")
	}
	s.mu.Lock()
	s.exportID = preview.ID

	s.mu.Unlock()
	return map[string]any{"exportPlanId": preview.ID, "fileCount": preview.FileCount, "dependencyCount": preview.DependencyCount, "skippedDeletes": preview.SkippedDeletes, "warnings": exportWarningRows(preview.Warnings)}, nil
}
func exportWarningRows(warnings []string) []map[string]string {
	rows := []map[string]string{}
	for i, w := range warnings {
		rows = append(rows, map[string]string{"warningId": fmt.Sprintf("w%d", i), "message": w})
	}
	return rows
}
func (s *MCPService) exportPreview(a mcpArgs) (any, error) {
	s.mu.Lock()
	id := s.exportID
	s.mu.Unlock()
	if id == "" || id != a.ExportPlanID {
		return nil, fmt.Errorf("PLAN_NOT_FOUND")
	}
	s.export.mu.Lock()
	defer s.export.mu.Unlock()
	p := s.export.plan
	if p == nil || !s.export.validLockedSafe() {
		return nil, ErrExportStale
	}
	start := min(a.Cursor, len(p.preview.Files))
	end := min(start+a.Limit, len(p.preview.Files))
	next := -1
	if end < len(p.preview.Files) {
		next = end
	}
	rows := []map[string]any{}
	for _, file := range p.preview.Files[start:end] {
		rows = append(rows, map[string]any{"entryKey": file.Path, "path": file.Path, "required": file.Required, "dependencyCount": file.DependencyCount})
	}
	return map[string]any{"exportPlanId": id, "rows": rows, "total": len(p.preview.Files), "nextCursor": next, "warnings": exportWarningRows(p.preview.Warnings)}, nil
}
func (s *MCPService) exportExecute(a mcpArgs) (any, error) {
	if err := absolutePath(a.OutputParent); err != nil {
		return nil, err
	}
	s.mu.Lock()
	id := s.exportID
	s.mu.Unlock()
	if id == "" || id != a.ExportPlanID {
		return nil, fmt.Errorf("PLAN_NOT_FOUND")
	}
	s.export.mu.Lock()
	p := s.export.plan
	if p == nil || !s.export.validLockedSafe() {
		s.export.mu.Unlock()
		return nil, ErrExportStale
	}
	known := map[string]bool{}
	for _, f := range p.preview.Files {
		known[f.Path] = true
	}
	selected := map[string]bool{}
	for _, key := range a.SelectedEntryKeys {
		if !known[key] {
			s.export.mu.Unlock()
			return nil, fmt.Errorf("INVALID_SELECTION")
		}
		selected[key] = true
	}
	excluded := []string{}
	for _, f := range p.preview.Files {
		if !f.Required && !selected[f.Path] {
			excluded = append(excluded, f.Path)
		}
	}
	if len(selected) == 0 {
		s.export.mu.Unlock()
		return nil, fmt.Errorf("EMPTY_SELECTION")
	}
	ack := map[string]bool{}
	for _, w := range a.AcknowledgedWarningIDs {
		ack[w] = true
	}
	for i := range p.preview.Warnings {
		if !ack[fmt.Sprintf("w%d", i)] {
			s.export.mu.Unlock()
			return nil, fmt.Errorf("WARNINGS_PENDING: 请先查看并回传 warningId")
		}
	}
	if p.request.IncludeDependencies && len(excluded) > 0 && !ack["selection-dependencies"] {
		s.export.mu.Unlock()
		return nil, fmt.Errorf("WARNINGS_PENDING: warningId=selection-dependencies，排除文件可能移除依赖，服务不会自动补回")
	}
	mode := p.request.Mode
	name := p.request.Name
	s.export.mu.Unlock()
	parent, err := filepath.EvalSymlinks(a.OutputParent)
	if err != nil {
		return nil, err
	}
	if mode == "direct" {
		name = a.OutputName
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
			return nil, fmt.Errorf("INVALID_ARGUMENT: outputName 需要单个目录名")
		}
	}
	dest := filepath.Join(parent, name)
	if _, err = os.Lstat(dest); err == nil {
		return nil, fmt.Errorf("TARGET_EXISTS")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	var output string
	if mode == "direct" {
		temp, err := os.MkdirTemp(parent, ".pvfine-export-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(temp)
		// The underlying export consumes its plan on success; directory publication
		// failure must therefore be reported distinctly and never blindly retried.
		if _, err = s.export.executeSelectedTo(id, temp, excluded); err != nil {
			return nil, err
		}
		if err = publishModDirectory(temp, dest); err != nil {
			return nil, fmt.Errorf("PUBLISH_FAILED: 输出未发布，请重新准备导出: %w", err)
		}
		output = dest
	} else {
		output, err = s.export.executeSelectedTo(id, parent, excluded)
		if err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	s.exportID = ""
	s.mu.Unlock()
	emitEvent("mcp:exported", map[string]any{"outputPath": output, "mode": mode})
	return map[string]any{"outputPath": output, "mode": mode, "published": true}, nil
}

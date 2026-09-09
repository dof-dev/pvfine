package services

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

const (
	BatchModeText       = "text"
	BatchModeStructured = "structured"

	BatchFileChanged = "changed"
	BatchFileSkipped = "skipped"
	BatchFileError   = "error"

	batchPreviewPageSize = 100
	batchMaxDiffLines    = 2000
	batchDiffContext     = 2
)

var batchPlanSequence uint64

// BatchService previews and applies multi-file edits against the current PVF.
// Preview plans live only in memory and are invalidated by every archive
// content mutation.
type BatchService struct{ c *core }

func NewBatchService(c *core) *BatchService { return &BatchService{c: c} }

type BatchRequest struct {
	Mode       string                `json:"mode"`
	Paths      []string              `json:"paths"`
	Text       *TextReplaceSpec      `json:"text,omitempty"`
	Operations []StructuredOperation `json:"operations,omitempty"`
}

type TextReplaceSpec struct {
	Find        string `json:"find"`
	Replacement string `json:"replacement"`
	Regex       bool   `json:"regex"`
}

type StructuredOperation struct {
	Kind            string `json:"kind"`
	Section         string `json:"section"`
	TokenIndex      int    `json:"tokenIndex"`
	Value           string `json:"value"`
	CreateIfMissing bool   `json:"createIfMissing"`
	Operator        string `json:"operator"`
	Operand         string `json:"operand"`
	OperandEnd      string `json:"operandEnd"`
	AnchorSection   string `json:"anchorSection"`
	HasEndTag       bool   `json:"hasEndTag"`
}

type BatchPreviewPage struct {
	PlanID             string              `json:"planId"`
	NextCursor         int                 `json:"nextCursor"`
	RequestedFiles     int                 `json:"requestedFiles"`
	MatchedFiles       int                 `json:"matchedFiles"`
	MatchedOccurrences int                 `json:"matchedOccurrences"`
	ChangedFiles       int                 `json:"changedFiles"`
	Rows               []*BatchFilePreview `json:"rows"`
}

type BatchFilePreview struct {
	FileIndex     int32            `json:"fileIndex"`
	Path          string           `json:"path"`
	Status        string           `json:"status"`
	MatchCount    int              `json:"matchCount"`
	Reason        string           `json:"reason,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
	Diff          []*BatchDiffLine `json:"diff,omitempty"`
	DiffTruncated bool             `json:"diffTruncated,omitempty"`
}

type BatchDiffLine struct {
	Kind    string `json:"kind"`
	Text    string `json:"text"`
	OldLine int    `json:"oldLine"`
	NewLine int    `json:"newLine"`
}

type BatchApplyResult struct {
	AppliedFiles  int32   `json:"appliedFiles"`
	FileIndexes   []int32 `json:"fileIndexes"`
	ModifiedCount int     `json:"modifiedCount"`
	Revision      uint64  `json:"revision"`
}

type batchPlan struct {
	id                 string
	archive            *pvf.Archive
	revision           uint64
	staged             *pvf.Archive
	rows               []batchPlanRow
	requestedFiles     int
	matchedFiles       int
	matchedOccurrences int
	changedFiles       int
}

type batchPlanRow struct {
	preview   BatchFilePreview
	afterText string
}

type batchFileTarget struct {
	index int32
	path  string
}

// Preview builds an in-memory plan and returns its first page. It does not
// modify the live archive or any editor overlay.
func (s *BatchService) Preview(request BatchRequest) (*BatchPreviewPage, error) {
	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	if mode != BatchModeText && mode != BatchModeStructured {
		return nil, fmt.Errorf("未知批处理模式: %s", request.Mode)
	}
	if len(request.Paths) == 0 {
		return nil, fmt.Errorf("没有可处理的文件")
	}
	if err := validateBatchRequest(mode, request); err != nil {
		return nil, err
	}

	s.c.mu.RLock()
	a := s.c.archive
	if a == nil {
		s.c.mu.RUnlock()
		return nil, ErrNoArchive
	}
	revision := s.c.batchRevision
	targets := resolveBatchTargets(a, request.Paths)
	stage := a.CloneForBatch()
	s.c.mu.RUnlock()
	if len(targets) == 0 {
		return nil, fmt.Errorf("没有可处理的文件")
	}

	plan := &batchPlan{
		id:             nextBatchPlanID(),
		archive:        a,
		revision:       revision,
		staged:         stage,
		requestedFiles: len(request.Paths),
	}
	if err := buildBatchPlan(plan, stage, targets, mode, request); err != nil {
		return nil, err
	}

	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.archive != a || s.c.batchRevision != revision {
		return nil, ErrBatchPlanStale
	}
	s.c.batchPlan = plan
	return batchPreviewPageFromPlan(plan, 0, batchPreviewPageSize), nil
}

// PreviewPage returns another page from the currently retained preview plan.
func (s *BatchService) PreviewPage(planID string, cursor, limit int) (*BatchPreviewPage, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	plan, err := s.currentBatchPlanLocked(planID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = batchPreviewPageSize
	}
	return batchPreviewPageFromPlan(plan, cursor, limit), nil
}

// Apply commits exactly the selected changed rows from a preview plan. An
// empty selection is rejected so the UI cannot accidentally report success for
// a no-op.
func (s *BatchService) Apply(planID string, fileIndexes []int32) (BatchApplyResult, error) {
	s.c.mu.Lock()
	plan, err := s.currentBatchPlanLocked(planID)
	if err != nil {
		s.c.mu.Unlock()
		return BatchApplyResult{}, err
	}
	if len(fileIndexes) == 0 {
		s.c.mu.Unlock()
		return BatchApplyResult{}, fmt.Errorf("没有选中的变更文件")
	}

	rowsByIndex := make(map[int32]*batchPlanRow, len(plan.rows))
	for index := range plan.rows {
		row := &plan.rows[index]
		if row.preview.FileIndex >= 0 {
			rowsByIndex[row.preview.FileIndex] = row
		}
	}
	selected := make(map[int32]struct{}, len(fileIndexes))
	ordered := make([]int32, 0, len(fileIndexes))
	for _, fileIndex := range fileIndexes {
		if _, exists := selected[fileIndex]; exists {
			continue
		}
		row, exists := rowsByIndex[fileIndex]
		if !exists || row.preview.Status != BatchFileChanged {
			s.c.mu.Unlock()
			return BatchApplyResult{}, fmt.Errorf("文件 %d 不是可应用的批处理结果", fileIndex)
		}
		selected[fileIndex] = struct{}{}
		ordered = append(ordered, fileIndex)
	}
	var before pvfversion.ContentSnapshot
	if s.c.versionRepo != nil {
		paths := make([]string, 0, len(ordered))
		for _, fileIndex := range ordered {
			paths = append(paths, plan.archive.Path(fileIndex))
		}
		before, err = pvfversion.ContentSnapshotFromArchive(plan.archive, paths)
		if err != nil {
			s.c.mu.Unlock()
			return BatchApplyResult{}, err
		}
	}
	if err := plan.archive.CommitBatch(plan.staged, selected); err != nil {
		s.c.mu.Unlock()
		return BatchApplyResult{}, err
	}
	if s.c.versionRepo != nil {
		paths := make([]string, 0, len(ordered))
		for _, fileIndex := range ordered {
			paths = append(paths, plan.archive.Path(fileIndex))
		}
		after, snapshotErr := pvfversion.ContentSnapshotFromArchive(plan.archive, paths)
		if snapshotErr != nil {
			s.c.mu.Unlock()
			return BatchApplyResult{}, snapshotErr
		}
		if recordErr := s.c.recordVersionMutationLocked("批量操作", before, after); recordErr != nil {
			s.c.mu.Unlock()
			return BatchApplyResult{}, recordErr
		}
	}

	if s.c.editorText == nil {
		s.c.editorText = make(map[int32]string)
	}
	for _, fileIndex := range ordered {
		row := rowsByIndex[fileIndex]
		s.c.editorText[fileIndex] = row.afterText
	}
	s.c.batchRevision++
	s.c.batchPlan = nil
	s.c.editorAnnotation = editorAnnotationCache{}
	s.c.annotationRelations = make(map[string]map[string]*relationTarget)
	s.c.invalidateAdvancedSearchLocked()
	info := plan.archive.Info()
	revision := s.c.batchRevision
	versioned := s.c.versionRepo != nil
	s.c.mu.Unlock()

	emitEvent("archive:advanced-search-stale", map[string]any{"batch": true})
	emitEvent("archive:batch-applied", map[string]any{
		"fileIndexes":   ordered,
		"modifiedCount": info.ModifiedCount,
		"revision":      revision,
	})
	if versioned {
		emitVersionState(s.c, "batch-applied")
	}
	// One rebuild updates all affected names/tags and avoids emitting one index
	// invalidation per file.
	s.c.startSearchIndex()
	return BatchApplyResult{
		AppliedFiles:  int32(len(ordered)),
		FileIndexes:   ordered,
		ModifiedCount: info.ModifiedCount,
		Revision:      revision,
	}, nil
}

func (s *BatchService) currentBatchPlanLocked(planID string) (*batchPlan, error) {
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	plan := s.c.batchPlan
	if plan == nil || plan.id != planID || plan.archive != s.c.archive || plan.revision != s.c.batchRevision {
		return nil, ErrBatchPlanStale
	}
	return plan, nil
}

func nextBatchPlanID() string {
	return fmt.Sprintf("batch-%d", atomic.AddUint64(&batchPlanSequence, 1))
}

func validateBatchRequest(mode string, request BatchRequest) error {
	switch mode {
	case BatchModeText:
		if request.Text == nil {
			return fmt.Errorf("文本批处理参数为空")
		}
		if request.Text.Find == "" {
			return fmt.Errorf("查找内容不能为空")
		}
		if request.Text.Regex {
			re, err := regexp.Compile(request.Text.Find)
			if err != nil {
				return fmt.Errorf("正则表达式无效: %w", err)
			}
			if err := validateBatchReplacement(re, request.Text.Replacement); err != nil {
				return err
			}
		}
	case BatchModeStructured:
		if len(request.Operations) == 0 {
			return fmt.Errorf("至少需要一条结构化操作")
		}
		for index, operation := range request.Operations {
			kind := strings.ToLower(strings.TrimSpace(operation.Kind))
			if kind == "" {
				return fmt.Errorf("第 %d 条操作类型为空", index+1)
			}
			if kind != "insert" && normalizeBatchSectionName(operation.Section) == "" {
				return fmt.Errorf("第 %d 条操作 section 为空", index+1)
			}
		}
	}
	return nil
}

func validateBatchReplacement(re *regexp.Regexp, replacement string) error {
	for index := 0; index < len(replacement); index++ {
		if replacement[index] != '$' {
			continue
		}
		if index+1 >= len(replacement) {
			return fmt.Errorf("替换文本末尾的 $ 无效,请使用 $$ 表示字面量")
		}
		index++
		if replacement[index] == '$' {
			continue
		}
		if replacement[index] == '{' {
			end := strings.IndexByte(replacement[index+1:], '}')
			if end < 0 {
				return fmt.Errorf("替换捕获组缺少 }")
			}
			name := replacement[index+1 : index+1+end]
			if re.SubexpIndex(name) < 0 {
				return fmt.Errorf("替换捕获组不存在: %s", name)
			}
			index += end + 1
			continue
		}
		start := index
		if replacement[index] >= '0' && replacement[index] <= '9' {
			for index+1 < len(replacement) && replacement[index+1] >= '0' && replacement[index+1] <= '9' {
				index++
			}
			group, _ := strconv.Atoi(replacement[start : index+1])
			if group > re.NumSubexp() {
				return fmt.Errorf("替换捕获组不存在: $%d", group)
			}
			continue
		}
		if isBatchNameStart(replacement[index]) {
			for index+1 < len(replacement) && isBatchNamePart(replacement[index+1]) {
				index++
			}
			name := replacement[start : index+1]
			if re.SubexpIndex(name) < 0 {
				return fmt.Errorf("替换捕获组不存在: $%s", name)
			}
			continue
		}
		return fmt.Errorf("替换文本中的 $ 用法无效")
	}
	return nil
}

func isBatchNameStart(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isBatchNamePart(value byte) bool {
	return isBatchNameStart(value) || value >= '0' && value <= '9'
}

func resolveBatchTargets(a *pvf.Archive, paths []string) []batchFileTarget {
	seen := make(map[int32]struct{}, len(paths))
	seenMissing := make(map[string]struct{}, len(paths))
	result := make([]batchFileTarget, 0, len(paths))
	for _, rawPath := range paths {
		path := normalizeBatchPath(rawPath)
		if path == "" {
			continue
		}
		index, ok := a.Find(path)
		if !ok {
			if _, exists := seenMissing[path]; exists {
				continue
			}
			seenMissing[path] = struct{}{}
			result = append(result, batchFileTarget{index: -1, path: path})
			continue
		}
		if _, exists := seen[index]; exists {
			continue
		}
		seen[index] = struct{}{}
		result = append(result, batchFileTarget{index: index, path: a.Path(index)})
	}
	sort.SliceStable(result, func(left, right int) bool {
		return strings.ToLower(result[left].path) < strings.ToLower(result[right].path)
	})
	return result
}

func normalizeBatchPath(path string) string {
	return strings.Trim(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"), "/")
}

func normalizeBatchSectionName(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") && len(name) > 2 {
		name = name[1 : len(name)-1]
	}
	return strings.TrimPrefix(name, "/")
}

func buildBatchPlan(plan *batchPlan, stage *pvf.Archive, targets []batchFileTarget, mode string, request BatchRequest) error {
	operations := make([]pvf.StructuredBatchOperation, 0, len(request.Operations))
	for _, operation := range request.Operations {
		if operation.TokenIndex < 0 && strings.ToLower(strings.TrimSpace(operation.Kind)) != "set" {
			operation.TokenIndex = 0
		}
		operations = append(operations, pvf.StructuredBatchOperation{
			Kind:            operation.Kind,
			Section:         operation.Section,
			TokenIndex:      operation.TokenIndex,
			Value:           operation.Value,
			CreateIfMissing: operation.CreateIfMissing,
			Operator:        operation.Operator,
			Operand:         operation.Operand,
			OperandEnd:      operation.OperandEnd,
			AnchorSection:   operation.AnchorSection,
			HasEndTag:       operation.HasEndTag,
		})
	}

	for _, target := range targets {
		row := batchPlanRow{preview: BatchFilePreview{
			FileIndex: target.index,
			Path:      target.path,
		}}
		if target.index < 0 {
			row.preview.Status = BatchFileSkipped
			row.preview.Reason = "当前归档不存在"
			plan.rows = append(plan.rows, row)
			continue
		}

		file := stage.File(target.index)
		var matchCount int
		var warning []string
		var beforeText string
		var afterText string
		var changed bool
		var err error
		switch mode {
		case BatchModeText:
			if file.DataType != pvf.TypeScript && file.DataType != pvf.TypeUnicode {
				row.preview.Status = BatchFileSkipped
				row.preview.Reason = fmt.Sprintf("不支持文本编辑的数据类型 %d", file.DataType)
				plan.rows = append(plan.rows, row)
				continue
			}
			before, readErr := stage.Text(target.index)
			if readErr != nil {
				err = readErr
				break
			}
			beforeText = before
			var after string
			after, matchCount, err = replaceBatchText(before, *request.Text)
			if err == nil && matchCount > 0 && after != before {
				if err = stage.SetText(target.index, after); err == nil {
					afterText = after
					changed = true
				}
			}
			if err == nil && matchCount > 0 && !changed {
				row.preview.Status = BatchFileSkipped
				row.preview.Reason = "匹配成功,但替换后内容未变化"
			}
		case BatchModeStructured:
			if file.DataType != pvf.TypeScript {
				row.preview.Status = BatchFileSkipped
				row.preview.Reason = "结构化操作只支持 TypeScript"
				plan.rows = append(plan.rows, row)
				continue
			}
			raw, readErr := stage.RawBytes(target.index)
			if readErr != nil {
				err = readErr
				break
			}
			beforeText, err = stage.Text(target.index)
			if err != nil {
				break
			}
			var transformed pvf.BatchTransformResult
			transformed, err = stage.TransformStructuredBatch(raw, operations)
			if err == nil {
				matchCount = transformed.MatchCount()
				warning = transformed.Warnings()
				changed = transformed.Changed()
				if changed {
					if err = stage.SetRawBytes(target.index, transformed.Raw()); err == nil {
						afterText, err = stage.Text(target.index)
					}
				}
			}
			if err == nil && !changed {
				row.preview.Status = BatchFileSkipped
				if len(warning) > 0 {
					row.preview.Reason = strings.Join(warning, "；")
				} else {
					row.preview.Reason = "操作未产生变化"
				}
			}
		}

		row.preview.MatchCount = matchCount
		row.preview.Warnings = append([]string(nil), warning...)
		if err != nil {
			row.preview.Status = BatchFileError
			row.preview.Reason = err.Error()
			plan.rows = append(plan.rows, row)
			continue
		}
		if matchCount > 0 {
			plan.matchedFiles++
			plan.matchedOccurrences += matchCount
		}
		if changed {
			row.preview.Status = BatchFileChanged
			row.afterText = afterText
			row.preview.Diff, row.preview.DiffTruncated = buildBatchDiff(beforeText, afterText)
			plan.changedFiles++
		}
		if row.preview.Status != "" {
			plan.rows = append(plan.rows, row)
		}
	}
	return nil
}

func replaceBatchText(text string, spec TextReplaceSpec) (string, int, error) {
	if spec.Regex {
		re, err := regexp.Compile(spec.Find)
		if err != nil {
			return "", 0, fmt.Errorf("正则表达式无效: %w", err)
		}
		matches := re.FindAllStringIndex(text, -1)
		return re.ReplaceAllString(text, spec.Replacement), len(matches), nil
	}
	return strings.ReplaceAll(text, spec.Find, spec.Replacement), strings.Count(text, spec.Find), nil
}

func batchPreviewPageFromPlan(plan *batchPlan, cursor, limit int) *BatchPreviewPage {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(plan.rows) {
		cursor = len(plan.rows)
	}
	if limit <= 0 {
		limit = batchPreviewPageSize
	}
	end := cursor + limit
	if end > len(plan.rows) {
		end = len(plan.rows)
	}
	rows := make([]*BatchFilePreview, 0, end-cursor)
	for index := cursor; index < end; index++ {
		preview := plan.rows[index].preview
		preview.Warnings = append([]string(nil), preview.Warnings...)
		preview.Diff = append([]*BatchDiffLine(nil), preview.Diff...)
		rows = append(rows, &preview)
	}
	next := -1
	if end < len(plan.rows) {
		next = end
	}
	return &BatchPreviewPage{
		PlanID:             plan.id,
		NextCursor:         next,
		RequestedFiles:     plan.requestedFiles,
		MatchedFiles:       plan.matchedFiles,
		MatchedOccurrences: plan.matchedOccurrences,
		ChangedFiles:       plan.changedFiles,
		Rows:               rows,
	}
}

type batchDiffOp struct {
	kind byte
	text string
}

func buildBatchDiff(before, after string) ([]*BatchDiffLine, bool) {
	oldLines := splitBatchLines(before)
	newLines := splitBatchLines(after)
	ops := batchDiffOps(oldLines, newLines)
	if len(ops) == 0 {
		return nil, false
	}
	changed := make([]bool, len(ops))
	for index, op := range ops {
		changed[index] = op.kind != '='
	}
	for index, op := range ops {
		if op.kind != '=' {
			start := index - batchDiffContext
			if start < 0 {
				start = 0
			}
			end := index + batchDiffContext + 1
			if end > len(ops) {
				end = len(ops)
			}
			for context := start; context < end; context++ {
				changed[context] = true
			}
		}
	}

	result := make([]*BatchDiffLine, 0)
	oldLine, newLine := 1, 1
	for index, op := range ops {
		line := &BatchDiffLine{Text: op.text, OldLine: oldLine, NewLine: newLine}
		switch op.kind {
		case '=':
			line.Kind = "context"
			oldLine++
			newLine++
		case '-':
			line.Kind = "remove"
			oldLine++
		case '+':
			line.Kind = "add"
			newLine++
		}
		if changed[index] {
			result = append(result, line)
		}
	}
	if len(result) <= batchMaxDiffLines {
		return result, false
	}
	return result[:batchMaxDiffLines], true
}

func splitBatchLines(text string) []string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func batchDiffOps(oldLines, newLines []string) []batchDiffOp {
	// The bounded dynamic-programming path gives compact diffs for normal
	// scripts. Very large changed regions fall back to a safe remove/add block
	// rather than allocating a quadratic matrix.
	if len(oldLines) == 0 {
		result := make([]batchDiffOp, 0, len(newLines))
		for _, line := range newLines {
			result = append(result, batchDiffOp{kind: '+', text: line})
		}
		return result
	}
	if len(newLines) == 0 {
		result := make([]batchDiffOp, 0, len(oldLines))
		for _, line := range oldLines {
			result = append(result, batchDiffOp{kind: '-', text: line})
		}
		return result
	}
	if len(oldLines) > 1200 || len(newLines) > 1200 || len(oldLines)*len(newLines) > 900000 {
		result := make([]batchDiffOp, 0, len(oldLines)+len(newLines))
		for _, line := range oldLines {
			result = append(result, batchDiffOp{kind: '-', text: line})
		}
		for _, line := range newLines {
			result = append(result, batchDiffOp{kind: '+', text: line})
		}
		return result
	}

	rows := make([][]int, len(oldLines)+1)
	for row := range rows {
		rows[row] = make([]int, len(newLines)+1)
	}
	for row := len(oldLines) - 1; row >= 0; row-- {
		for column := len(newLines) - 1; column >= 0; column-- {
			if oldLines[row] == newLines[column] {
				rows[row][column] = rows[row+1][column+1] + 1
			} else if rows[row+1][column] >= rows[row][column+1] {
				rows[row][column] = rows[row+1][column]
			} else {
				rows[row][column] = rows[row][column+1]
			}
		}
	}

	result := make([]batchDiffOp, 0, len(oldLines)+len(newLines))
	row, column := 0, 0
	for row < len(oldLines) && column < len(newLines) {
		if oldLines[row] == newLines[column] {
			result = append(result, batchDiffOp{kind: '=', text: oldLines[row]})
			row++
			column++
		} else if rows[row+1][column] >= rows[row][column+1] {
			result = append(result, batchDiffOp{kind: '-', text: oldLines[row]})
			row++
		} else {
			result = append(result, batchDiffOp{kind: '+', text: newLines[column]})
			column++
		}
	}
	for row < len(oldLines) {
		result = append(result, batchDiffOp{kind: '-', text: oldLines[row]})
		row++
	}
	for column < len(newLines) {
		result = append(result, batchDiffOp{kind: '+', text: newLines[column]})
		column++
	}
	return result
}

package services

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

const (
	worldDropPath         = "etc/worlddrop.etc"
	regionalWorldDropPath = "etc/(r)worlddrop.etc"
)

type WorldDropItem struct {
	ID     int32  `json:"id"`
	Weight int32  `json:"weight"`
	Name   string `json:"name"`
}

type WorldDropLevel struct {
	Level int32           `json:"level"`
	Items []WorldDropItem `json:"items"`
}

type WorldDropDocument struct {
	Revision uint64           `json:"revision"`
	Levels   []WorldDropLevel `json:"levels"`
}

type WorldDropEditRequest struct {
	FileIndex int32            `json:"fileIndex"`
	Path      string           `json:"path"`
	Text      string           `json:"text"`
	Revision  uint64           `json:"revision"`
	Levels    []WorldDropLevel `json:"levels"`
}

type WorldDropEditResult struct {
	Revision      uint64           `json:"revision"`
	Files         []ShopEditedFile `json:"files"`
	ModifiedCount int              `json:"modifiedCount"`
}

func validateWorldDropFile(a *pvf.Archive, index int32, path string) error {
	if err := validateAnnotationIndex(a, index); err != nil {
		return err
	}
	filePath := a.Path(index)
	if (!sameSearchPath(filePath, worldDropPath) && !sameSearchPath(filePath, regionalWorldDropPath)) ||
		(path != "" && !sameSearchPath(path, filePath)) {
		return fmt.Errorf("全局掉率文件已变化")
	}
	if a.File(index).DataType != pvf.TypeScript {
		return fmt.Errorf("全局掉率文件不是脚本类型")
	}
	return nil
}

// ReadWorldDrop parses the caller's current editor draft, then resolves names
// against the archive's item relation index. Unknown IDs remain editable.
func (s *FileGUIService) ReadWorldDrop(fileIndex int32, text string) (*WorldDropDocument, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	if err := validateWorldDropFile(s.c.archive, fileIndex, ""); err != nil {
		return nil, err
	}
	if len(text) > maxEditableBytes {
		return nil, fmt.Errorf("全局掉率文本过大")
	}
	levels, err := parseWorldDrop(text)
	if err != nil {
		return nil, err
	}
	names := make(map[int32]string)
	for li := range levels {
		for ii := range levels[li].Items {
			item := &levels[li].Items[ii]
			if name, ok := names[item.ID]; ok {
				item.Name = name
				continue
			}
			name := fmt.Sprintf("未知物品 #%d", item.ID)
			if item.ID <= 0 || s.c.annotationEngine == nil {
				item.Name = name
				names[item.ID] = name
				continue
			}
			if ref, ok := s.c.resolveAnnotationReferenceLocked("物品", strconv.FormatInt(int64(item.ID), 10)); ok && ref.Name != "" {
				name = resolvePreviewText(s.c.archive, ref.Name)
			}
			item.Name = name
			names[item.ID] = name
		}
	}
	return &WorldDropDocument{Revision: s.c.batchRevision, Levels: levels}, nil
}

func worldDropLine(text string, offset int) int {
	units := utf16.Encode([]rune(text))
	if offset > len(units) {
		offset = len(units)
	}
	line := 1
	for _, unit := range units[:offset] {
		if unit == '\n' {
			line++
		}
	}
	return line
}

func parseWorldDrop(text string) ([]WorldDropLevel, error) {
	view := pvf.ParseScriptView(text)
	sections := 0
	values := make([]pvf.ScriptElement, 0)
	for _, element := range view.Elements {
		if element.Kind == pvf.ScriptElementSection {
			sections++
			if !strings.EqualFold(element.Section, "world drop") {
				return nil, fmt.Errorf("第 %d 行：只允许 [world drop] section", worldDropLine(view.Text, element.Start))
			}
			continue
		}
		if element.Kind == pvf.ScriptElementToken {
			if element.SectionID == 0 {
				return nil, fmt.Errorf("第 %d 行：值位于 [world drop] 外", worldDropLine(view.Text, element.Start))
			}
			values = append(values, element)
		}
	}
	if sections != 1 {
		return nil, fmt.Errorf("文件必须且只能包含一个 [world drop] section")
	}
	levels := make([]WorldDropLevel, 0)
	for cursor := 0; cursor < len(values); {
		start := values[cursor]
		parseNumber := func(at int, label string) (int32, error) {
			if at >= len(values) {
				return 0, fmt.Errorf("第 %d 行：缺少%s", worldDropLine(view.Text, start.Start), label)
			}
			value := values[at]
			n, err := strconv.ParseInt(value.Value, 10, 32)
			if value.TokenType != 0 || err != nil {
				return 0, fmt.Errorf("第 %d 行：%s必须是整数", worldDropLine(view.Text, value.Start), label)
			}
			return int32(n), nil
		}
		level, err := parseNumber(cursor, "等级")
		if err != nil {
			return nil, err
		}
		if level <= 0 {
			return nil, fmt.Errorf("第 %d 行：等级必须是正整数", worldDropLine(view.Text, start.Start))
		}
		for _, existing := range levels {
			if existing.Level == level {
				return nil, fmt.Errorf("第 %d 行：等级 %d 重复", worldDropLine(view.Text, start.Start), level)
			}
		}
		zero, err := parseNumber(cursor+1, "等级后的固定值 0")
		if err != nil {
			return nil, err
		}
		if zero != 0 {
			return nil, fmt.Errorf("第 %d 行：等级后的固定值必须是 0", worldDropLine(view.Text, values[cursor+1].Start))
		}
		cursor += 2
		items := make([]WorldDropItem, 0)
		terminated := false
		for cursor < len(values) {
			id, err := parseNumber(cursor, "道具 ID 或结束标记")
			if err != nil {
				return nil, err
			}
			if id == -1 {
				cursor++
				terminated = true
				break
			}
			weight, err := parseNumber(cursor+1, "道具权重")
			if err != nil {
				return nil, err
			}
			if weight < 0 {
				return nil, fmt.Errorf("第 %d 行：道具权重不能为负数", worldDropLine(view.Text, values[cursor+1].Start))
			}
			items = append(items, WorldDropItem{ID: id, Weight: weight})
			cursor += 2
		}
		if !terminated {
			return nil, fmt.Errorf("第 %d 行：等级 %d 缺少结束标记 -1", worldDropLine(view.Text, start.Start), level)
		}
		levels = append(levels, WorldDropLevel{Level: level, Items: items})
	}
	return levels, nil
}

func validateWorldDropLevels(levels, previous []WorldDropLevel) error {
	seen := make(map[int32]bool, len(levels))
	legacyIDs := make(map[int32]map[int32]int, len(previous))
	for _, level := range previous {
		for _, item := range level.Items {
			if item.ID > 0 {
				continue
			}
			if legacyIDs[level.Level] == nil {
				legacyIDs[level.Level] = make(map[int32]int)
			}
			legacyIDs[level.Level][item.ID]++
		}
	}
	for _, level := range levels {
		if level.Level <= 0 || seen[level.Level] {
			return fmt.Errorf("等级必须是互不重复的正整数")
		}
		seen[level.Level] = true
		for _, item := range level.Items {
			if item.ID <= 0 {
				if legacyIDs[level.Level][item.ID] == 0 {
					return fmt.Errorf("等级 %d 不能新增非正数道具 ID %d", level.Level, item.ID)
				}
				legacyIDs[level.Level][item.ID]--
			}
			if item.Weight < 0 {
				return fmt.Errorf("等级 %d 中道具权重不能为负数", level.Level)
			}
		}
	}
	return nil
}

func renderWorldDrop(levels []WorldDropLevel) string {
	var text strings.Builder
	text.WriteString("[world drop]\n")
	for _, level := range levels {
		fmt.Fprintf(&text, "\t%d\t0\n", level.Level)
		for _, item := range level.Items {
			fmt.Fprintf(&text, "\t%d\t%d\n", item.ID, item.Weight)
		}
		text.WriteString("\t-1\n")
	}
	return text.String()
}

// ApplyWorldDropEdit publishes one complete validated snapshot to the archive
// overlay. Like shop edits, a stale revision or changed editor draft is rejected.
func (s *FileGUIService) ApplyWorldDropEdit(req WorldDropEditRequest) (*WorldDropEditResult, error) {
	s.c.mu.Lock()
	result, archive, summary, err := s.applyWorldDropEditLocked(req)
	s.c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if len(result.Files) > 0 {
		s.c.scheduleArchiveMutations(archive, summary)
		emitEvent("archive:batch-applied", map[string]any{"source": "world-drop-gui", "structural": false, "fileIndexes": []int32{req.FileIndex}, "revision": result.Revision, "modifiedCount": result.ModifiedCount})
		emitEvent("archive:advanced-search-stale", map[string]any{"worldDrop": true})
		emitVersionState(s.c, "world-drop-edited")
	}
	return result, nil
}

func (s *FileGUIService) applyWorldDropEditLocked(req WorldDropEditRequest) (*WorldDropEditResult, *pvf.Archive, pvf.MutationSummary, error) {
	fail := func(err error) (*WorldDropEditResult, *pvf.Archive, pvf.MutationSummary, error) {
		return nil, nil, pvf.MutationSummary{}, err
	}
	a := s.c.archive
	if a == nil {
		return fail(ErrNoArchive)
	}
	if req.Revision != s.c.batchRevision {
		return fail(fmt.Errorf("全局掉率数据已变化，请重新加载"))
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		return fail(err)
	}
	if err := validateWorldDropFile(a, req.FileIndex, req.Path); err != nil {
		return fail(err)
	}
	if len(req.Text) > maxEditableBytes {
		return fail(fmt.Errorf("全局掉率文本过大"))
	}
	previous, err := parseWorldDrop(req.Text)
	if err != nil {
		return fail(err)
	}
	if err := validateWorldDropLevels(req.Levels, previous); err != nil {
		return fail(err)
	}
	if equalWorldDropLevels(previous, req.Levels) {
		return &WorldDropEditResult{Revision: s.c.batchRevision, Files: []ShopEditedFile{}, ModifiedCount: a.ModifiedCount()}, a, pvf.MutationSummary{}, nil
	}
	stage := a.CloneForBatch()
	newText := renderWorldDrop(req.Levels)
	if len(newText) > maxEditableBytes {
		return fail(fmt.Errorf("全局掉率文本过大"))
	}
	if err := stage.SetText(req.FileIndex, newText); err != nil {
		return fail(err)
	}
	beforeRaw, err := a.RawBytes(req.FileIndex)
	if err != nil {
		return fail(err)
	}
	afterRaw, err := stage.RawBytes(req.FileIndex)
	if err != nil {
		return fail(err)
	}
	result := &WorldDropEditResult{Files: []ShopEditedFile{}}
	if bytes.Equal(beforeRaw, afterRaw) {
		result.Revision = s.c.batchRevision
		result.ModifiedCount = a.ModifiedCount()
		return result, a, pvf.MutationSummary{}, nil
	}
	path := a.Path(req.FileIndex)
	var before, after pvfversion.ContentSnapshot
	if s.c.versionRepo != nil {
		before, err = pvfversion.ContentSnapshotFromArchive(a, []string{path})
		if err != nil {
			return fail(err)
		}
		after, err = pvfversion.ContentSnapshotFromArchive(stage, []string{path})
		if err != nil {
			return fail(err)
		}
	}
	checkpoint := a.MutationCheckpoint()
	changed := map[int32]struct{}{req.FileIndex: {}}
	if s.c.diskIndex != nil {
		if err := s.c.diskIndex.refreshFileMetadata(stage, changed); err != nil {
			return fail(err)
		}
	}
	change := pvf.ScriptChange{Kind: pvf.ChangeKindChanged, Path: path, Raw: afterRaw, DataType: a.File(req.FileIndex).DataType}
	if err := a.ApplyScriptChanges(stage, []pvf.ScriptChange{change}); err != nil {
		if s.c.diskIndex != nil {
			_ = s.c.diskIndex.refreshFileMetadata(a, changed)
		}
		return fail(err)
	}
	if s.c.diskIndex == nil {
		refreshArchiveIndexMetadataLocked(s.c, changed)
	}
	if err := s.c.recordVersionMutationLocked("全局掉率编辑", before, after); err != nil {
		return fail(err)
	}
	s.c.batchRevision++
	s.c.batchPlan = nil
	s.c.invalidateScriptLocked()
	s.c.invalidateAdvancedSearchLocked()
	s.c.editorAnnotation = editorAnnotationCache{}
	s.c.annotationRelations = make(map[string]map[string]*relationTarget)
	if s.c.editorText == nil {
		s.c.editorText = make(map[int32]string)
	}
	resultText, err := a.Text(req.FileIndex)
	if err != nil {
		return fail(err)
	}
	s.c.editorText[req.FileIndex] = resultText
	delete(s.c.visualsByFile, req.FileIndex)
	result.Files = append(result.Files, ShopEditedFile{FileIndex: req.FileIndex, Path: path, BeforeText: req.Text, Text: resultText})
	summary := a.MutationsSince(checkpoint)
	a.ClearMutations()
	result.Revision = s.c.batchRevision
	result.ModifiedCount = a.ModifiedCount()
	return result, a, summary, nil
}

func equalWorldDropLevels(left, right []WorldDropLevel) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].Level != right[i].Level || len(left[i].Items) != len(right[i].Items) {
			return false
		}
		for j := range left[i].Items {
			if left[i].Items[j].ID != right[i].Items[j].ID || left[i].Items[j].Weight != right[i].Items[j].Weight {
				return false
			}
		}
	}
	return true
}

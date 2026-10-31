package services

import (
	"fmt"
	"strconv"
	"strings"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

// ItemCreateRequest carries all relevant editor drafts, so unsaved list entries
// participate in collision checks and survive registration.
type ItemCreateRequest struct {
	Path     string      `json:"path"`
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Text     string      `json:"text"`
	Revision uint64      `json:"revision"`
	Drafts   []ShopDraft `json:"drafts"`
}
type ItemCreationCheck struct {
	Revision  uint64   `json:"revision"`
	ListPath  string   `json:"listPath"`
	EntryPath string   `json:"entryPath"`
	Conflicts []string `json:"conflicts"`
}
type ItemCreateResult struct {
	FileIndex int32            `json:"fileIndex"`
	Path      string           `json:"path"`
	ID        string           `json:"id"`
	Files     []ShopEditedFile `json:"files"`
}

func itemCreationPath(raw string) (string, string, error) {
	p, err := normalizeNewFilePath(raw)
	if err != nil {
		return "", "", err
	}
	lower := strings.ToLower(p)
	kind := ""
	if strings.HasPrefix(lower, "equipment/") && strings.HasSuffix(lower, ".equ") {
		kind = "equipment"
	}
	if strings.HasPrefix(lower, "stackable/") && strings.HasSuffix(lower, ".stk") {
		kind = "stackable"
	}
	if kind == "" || strings.ContainsAny(p, "`\r\n\x00:") {
		return "", "", fmt.Errorf("物品路径必须是 equipment/ 下的 .equ 或 stackable/ 下的 .stk")
	}
	return p, kind, nil
}

// itemCreationStageLocked only applies item-list/hash drafts; unrelated drafts
// are never silently saved by creation.
func (s *FileGUIService) itemCreationStageLocked(req ItemCreateRequest) (*pvf.Archive, *ItemCreationCheck, []int32, error) {
	a := s.c.archive
	if a == nil {
		return nil, nil, nil, ErrNoArchive
	}
	if !a.WriteCapabilities().CanChangeStructure {
		return nil, nil, nil, fmt.Errorf("当前归档不支持新增物品文件")
	}
	p, kind, err := itemCreationPath(req.Path)
	if err != nil {
		return nil, nil, nil, err
	}
	id, err := strconv.ParseUint(req.ID, 10, 31)
	if err != nil || strconv.FormatUint(id, 10) != req.ID || id == 0 {
		return nil, nil, nil, fmt.Errorf("物品 ID 必须是 1–2147483647 的整数，不能有前导零")
	}
	stage := a.CloneForBatch()
	relevant := make(map[int32]bool)
	lists := []int32{}
	// Check both layouts, including unusual archives that contain both lists.
	for _, candidate := range []string{"equipment/equipment.lst", "stackable/stackable.lst", "list/equipment.lst", "list/stackable.lst"} {
		if index, ok := a.Find(candidate); ok {
			relevant[index] = true
			lists = append(lists, index)
			if hp, ok := pvf.IndexHashCompanionPath(candidate); ok {
				if hi, found := a.Find(hp); found {
					relevant[hi] = true
				}
			}
		}
	}
	seen := map[int32]bool{}

	for _, draft := range req.Drafts {
		if !relevant[draft.FileIndex] {
			continue
		}
		if seen[draft.FileIndex] || !sameSearchPath(a.Path(draft.FileIndex), draft.Path) {
			return nil, nil, nil, fmt.Errorf("登记列表草稿已变化或重复")
		}
		seen[draft.FileIndex] = true
		if len(draft.Text) > int(s.c.editableByteLimit()) {
			return nil, nil, nil, fmt.Errorf("登记列表草稿过大")
		}
		if err := stage.SetText(draft.FileIndex, draft.Text); err != nil {
			return nil, nil, nil, err
		}
	}
	li, ok := stage.FindList(kind + "/" + kind + ".lst")
	if !ok {
		return nil, nil, nil, fmt.Errorf("归档缺少 %s.lst，无法登记物品", kind)
	}
	check := &ItemCreationCheck{Revision: s.c.batchRevision, ListPath: stage.Path(li), EntryPath: listEntryPath(stage.Path(li), p), Conflicts: []string{}}
	if _, exists := a.Find(p); exists {
		check.Conflicts = append(check.Conflicts, "目标文件已存在："+p)
	}
	for _, index := range lists {
		pairs, err := stage.ScriptListPairs(index)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("无法检查 %s：%w", stage.Path(index), err)
		}
		for _, pair := range pairs {
			n, parseErr := strconv.ParseUint(pair.ID, 10, 32)
			if parseErr == nil && n == id {
				check.Conflicts = append(check.Conflicts, fmt.Sprintf("ID 已在 %s 中使用：%s", stage.Path(index), pair.Path))
			}
			candidates, _ := listPathCandidates(stage.Path(index), pair.Path)
			for _, targetPath := range candidates {
				if sameSearchPath(targetPath, p) {
					check.Conflicts = append(check.Conflicts, "目标路径已有登记："+stage.Path(index))
					break
				}
			}
		}
	}
	if stage.ContentRules().RequiresIndexHash {
		hp, _ := pvf.IndexHashCompanionPath(check.ListPath)
		if _, ok := stage.Find(hp); !ok {
			return nil, nil, nil, fmt.Errorf("归档缺少 indexhash：%s", hp)
		}
	}
	// Discard the collision-check staging. Only target-list drafts may be consumed.
	target := map[int32]bool{li: true}
	if a.ContentRules().RequiresIndexHash {
		hp, _ := pvf.IndexHashCompanionPath(check.ListPath)
		if hi, found := a.Find(hp); found {
			target[hi] = true
		}
	}
	stage = a.CloneForBatch()
	for _, draft := range req.Drafts {
		if target[draft.FileIndex] {
			if err := stage.SetText(draft.FileIndex, draft.Text); err != nil {
				return nil, nil, nil, err
			}
		}
	}

	return stage, check, lists, nil
}

func (s *FileGUIService) CheckItemCreation(req ItemCreateRequest) (*ItemCreationCheck, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	_, check, _, err := s.itemCreationStageLocked(req)
	return check, err
}

// CreateItem stages the script, LST and (where required) indexhash before
// publishing one version operation. No PVF disk write happens here.
func (s *FileGUIService) CreateItem(req ItemCreateRequest) (*ItemCreateResult, error) {
	s.c.mu.Lock()
	fail := func(err error) (*ItemCreateResult, error) { s.c.mu.Unlock(); return nil, err }
	a := s.c.archive
	if a == nil {
		return fail(ErrNoArchive)
	}
	if req.Revision != s.c.batchRevision {
		return fail(fmt.Errorf("归档已变化，请重新检查 ID"))
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		return fail(err)
	}
	if strings.TrimSpace(req.Name) == "" || len([]rune(req.Name)) > 200 || strings.ContainsAny(req.Name, "\r\n\x00") {
		return fail(fmt.Errorf("物品名称需为 1–200 个字符，不能包含换行"))
	}
	if len(req.Text) > int(s.c.editableByteLimit()) {
		return fail(fmt.Errorf("参考物品文本过大"))
	}
	source, err := itemCreationSource(req.Text)
	if err != nil {
		return fail(err)
	}
	stage, check, _, err := s.itemCreationStageLocked(req)
	if err != nil {
		return fail(err)
	}
	if len(check.Conflicts) > 0 {
		return fail(fmt.Errorf("%s", strings.Join(check.Conflicts, "；")))
	}
	p, _, _ := itemCreationPath(req.Path)
	index, err := stage.AddFileText(p, source, pvf.TypeScript)
	if err != nil {
		return fail(err)
	}
	raw, err := stage.RawBytes(index)
	if err != nil {
		return fail(err)
	}
	doc, err := stage.ParseScriptDocument(raw)
	if err != nil {
		return fail(err)
	}
	if len(doc.Warnings()) > 0 {
		return fail(fmt.Errorf("参考物品结构不完整，请先修复 DSL"))
	}
	names := doc.Sections([]string{"name"})
	value, err := pvf.NewScriptValue(pvf.ScriptTokenQuoted, req.Name, pvf.ScriptPoolUTF16)
	if err != nil {
		return fail(err)
	}
	if len(names) > 1 {
		return fail(fmt.Errorf("参考物品包含重复名称字段"))
	}
	if len(names) == 1 {
		err = names[0].SetValues([]pvf.ScriptValue{value})
	} else {
		_, err = doc.AppendSection("name", []pvf.ScriptValue{value}, false)
	}
	if err != nil {
		return fail(err)
	}
	raw, err = stage.EncodeScriptDocument(doc)
	if err != nil {
		return fail(err)
	}
	if err := stage.SetRawBytes(index, raw); err != nil {
		return fail(err)
	}
	li, _ := stage.Find(check.ListPath)
	if err := stage.SetListPair(li, req.ID, check.EntryPath); err != nil {
		return fail(err)
	}
	if stage.ContentRules().RequiresIndexHash {
		hp, _ := pvf.IndexHashCompanionPath(check.ListPath)
		id, _ := strconv.ParseUint(req.ID, 10, 32)
		if err := stage.SetIndexHashEntriesForListIDs(hp, check.ListPath, []uint32{uint32(id)}); err != nil {
			return fail(err)
		}
	}
	paths := []string{p, check.ListPath}
	if stage.ContentRules().RequiresIndexHash {
		hp, _ := pvf.IndexHashCompanionPath(check.ListPath)
		paths = append(paths, hp)
	}
	// Capture consumed drafts in the result so the frontend can advance only
	// those baselines and preserve every unrelated editor draft.
	result := &ItemCreateResult{FileIndex: index, Path: p, ID: req.ID, Files: []ShopEditedFile{}}
	for _, changedPath := range paths[1:] {
		i, _ := a.Find(changedPath)
		before, err := a.Text(i)
		if err != nil {
			return fail(err)
		}
		for _, d := range req.Drafts {
			if d.FileIndex == i {
				before = d.Text
			}
		}
		after, err := stage.Text(i)
		if err != nil {
			return fail(err)
		}
		result.Files = append(result.Files, ShopEditedFile{FileIndex: i, Path: changedPath, BeforeText: before, Text: after})
	}
	var before, after pvfversion.ContentSnapshot
	if s.c.versionRepo != nil {
		before, err = pvfversion.ContentSnapshotFromArchive(a, paths)
		if err != nil {
			return fail(err)
		}
		after, err = pvfversion.ContentSnapshotFromArchive(stage, paths)
		if err != nil {
			return fail(err)
		}
	}
	revision := s.c.batchRevision
	incremental := s.c.diskIndex != nil && s.c.diskIndex.entries != nil
	s.c.mu.Unlock()
	var prepared *preparedArchiveIndexes
	if !incremental {
		prepared, err = prepareArchiveDerivedIndexes(stage)
	}
	s.c.mu.Lock()
	if err != nil {
		closePreparedArchiveIndexes(prepared)
		return fail(err)
	}
	if s.c.archive != a || s.c.batchRevision != revision {
		closePreparedArchiveIndexes(prepared)
		return fail(fmt.Errorf("归档已变化，请重新检查 ID"))
	}
	if incremental {
		err = s.c.rebuildArchiveIndexesLocked(stage)
	} else {
		err = s.c.installPreparedArchiveIndexesLocked(stage, prepared, true)
	}
	if err != nil {
		closePreparedArchiveIndexes(prepared)
		return fail(err)
	}
	if err := s.c.recordVersionMutationLocked("新建并登记物品", before, after); err != nil {
		return fail(err)
	}
	summary := stage.MutationsSince(0)
	stage.ClearMutations()
	info := stage.Info()
	s.c.mu.Unlock()
	if err := s.c.scheduleArchiveMutations(stage, summary); err != nil {
		return nil, err
	}
	emitEvent("archive:changed", info)
	emitEvent("archive:batch-applied", map[string]any{"source": "item-create", "structural": true})
	emitEvent("archive:advanced-search-stale", map[string]any{"item": true})
	emitVersionState(s.c, "item-created")
	return result, nil
}

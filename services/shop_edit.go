package services

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

type ShopMaterialInput struct {
	ItemID   string `json:"itemId"`
	Quantity string `json:"quantity"`
}
type ShopDraft struct {
	FileIndex int32  `json:"fileIndex"`
	Path      string `json:"path"`
	Text      string `json:"text"`
}
type ShopEditRequest struct {
	FileIndex    int32               `json:"fileIndex"`
	Path         string              `json:"path"`
	Text         string              `json:"text"`
	Revision     uint64              `json:"revision"`
	Action       string              `json:"action"`
	TabIndex     int                 `json:"tabIndex"`
	SourceStart  int                 `json:"sourceStart"`
	CategoryID   string              `json:"categoryId"`
	ItemID       string              `json:"itemId"`
	Name         string              `json:"name"`
	SetGold      bool                `json:"setGold"`
	Gold         string              `json:"gold"` // empty removes [price], "0" is an explicit zero
	SetMaterials bool                `json:"setMaterials"`
	Materials    []ShopMaterialInput `json:"materials"`
	Drafts       []ShopDraft         `json:"drafts"`
}
type ShopEditedFile struct {
	FileIndex  int32  `json:"fileIndex"`
	Path       string `json:"path"`
	BeforeText string `json:"beforeText"`
	Text       string `json:"text"`
}
type ShopEditResult struct {
	Revision      uint64           `json:"revision"`
	Files         []ShopEditedFile `json:"files"`
	AffectedItems int              `json:"affectedItems"`
	ModifiedCount int              `json:"modifiedCount"`
}

// ReadItem resolves the same item union used by shop lists and search pickers.
func (s *FileGUIService) ReadItem(id string, drafts []ShopDraft) (*ShopItem, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	if s.c.annotationEngine == nil {
		return nil, fmt.Errorf("物品索引未初始化")
	}
	ref, ok := s.c.resolveAnnotationReferenceLocked("物品", id)
	if !ok {
		return nil, fmt.Errorf("找不到物品 ID %s", id)
	}
	for _, draft := range drafts {
		if draft.FileIndex == ref.FileIndex {
			if !sameSearchPath(draft.Path, s.c.archive.Path(ref.FileIndex)) {
				return nil, fmt.Errorf("物品草稿已变化")
			}
			return s.readItemLocked(id, &draft.Text)
		}
	}
	return s.readItemLocked(id, nil)
}
func (s *FileGUIService) readItemLocked(id string, draftText *string) (*ShopItem, error) {
	if s.c.annotationEngine == nil {
		return nil, fmt.Errorf("物品索引未初始化")
	}
	ref, ok := s.c.resolveAnnotationReferenceLocked("物品", id)
	if !ok {
		return nil, fmt.Errorf("找不到物品 ID %s", id)
	}
	a := s.c.archive
	meta, err := a.ScriptMetadata(ref.FileIndex)
	if err != nil {
		return nil, err
	}
	text, err := a.Text(ref.FileIndex)
	if err != nil {
		return nil, err
	}
	if draftText != nil {
		text = *draftText
	}
	doc := &ShopDocument{}
	item := &ShopItem{ID: id, FileIndex: ref.FileIndex, Path: a.Path(ref.FileIndex), Name: markedName(meta), Icon: imageReferenceFromPVF(meta.Icon), Costs: shopCosts(text, id, doc)}
	if len(doc.Issues) > 0 {
		return nil, fmt.Errorf("物品成本格式异常，请先在文本模式修正：%s", doc.Issues[0].Message)
	}
	for i := range item.Costs {
		cost := &item.Costs[i]
		if cost.Kind != "material" {
			continue
		}
		if m, ok := s.c.resolveAnnotationReferenceLocked("物品", cost.ItemID); ok {
			cost.Name = m.Name
			if metadata, err := a.ScriptMetadata(m.FileIndex); err == nil {
				cost.Name = markedName(metadata)
				cost.Icon = imageReferenceFromPVF(metadata.Icon)
			}
		} else {
			cost.Name = "未知物品 #" + cost.ItemID
		}
	}
	return item, nil
}

// ApplyShopEdit stages every touched file before publishing one overlay change.
// The caller sends dirty drafts, but only files touched by this operation are
// consumed. Revision and path checks reject another archive or stale dialog.
func (s *FileGUIService) ApplyShopEdit(req ShopEditRequest) (*ShopEditResult, error) {
	s.c.mu.Lock()
	result, a, summary, err := s.applyShopEditLocked(req)
	s.c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if len(result.Files) > 0 {
		s.c.scheduleArchiveMutations(a, summary)
		indexes := make([]int32, 0, len(result.Files))
		for _, f := range result.Files {
			indexes = append(indexes, f.FileIndex)
		}
		emitEvent("archive:batch-applied", map[string]any{"source": "shop-gui", "structural": false, "fileIndexes": indexes, "revision": result.Revision, "modifiedCount": result.ModifiedCount})
		emitEvent("archive:advanced-search-stale", map[string]any{"shop": true})
		emitVersionState(s.c, "shop-edited")
	}
	return result, nil
}

func (s *FileGUIService) applyShopEditLocked(req ShopEditRequest) (*ShopEditResult, *pvf.Archive, pvf.MutationSummary, error) {
	fail := func(err error) (*ShopEditResult, *pvf.Archive, pvf.MutationSummary, error) {
		return nil, nil, pvf.MutationSummary{}, err
	}
	a := s.c.archive
	if a == nil {
		return fail(ErrNoArchive)
	}
	if req.Revision != s.c.batchRevision {
		return fail(fmt.Errorf("商店数据已变化，请关闭表单并重新加载后再操作"))
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		return fail(err)
	}
	if err := validateAnnotationIndex(a, req.FileIndex); err != nil {
		return fail(err)
	}
	if !sameSearchPath(req.Path, a.Path(req.FileIndex)) || !strings.HasSuffix(strings.ToLower(req.Path), ".shp") {
		return fail(fmt.Errorf("商店文件已变化"))
	}
	if len(req.Text) > maxEditableBytes {
		return fail(fmt.Errorf("商店文本过大"))
	}
	parsed := parseShop(req.Text)
	if len(parsed.Issues) > 0 && !(req.Action == "add-tab" && len(parsed.Tabs) == 0 && len(parsed.Issues) == 1) {
		return fail(fmt.Errorf("商店结构异常，请先在文本模式修正：%s", parsed.Issues[0].Message))
	}
	stage := a.CloneForBatch()
	drafts := make(map[int32]ShopDraft)
	for _, d := range req.Drafts {
		if d.FileIndex < 0 || d.FileIndex >= a.FileCount() || !sameSearchPath(d.Path, a.Path(d.FileIndex)) {
			return fail(fmt.Errorf("草稿文件已变化"))
		}
		if _, ok := drafts[d.FileIndex]; ok {
			return fail(fmt.Errorf("重复的文件草稿"))
		}
		drafts[d.FileIndex] = d
	}
	drafts[req.FileIndex] = ShopDraft{FileIndex: req.FileIndex, Path: req.Path, Text: req.Text}
	loaded := make(map[int32]bool)
	beforeTexts := make(map[int32]string)
	load := func(index int32) (*pvf.ScriptDocument, error) {
		if !loaded[index] {
			text, err := a.Text(index)
			if err != nil {
				return nil, err
			}
			if cached, ok := s.c.editorText[index]; ok {
				text = cached
			}
			if draft, ok := drafts[index]; ok {
				text = draft.Text
			}
			if len(text) > maxEditableBytes {
				return nil, fmt.Errorf("待修改文件超过编辑大小限制")
			}
			beforeTexts[index] = text
			rawText, err := a.Text(index)
			if err != nil {
				return nil, err
			}
			if text != rawText {
				if err := stage.SetText(index, text); err != nil {
					return nil, err
				}
			}
			loaded[index] = true
		}
		raw, err := stage.RawBytes(index)
		if err != nil {
			return nil, err
		}
		return stage.ParseScriptDocument(raw)
	}
	document, err := load(req.FileIndex)
	if err != nil {
		return fail(err)
	}
	sell := document.Sections([]string{"sell info"})
	if len(sell) != 1 || !sell[0].HasEndTag() {
		return fail(fmt.Errorf("商店必须包含一个完整的 [sell info] 区块"))
	}
	tabs := document.Sections([]string{"sell info", "tab"})
	var tab *pvf.ScriptSection
	if req.Action != "add-tab" {
		if req.TabIndex < 0 || req.TabIndex >= len(tabs) || req.TabIndex >= len(parsed.Tabs) {
			return fail(fmt.Errorf("分页已变化，请重新加载"))
		}
		tab = tabs[req.TabIndex]
	}
	itemIDs := []string{}
	shopChanged := false
	switch req.Action {
	case "add-tab", "rename-tab":
		name := strings.TrimSpace(req.Name)
		if name == "" || strings.ContainsAny(name, "`\r\n") || len([]rune(name)) > 100 {
			return fail(fmt.Errorf("分页名称需为 1–100 个字符，不能包含换行或反引号"))
		}
		value, err := pvf.NewScriptValue(pvf.ScriptTokenQuoted, name, pvf.ScriptPoolUTF16)
		if err != nil {
			return fail(err)
		}
		if req.Action == "rename-tab" {
			err = tab.SetValues([]pvf.ScriptValue{value})
		} else {
			tab, err = sell[0].AppendSection("tab", []pvf.ScriptValue{value}, true)
			if err == nil {
				if parsed.CategoryType == "" {
					_, err = tab.AppendSection("item list", nil, true)
				} else {
					if len(parsed.Categories) == 0 {
						if parsed.CategoryType == "basic job" {
							if index, ok := a.Find("character/character.lst"); ok {
								pairs, e := a.ScriptListPairs(index)
								if e != nil {
									return fail(e)
								}
								for _, pair := range pairs {
									parsed.Categories = append(parsed.Categories, ShopCategory{ID: pair.ID})
								}
							}
						} else {
							for _, id := range []string{"0", "1", "2", "3"} {
								parsed.Categories = append(parsed.Categories, ShopCategory{ID: id})
							}
						}
						if len(parsed.Categories) == 0 {
							return fail(fmt.Errorf("商店没有可用分类，请先配置职业列表"))
						}
					}
					for _, category := range parsed.Categories {
						group, e := tab.AppendSection("category entry", nil, true)
						if e != nil {
							return fail(e)
						}
						id, e := shopInteger(category.ID)
						if e != nil {
							return fail(e)
						}
						if _, e = group.AppendSection("id", []pvf.ScriptValue{id}, false); e != nil {
							return fail(e)
						}
						if _, e = group.AppendSection("item list", nil, true); e != nil {
							return fail(e)
						}
					}
				}
			}
		}
		if err != nil {
			return fail(err)
		}
		shopChanged = true
	case "delete-tab":
		if err := tab.Delete(); err != nil {
			return fail(err)
		}
		shopChanged = true
	case "edit-item", "delete-item":
		ordinal := -1
		count := 0
		for _, group := range parsed.Tabs[req.TabIndex].Groups {
			for _, entry := range group.Items {
				if entry.SourceStart == req.SourceStart {
					ordinal = count
				}
				count++
			}
		}
		if ordinal < 0 {
			return fail(fmt.Errorf("商品位置已变化，请重新打开表单"))
		}
		var target *pvf.ScriptSection
		offset := ordinal
		for _, list := range shopItemLists(tab) {
			if offset < len(list.Values()) {
				target = list
				break
			}
			offset -= len(list.Values())
		}
		if target == nil {
			return fail(fmt.Errorf("无法定位商品记录"))
		}
		values := target.Values()
		if req.Action == "delete-item" {
			values = append(values[:offset], values[offset+1:]...)
		} else {
			value, err := shopInteger(req.ItemID)
			if err != nil {
				return fail(err)
			}
			if _, err = s.readItemLocked(req.ItemID, nil); err != nil {
				return fail(err)
			}
			values[offset] = value
			itemIDs = append(itemIDs, req.ItemID)
		}
		if err := target.SetValues(values); err != nil {
			return fail(err)
		}
		shopChanged = true
	case "add-item":
		value, err := shopInteger(req.ItemID)
		if err != nil {
			return fail(err)
		}
		if _, err = s.readItemLocked(req.ItemID, nil); err != nil {
			return fail(err)
		}
		var target *pvf.ScriptSection
		if parsed.CategoryType == "" {
			for _, child := range tab.Children() {
				if child.Name() == "item list" {
					target = child
					break
				}
			}
			if target == nil {
				target, err = tab.AppendSection("item list", nil, true)
			}
		} else {
			for _, group := range tab.Children() {
				if group.Name() != "category entry" {
					continue
				}
				id := ""
				for _, child := range group.Children() {
					if child.Name() == "id" {
						if v, ok := child.Get(0); ok {
							id = fmt.Sprint(v)
						}
					}
				}
				if id != req.CategoryID {
					continue
				}
				for _, child := range group.Children() {
					if child.Name() == "item list" {
						target = child
						break
					}
				}
				if target == nil {
					target, err = group.AppendSection("item list", nil, true)
				}
				break
			}
		}
		if err != nil {
			return fail(err)
		}
		if target == nil {
			known := false
			for _, category := range parsed.Categories {
				if category.ID == req.CategoryID {
					known = true
				}
			}
			if !known {
				return fail(fmt.Errorf("当前商店没有该分类，无法添加商品"))
			}
			group, e := tab.AppendSection("category entry", nil, true)
			if e != nil {
				return fail(e)
			}
			id, e := shopInteger(req.CategoryID)
			if e != nil {
				return fail(e)
			}
			if _, e = group.AppendSection("id", []pvf.ScriptValue{id}, false); e != nil {
				return fail(e)
			}
			target, e = group.AppendSection("item list", nil, true)
			if e != nil {
				return fail(e)
			}
		}
		if err := target.Append(value); err != nil {
			return fail(err)
		}
		itemIDs = append(itemIDs, req.ItemID)
		shopChanged = true
	case "batch-costs":
		if !req.SetGold && !req.SetMaterials {
			return fail(fmt.Errorf("请选择至少一种需要批量设置的成本"))
		}
		for _, list := range shopItemLists(tab) {
			for _, v := range list.Values() {
				itemIDs = append(itemIDs, fmt.Sprint(v.Value))
			}
		}
		if len(itemIDs) == 0 {
			return fail(fmt.Errorf("当前分页没有商品"))
		}
	default:
		return fail(fmt.Errorf("未知商店操作"))
	}
	if shopChanged {
		raw, err := stage.EncodeScriptDocument(document)
		if err != nil {
			return fail(err)
		}
		if err := stage.SetRawBytes(req.FileIndex, raw); err != nil {
			return fail(err)
		}
	}
	var gold []pvf.ScriptValue
	if req.SetGold && req.Gold != "" {
		v, err := shopInteger(req.Gold)
		if err != nil {
			return fail(fmt.Errorf("金币价格无效：%w", err))
		}
		gold = []pvf.ScriptValue{v}
	}
	materials := []pvf.ScriptValue{}
	if req.SetMaterials {
		for _, m := range req.Materials {
			id, err := shopInteger(m.ItemID)
			if err != nil {
				return fail(err)
			}
			qty, err := shopInteger(m.Quantity)
			if err != nil {
				return fail(err)
			}
			if _, ok := s.c.resolveAnnotationReferenceLocked("物品", m.ItemID); !ok {
				return fail(fmt.Errorf("找不到兑换道具 %s", m.ItemID))
			}
			materials = append(materials, id, qty)
		}
	}
	seen := make(map[int32]bool)
	for _, id := range itemIDs {
		ref, ok := s.c.resolveAnnotationReferenceLocked("物品", id)
		if !ok {
			return fail(fmt.Errorf("找不到商品 %s，未应用任何修改", id))
		}
		if seen[ref.FileIndex] {
			continue
		}
		seen[ref.FileIndex] = true
		if !req.SetGold && !req.SetMaterials {
			continue
		}
		itemDocument, err := load(ref.FileIndex)
		if err != nil {
			return fail(err)
		}
		if req.SetGold {
			if err := replaceShopCostSection(itemDocument, "price", gold, false); err != nil {
				return fail(err)
			}
		}
		if req.SetMaterials {
			if err := replaceShopCostSection(itemDocument, "need material", materials, true); err != nil {
				return fail(err)
			}
		}
		raw, err := stage.EncodeScriptDocument(itemDocument)
		if err != nil {
			return fail(err)
		}
		if err := stage.SetRawBytes(ref.FileIndex, raw); err != nil {
			return fail(err)
		}
	}
	indexes := make([]int32, 0, len(loaded))
	for i := range loaded {
		indexes = append(indexes, i)
	}
	sort.Slice(indexes, func(i, j int) bool { return indexes[i] < indexes[j] })
	changes := []pvf.ScriptChange{}
	paths := []string{}
	result := &ShopEditResult{Files: []ShopEditedFile{}, AffectedItems: len(seen)}
	for _, i := range indexes {
		before, err := a.RawBytes(i)
		if err != nil {
			return fail(err)
		}
		after, err := stage.RawBytes(i)
		if err != nil {
			return fail(err)
		}
		if bytes.Equal(before, after) {
			continue
		}
		text, err := stage.Text(i)
		if err != nil {
			return fail(err)
		}
		changes = append(changes, pvf.ScriptChange{Kind: pvf.ChangeKindChanged, Path: a.Path(i), Raw: after, DataType: a.File(i).DataType})
		paths = append(paths, a.Path(i))
		result.Files = append(result.Files, ShopEditedFile{FileIndex: i, Path: a.Path(i), BeforeText: beforeTexts[i], Text: text})
	}
	var before, after pvfversion.ContentSnapshot
	if len(changes) > 0 && s.c.versionRepo != nil {
		before, err = pvfversion.ContentSnapshotFromArchive(a, paths)
		if err != nil {
			return fail(err)
		}
		after, err = pvfversion.ContentSnapshotFromArchive(stage, paths)
		if err != nil {
			return fail(err)
		}
	}
	checkpoint := a.MutationCheckpoint()
	if len(changes) > 0 {
		changedIndexes := make(map[int32]struct{}, len(result.Files))
		for _, file := range result.Files {
			changedIndexes[file.FileIndex] = struct{}{}
		}
		if s.c.diskIndex != nil {
			if err := s.c.diskIndex.refreshFileMetadata(stage, changedIndexes); err != nil {
				return fail(err)
			}
		}
		if err := a.ApplyScriptChanges(stage, changes); err != nil {
			if s.c.diskIndex != nil {
				_ = s.c.diskIndex.refreshFileMetadata(a, changedIndexes)
			}
			return fail(err)
		}
		if s.c.diskIndex == nil {
			refreshArchiveIndexMetadataLocked(s.c, changedIndexes)
		}
		if err := s.c.recordVersionMutationLocked("商店编辑", before, after); err != nil {
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
		for _, f := range result.Files {
			s.c.editorText[f.FileIndex] = f.Text
			delete(s.c.visualsByFile, f.FileIndex)
		}
	}
	summary := a.MutationsSince(checkpoint)
	a.ClearMutations()
	result.Revision = s.c.batchRevision
	result.ModifiedCount = a.ModifiedCount()
	return result, a, summary, nil
}

func shopInteger(text string) (pvf.ScriptValue, error) {
	n, err := strconv.ParseInt(text, 10, 32)
	if err != nil || n < 0 {
		return pvf.ScriptValue{}, fmt.Errorf("需要 0–2147483647 的整数：%s", text)
	}
	return pvf.NewScriptValue(pvf.ScriptTokenInteger, n, "")
}
func shopItemLists(tab *pvf.ScriptSection) []*pvf.ScriptSection {
	lists := []*pvf.ScriptSection{}
	for _, child := range tab.Children() {
		switch child.Name() {
		case "item list":
			lists = append(lists, child)
		case "category entry":
			for _, list := range child.Children() {
				if list.Name() == "item list" {
					lists = append(lists, list)
				}
			}
		}
	}
	return lists
}
func replaceShopCostSection(doc *pvf.ScriptDocument, name string, values []pvf.ScriptValue, paired bool) error {
	sections := doc.Sections([]string{name})
	if len(values) == 0 {
		for _, section := range sections {
			if err := section.Delete(); err != nil {
				return err
			}
		}
		return nil
	}
	if len(sections) == 0 {
		_, err := doc.AppendSection(name, values, paired)
		return err
	}
	if err := sections[0].SetValues(values); err != nil {
		return err
	}
	for _, section := range sections[1:] {
		if err := section.Delete(); err != nil {
			return err
		}
	}
	return nil
}

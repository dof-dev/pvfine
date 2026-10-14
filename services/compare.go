package services

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

// 对比状态常量
const (
	CompareOpAdd    = "add"
	CompareOpDelete = "delete"
	CompareOpModify = "modify"
	CompareOpSame   = "same"
)

// CompareFileItem 记录单个文件的对比状态
type CompareFileItem struct {
	Path        string           `json:"path"`
	Operation   string           `json:"operation"` // "add", "delete", "modify", "same"
	BaseType    int32            `json:"baseType"`
	TargetType  int32            `json:"targetType"`
	BaseSize    int32            `json:"baseSize"`
	TargetSize  int32            `json:"targetSize"`
	FileIndex   int32            `json:"fileIndex"` // 本地归档中的索引，不存在则为 -1
	Tags        []TreeTag        `json:"tags,omitempty"`
	Annotations []TreeAnnotation `json:"annotations,omitempty"`
	Icon        *ImageReference  `json:"icon,omitempty"`
}

// CompareSummary 对比结果汇总
type CompareSummary struct {
	BasePath    string             `json:"basePath"`
	TargetPath  string             `json:"targetPath"`
	BaseCount   int                `json:"baseCount"`
	TargetCount int                `json:"targetCount"`
	AddCount    int                `json:"addCount"`
	DeleteCount int                `json:"deleteCount"`
	ModifyCount int                `json:"modifyCount"`
	SameCount   int                `json:"sameCount"`
	Files       []*CompareFileItem `json:"files"` // 默认仅包含发生差异的文件 (add, delete, modify)
}

// CompareFileDiff 单个文件的 Diff 结果
type CompareFileDiff struct {
	Path          string           `json:"path"`
	Operation     string           `json:"operation"`
	BaseType      int32            `json:"baseType"`
	TargetType    int32            `json:"targetType"`
	BaseSize      int32            `json:"baseSize"`
	TargetSize    int32            `json:"targetSize"`
	BaseText      string           `json:"baseText"`
	TargetText    string           `json:"targetText"`
	TextAvailable bool             `json:"textAvailable"`
	Diff          []*BatchDiffLine `json:"diff,omitempty"`
	DiffTruncated bool             `json:"diffTruncated"`
}

// MergeResult 合并操作结果汇总
type MergeResult struct {
	SuccessCount int      `json:"successCount"`
	FailedCount  int      `json:"failedCount"`
	MergedPaths  []string `json:"mergedPaths"`
	Errors       []string `json:"errors,omitempty"`
}

// CompareService 提供两个 PVF 归档之间的差异对比服务
type CompareService struct {
	c                *core
	mu               sync.Mutex
	cachedBase       *pvf.Archive
	cachedTarget     *pvf.Archive
	cachedBasePath   string
	cachedTargetPath string
	cachedUseCurrent bool
	cachedFiles      map[string]*CompareFileItem
}

// NewCompareService 创建归档比对服务实例
func NewCompareService(c *core) *CompareService {
	return &CompareService{
		c:           c,
		cachedFiles: make(map[string]*CompareFileItem),
	}
}

// SelectPVFDialog 弹出系统原生文件选择对话框
func (s *CompareService) SelectPVFDialog(title string) (string, error) {
	if title == "" {
		title = "选择 PVF 归档文件"
	}
	app := application.Get()
	if app == nil || app.Dialog == nil {
		return "", errors.New("系统对话框不可用")
	}
	return app.Dialog.OpenFile().
		AddFilter("Script.pvf 归档 (*.pvf)", "*.pvf").
		AddFilter("全部文件 (*.*)", "*.*").
		SetTitle(title).
		PromptForSingleSelection()
}

func canonicalComparePath(p string) string {
	return pvf.NormalizePath(p)
}

func displayComparePath(p string) string {
	clean := strings.TrimSpace(p)
	clean = strings.ReplaceAll(clean, "\\", "/")
	return strings.Trim(clean, "/")
}

// resolveBaseArchive 解析基准归档（形态调整：强制必须使用当前打开的本地归档）
func (s *CompareService) resolveBaseArchive(basePath string, useCurrentBase bool) (*pvf.Archive, string, error) {
	s.c.mu.RLock()
	a := s.c.archive
	s.c.mu.RUnlock()
	if a == nil {
		return nil, "", errors.New("必须先在主界面打开一个 PVF 归档，才能进行差异对比")
	}
	path := a.SourcePath()
	if path == "" {
		path = "(当前工作区)"
	}
	return a, path, nil
}

// resolveTargetArchive 解析目标归档
func (s *CompareService) resolveTargetArchive(targetPath string) (*pvf.Archive, error) {
	if strings.TrimSpace(targetPath) == "" {
		return nil, errors.New("请指定对比的目标 PVF 文件路径")
	}
	a, err := pvf.Open(targetPath)
	if err != nil {
		return nil, fmt.Errorf("打开目标归档失败: %w", err)
	}
	return a, nil
}

// buildPathIndexes 构建路径索引
func buildPathIndexes(a *pvf.Archive) (map[string]int32, map[string]string) {
	idxMap := make(map[string]int32, a.FileCount())
	displayMap := make(map[string]string, a.FileCount())
	for i := int32(0); i < a.FileCount(); i++ {
		orig := a.Path(i)
		key := canonicalComparePath(orig)
		if key == "" {
			continue
		}
		idxMap[key] = i
		if _, exists := displayMap[key]; !exists {
			displayMap[key] = displayComparePath(orig)
		}
	}
	return idxMap, displayMap
}

type bothCandidate struct {
	key        string
	bIdx       int32
	tIdx       int32
	chunkIndex int32
	bType      int32
	tType      int32
	bSize      int32
	tSize      int32
	display    string
}

// categorizeCompareEntries 将所有文件划分到新增、删除和共同候选列表
func categorizeCompareEntries(base, target *pvf.Archive, baseIdxs, targetIdxs map[string]int32, baseDisplays, targetDisplays map[string]string) (adds, dels []*CompareFileItem, candidates []bothCandidate) {
	// 遍历 targetIdxs，查找新增项
	for k, tIdx := range targetIdxs {
		if _, hasBase := baseIdxs[k]; !hasBase {
			tf := target.File(tIdx)
			disp := targetDisplays[k]
			adds = append(adds, &CompareFileItem{
				Path:       disp,
				Operation:  CompareOpAdd,
				TargetType: tf.DataType,
				TargetSize: tf.DataSize,
				FileIndex:  -1,
			})
		}
	}

	// 遍历 baseIdxs，查找删除项和共同存在的待比对项
	for k, bIdx := range baseIdxs {
		disp := baseDisplays[k]
		bf := base.File(bIdx)
		tIdx, hasTarget := targetIdxs[k]
		if !hasTarget {
			dels = append(dels, &CompareFileItem{
				Path:      disp,
				Operation: CompareOpDelete,
				BaseType:  bf.DataType,
				BaseSize:  bf.DataSize,
				FileIndex: bIdx,
			})
			continue
		}

		tf := target.File(tIdx)
		if targetDisplays[k] != "" {
			disp = targetDisplays[k]
		}
		candidates = append(candidates, bothCandidate{
			key:        k,
			bIdx:       bIdx,
			tIdx:       tIdx,
			chunkIndex: bf.ChunkIndex,
			bType:      bf.DataType,
			tType:      tf.DataType,
			bSize:      bf.DataSize,
			tSize:      tf.DataSize,
			display:    disp,
		})
	}
	return adds, dels, candidates
}

// isCandidateEqual 检查双端候选文件内容是否相同
func isCandidateEqual(base, target *pvf.Archive, c bothCandidate) bool {
	// 1. 类型不同必定不同
	if c.bType != c.tType {
		return false
	}

	// 2. 二进制/资源文件：大小不同必定修改，无需读取内容；大小相同时才比较字节
	if c.bType != pvf.TypeScript && c.bType != pvf.TypeUnicode {
		if c.bSize != c.tSize {
			return false
		}
		rawB, errB := base.RawBytes(c.bIdx)
		rawT, errT := target.RawBytes(c.tIdx)
		if errB != nil || errT != nil {
			return false
		}
		return bytes.Equal(rawB, rawT)
	}

	// 3. 脚本文件：若大小相同且 raw bytes 一致，必定相同
	if c.bSize == c.tSize {
		rawB, errB := base.RawBytes(c.bIdx)
		rawT, errT := target.RawBytes(c.tIdx)
		if errB == nil && errT == nil && bytes.Equal(rawB, rawT) {
			return true
		}
	}

	// 4. raw token 不一致时，反编译文本进行规范比较
	txtB, errB := base.CanonicalText(c.bIdx)
	txtT, errT := target.CanonicalText(c.tIdx)
	if errB == nil && errT == nil {
		return txtB == txtT
	}
	return false
}

// compareBothCandidates 按 ChunkIndex 顺序执行比对，极大提升缓存命中率并防止内存抖动
func compareBothCandidates(base, target *pvf.Archive, candidates []bothCandidate) []*CompareFileItem {
	if len(candidates) == 0 {
		return nil
	}

	// 核心局部性优化：按 ChunkIndex 排序，使同一 Chunk 的文件连续被访问，一次解压反复复用
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].chunkIndex != candidates[j].chunkIndex {
			return candidates[i].chunkIndex < candidates[j].chunkIndex
		}
		return candidates[i].key < candidates[j].key
	})

	results := make([]*CompareFileItem, len(candidates))

	// 控制适度并发，避免多个 worker 冲刷置换同一个 Chunk 缓存
	workers := 4
	if len(candidates) < workers*2 {
		workers = 1
	}
	chunkSize := (len(candidates) + workers - 1) / workers
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if end > len(candidates) {
			end = len(candidates)
		}
		if start >= end {
			continue
		}
		wg.Add(1)
		go func(sub []bothCandidate, offset int) {
			defer wg.Done()
			for i, c := range sub {
				item := &CompareFileItem{
					Path:       c.display,
					BaseType:   c.bType,
					TargetType: c.tType,
					BaseSize:   c.bSize,
					TargetSize: c.tSize,
					FileIndex:  c.bIdx,
				}
				if isCandidateEqual(base, target, c) {
					item.Operation = CompareOpSame
				} else {
					item.Operation = CompareOpModify
				}
				results[offset+i] = item
			}
		}(candidates[start:end], start)
	}
	wg.Wait()
	return results
}

// Compare 执行当前打开归档与指定目标归档的对比
func (s *CompareService) Compare(basePath, targetPath string, useCurrentBase bool) (*CompareSummary, error) {
	// 清理之前的旧比对会话，释放归档内存
	s.mu.Lock()
	s.releaseSessionLocked()
	s.mu.Unlock()

	baseArchive, effectiveBasePath, err := s.resolveBaseArchive(basePath, true)
	if err != nil {
		return nil, err
	}

	targetArchive, err := s.resolveTargetArchive(targetPath)
	if err != nil {
		return nil, err
	}
	cachedSuccessfully := false
	defer func() {
		if !cachedSuccessfully && targetArchive != nil {
			targetArchive.Release()
		}
	}()

	baseIdxs, baseDisplays := buildPathIndexes(baseArchive)
	targetIdxs, targetDisplays := buildPathIndexes(targetArchive)

	adds, dels, candidates := categorizeCompareEntries(baseArchive, targetArchive, baseIdxs, targetIdxs, baseDisplays, targetDisplays)
	bothResults := compareBothCandidates(baseArchive, targetArchive, candidates)

	// 比对完成后，立即释放解压期间积累的大块 Chunk 缓存与解析字符串，避免常驻高内存
	targetArchive.ResetCaches()
	runtime.GC()
	debug.FreeOSMemory()

	// 汇总统计与文件映射
	totalFiles := len(adds) + len(dels) + len(bothResults)
	fileMap := make(map[string]*CompareFileItem, totalFiles)
	diffFiles := make([]*CompareFileItem, 0, len(adds)+len(dels)+len(bothResults)/10)

	summary := &CompareSummary{
		BasePath:    filepath.Clean(effectiveBasePath),
		TargetPath:  filepath.Clean(targetPath),
		BaseCount:   int(baseArchive.FileCount()),
		TargetCount: int(targetArchive.FileCount()),
		AddCount:    len(adds),
		DeleteCount: len(dels),
	}

	for _, item := range adds {
		fileMap[canonicalComparePath(item.Path)] = item
		diffFiles = append(diffFiles, item)
	}
	for _, item := range dels {
		fileMap[canonicalComparePath(item.Path)] = item
		diffFiles = append(diffFiles, item)
	}
	for _, item := range bothResults {
		fileMap[canonicalComparePath(item.Path)] = item
		if item.Operation == CompareOpModify {
			summary.ModifyCount++
			diffFiles = append(diffFiles, item)
		} else {
			summary.SameCount++
		}
	}

	// 按路径排序差异列表
	sort.Slice(diffFiles, func(i, j int) bool {
		return strings.ToLower(diffFiles[i].Path) < strings.ToLower(diffFiles[j].Path)
	})

	// 核心数据瘦身：默认只向前端传输有差异的文件，极大降低 IPC 传输负担与前端响应式 Proxy 内存
	summary.Files = diffFiles

	// 注入标签、注释和图标元数据
	s.c.mu.RLock()
	for _, item := range diffFiles {
		s.populateCompareItemMetadataLocked(item)
	}
	s.c.mu.RUnlock()

	s.mu.Lock()
	s.cachedBase = baseArchive
	s.cachedTarget = targetArchive
	s.cachedBasePath = effectiveBasePath
	s.cachedTargetPath = targetPath
	s.cachedUseCurrent = true
	s.cachedFiles = fileMap
	cachedSuccessfully = true
	s.mu.Unlock()

	return summary, nil
}

// populateCompareItemMetadataLocked 填充单个对比项的元数据（标注、标签、图标）
func (s *CompareService) populateCompareItemMetadataLocked(item *CompareFileItem) {
	if item == nil {
		return
	}
	item.Annotations = s.c.annotationsForPathLocked(item.Path, false)
	if item.FileIndex >= 0 {
		if s.c.diskIndex != nil {
			item.Tags, _ = s.c.diskIndex.tags(item.FileIndex)
			visuals := s.c.diskIndex.visuals(item.FileIndex)
			item.Icon = cloneImageReference(visuals.icon)
		} else {
			item.Tags = cloneTreeTags(s.c.treeTagsByFile[item.FileIndex])
			visuals := s.c.visualsByFile[item.FileIndex]
			item.Icon = cloneImageReference(visuals.icon)
		}
	}
}

// GetSameFiles 分页获取未变更相同文件列表（按需查询，避免一次性加载数万个对象）
func (s *CompareService) GetSameFiles(query string, offset, limit int) ([]*CompareFileItem, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cachedFiles == nil {
		return nil, 0, errors.New("对比会话已失效")
	}

	q := strings.ToLower(strings.TrimSpace(query))
	matched := make([]*CompareFileItem, 0)
	for _, item := range s.cachedFiles {
		if item.Operation != CompareOpSame {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(item.Path), q) {
			continue
		}
		matched = append(matched, item)
	}

	sort.Slice(matched, func(i, j int) bool {
		return strings.ToLower(matched[i].Path) < strings.ToLower(matched[j].Path)
	})

	total := len(matched)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		return nil, total, nil
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	result := matched[offset:end]
	s.c.mu.RLock()
	for _, item := range result {
		s.populateCompareItemMetadataLocked(item)
	}
	s.c.mu.RUnlock()
	return result, total, nil
}

// buildFullCompareDiff 生成完整的行级差异列表（不丢弃上下文行，保留全部行供代码级完整浏览）
func buildFullCompareDiff(before, after string) []*BatchDiffLine {
	oldLines := splitBatchLines(before)
	newLines := splitBatchLines(after)
	ops := batchDiffOps(oldLines, newLines)
	if len(ops) == 0 {
		return nil
	}

	result := make([]*BatchDiffLine, 0, len(ops))
	oldLine, newLine := 1, 1
	for _, op := range ops {
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
		result = append(result, line)
	}
	return result
}

// GetDiff 获取单个文件的详细差异（返回完整文件行）
func (s *CompareService) GetDiff(path string) (*CompareFileDiff, error) {
	key := canonicalComparePath(path)
	if key == "" {
		return nil, errors.New("文件路径不能为空")
	}

	s.mu.Lock()
	baseArchive := s.cachedBase
	targetArchive := s.cachedTarget
	item, ok := s.cachedFiles[key]
	s.mu.Unlock()

	if baseArchive == nil || targetArchive == nil || !ok {
		return nil, errors.New("对比会话已失效，请重新比对")
	}

	bIdx, hasBase := baseArchive.Find(key)
	tIdx, hasTarget := targetArchive.Find(key)

	result := &CompareFileDiff{
		Path:       item.Path,
		Operation:  item.Operation,
		BaseType:   item.BaseType,
		TargetType: item.TargetType,
		BaseSize:   item.BaseSize,
		TargetSize: item.TargetSize,
	}

	// 检查文本可用性
	isBaseText := hasBase && (item.BaseType == pvf.TypeScript || item.BaseType == pvf.TypeUnicode)
	isTargetText := hasTarget && (item.TargetType == pvf.TypeScript || item.TargetType == pvf.TypeUnicode)

	if (!hasBase || isBaseText) && (!hasTarget || isTargetText) {
		result.TextAvailable = true
		if hasBase {
			if txt, err := baseArchive.Text(bIdx); err == nil {
				result.BaseText = txt
			}
		}
		if hasTarget {
			if txt, err := targetArchive.Text(tIdx); err == nil {
				result.TargetText = txt
			}
		}
		// 生成完整的行级 Diff（不折叠不截断）
		diffLines := buildFullCompareDiff(result.BaseText, result.TargetText)
		result.Diff = diffLines
		result.DiffTruncated = false
	}

	return result, nil
}

func mergeFileEntry(a, targetArchive *pvf.Archive, rawPath string) (bool, error) {
	key := canonicalComparePath(rawPath)
	tIdx, hasTarget := targetArchive.Find(key)
	if !hasTarget {
		return false, fmt.Errorf("目标归档中不存在文件: %s", rawPath)
	}

	tf := targetArchive.File(tIdx)
	isScript := tf.DataType == pvf.TypeScript || tf.DataType == pvf.TypeUnicode
	bIdx, hasBase := a.Find(key)

	if isScript {
		txt, err := targetArchive.Text(tIdx)
		if err != nil {
			return false, fmt.Errorf("读取目标脚本文本失败: %w", err)
		}
		if hasBase {
			if bf := a.File(bIdx); bf.DataType != tf.DataType {
				if tf.DataType == pvf.TypeScript || tf.DataType == pvf.TypeUnicode {
					if err := a.SetDataType(bIdx, tf.DataType); err != nil {
						return false, err
					}
				}
			}
			if err := a.SetText(bIdx, txt); err != nil {
				return false, err
			}
			return false, nil
		}
		_, err = a.AddFileText(rawPath, txt, tf.DataType)
		return true, err
	}

	raw, err := targetArchive.RawBytes(tIdx)
	if err != nil {
		return false, fmt.Errorf("读取目标二进制数据失败: %w", err)
	}
	if hasBase {
		if bf := a.File(bIdx); bf.DataType != tf.DataType {
			if tf.DataType == pvf.TypeScript || tf.DataType == pvf.TypeUnicode {
				if err := a.SetDataType(bIdx, tf.DataType); err != nil {
					return false, err
				}
			}
		}
		if err := a.SetRawBytes(bIdx, raw); err != nil {
			return false, err
		}
		return false, nil
	}
	a.AddFile(rawPath, raw, tf.DataType)
	return true, nil
}

func deleteFileEntry(a *pvf.Archive, rawPath string) (bool, error) {
	key := canonicalComparePath(rawPath)
	bIdx, hasBase := a.Find(key)
	if !hasBase {
		return false, nil
	}
	deleted, err := a.RemoveFiles([]int32{bIdx})
	if err != nil {
		return false, err
	}
	return len(deleted) > 0, nil
}

// MergeFiles 将选中的差异文件从目标归档合并到本地当前归档中
func (s *CompareService) MergeFiles(paths []string) (*MergeResult, error) {
	if len(paths) == 0 {
		return &MergeResult{}, nil
	}

	s.mu.Lock()
	targetArchive := s.cachedTarget
	if targetArchive == nil {
		s.mu.Unlock()
		return nil, errors.New("对比会话已失效，请重新比对")
	}
	s.mu.Unlock()

	s.c.mu.Lock()
	a := s.c.archive
	if a == nil {
		s.c.mu.Unlock()
		return nil, ErrNoArchive
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}

	mutationCheckpoint := a.MutationCheckpoint()
	var before pvfversion.ContentSnapshot
	if s.c.versionRepo != nil {
		before, _ = pvfversion.ContentSnapshotFromArchive(a, paths)
	}

	res := &MergeResult{MergedPaths: make([]string, 0, len(paths))}
	structuralChanged := false

	for _, rawPath := range paths {
		key := canonicalComparePath(rawPath)
		_, hasTarget := targetArchive.Find(key)

		var structural bool
		var err error
		if hasTarget {
			structural, err = mergeFileEntry(a, targetArchive, rawPath)
		} else {
			structural, err = deleteFileEntry(a, rawPath)
		}

		if err != nil {
			res.FailedCount++
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", rawPath, err))
			continue
		}
		if structural {
			structuralChanged = true
		}
		res.SuccessCount++
		res.MergedPaths = append(res.MergedPaths, rawPath)
	}

	if structuralChanged {
		if err := s.c.rebuildArchiveIndexesLocked(a); err != nil {
			s.c.mu.Unlock()
			return nil, err
		}
	}

	if s.c.versionRepo != nil && len(res.MergedPaths) > 0 {
		after, _ := pvfversion.ContentSnapshotFromArchive(a, res.MergedPaths)
		_ = s.c.recordVersionMutationLocked("合并 PVF 差异", before, after)
	}

	s.c.invalidateScriptLocked()
	s.c.editorAnnotation = editorAnnotationCache{}
	s.c.annotationRelations = make(map[string]map[string]*relationTarget)
	s.c.invalidateAdvancedSearchLocked()

	mutationSummary := a.MutationsSince(mutationCheckpoint)
	a.ClearMutations()
	info := a.Info()
	versioned := s.c.versionRepo != nil
	s.c.mu.Unlock()

	s.c.scheduleArchiveMutations(a, mutationSummary)
	emitEvent("archive:changed", info)
	emitEvent("archive:advanced-search-stale", map[string]any{"batch": true})
	emitEvent("archive:batch-applied", map[string]any{
		"source":        "compare",
		"structural":    structuralChanged,
		"modifiedCount": info.ModifiedCount,
	})
	if versioned {
		emitVersionState(s.c, "files-merged")
	}

	// 同步更新对比会话中的文件状态
	s.mu.Lock()
	for _, p := range res.MergedPaths {
		k := canonicalComparePath(p)
		if it, ok := s.cachedFiles[k]; ok {
			it.Operation = CompareOpSame
		}
	}
	s.mu.Unlock()

	return res, nil
}

// MergeSingleFileText 将指定的文本内容应用合并到本地当前归档中
func (s *CompareService) MergeSingleFileText(path, text string) error {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return errors.New("文件路径不能为空")
	}

	s.c.mu.Lock()
	a := s.c.archive
	if a == nil {
		s.c.mu.Unlock()
		return ErrNoArchive
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return err
	}

	mutationCheckpoint := a.MutationCheckpoint()
	var before pvfversion.ContentSnapshot
	if s.c.versionRepo != nil {
		before, _ = pvfversion.ContentSnapshotFromArchive(a, []string{cleanPath})
	}

	key := canonicalComparePath(cleanPath)
	bIdx, exists := a.Find(key)
	structuralChanged := false

	if exists {
		if err := a.SetText(bIdx, text); err != nil {
			s.c.mu.Unlock()
			return err
		}
	} else {
		// 新增脚本文件
		dataType := int32(pvf.TypeScript)
		if strings.EqualFold(filepath.Ext(cleanPath), ".str") {
			dataType = pvf.TypeUnicode
		}
		newIdx, err := a.AddFileText(cleanPath, text, dataType)
		if err != nil {
			s.c.mu.Unlock()
			return err
		}
		bIdx = newIdx
		structuralChanged = true
	}

	if structuralChanged {
		if err := s.c.rebuildArchiveIndexesLocked(a); err != nil {
			s.c.mu.Unlock()
			return err
		}
	}

	if s.c.versionRepo != nil {
		after, _ := pvfversion.ContentSnapshotFromArchive(a, []string{cleanPath})
		_ = s.c.recordVersionMutationLocked("合并脚本内容", before, after)
	}

	s.c.invalidateScriptLocked()
	s.c.editorAnnotation = editorAnnotationCache{}
	s.c.annotationRelations = make(map[string]map[string]*relationTarget)
	s.c.invalidateAdvancedSearchLocked()

	mutationSummary := a.MutationsSince(mutationCheckpoint)
	a.ClearMutations()
	info := a.Info()
	versioned := s.c.versionRepo != nil
	s.c.mu.Unlock()

	s.c.scheduleArchiveMutations(a, mutationSummary)
	emitEvent("archive:changed", info)
	emitEvent("archive:advanced-search-stale", map[string]any{"batch": true})
	emitEvent("archive:batch-applied", map[string]any{
		"source":        "compare",
		"fileIndexes":   []int32{bIdx},
		"structural":    structuralChanged,
		"modifiedCount": info.ModifiedCount,
	})
	if versioned {
		emitVersionState(s.c, "file-updated")
	}

	// 更新对比状态
	s.mu.Lock()
	if it, ok := s.cachedFiles[key]; ok {
		it.Operation = CompareOpSame
	}
	s.mu.Unlock()

	return nil
}

func (s *CompareService) releaseSessionLocked() {
	if !s.cachedUseCurrent && s.cachedBase != nil {
		s.cachedBase.Release()
	}
	if s.cachedTarget != nil {
		s.cachedTarget.Release()
	}
	s.cachedBase = nil
	s.cachedTarget = nil
	s.cachedBasePath = ""
	s.cachedTargetPath = ""
	s.cachedUseCurrent = false
	s.cachedFiles = nil
}

// CloseCompare 关闭当前比对会话并释放资源
func (s *CompareService) CloseCompare() error {
	s.mu.Lock()
	s.releaseSessionLocked()
	s.mu.Unlock()
	runtime.GC()
	debug.FreeOSMemory()
	return nil
}

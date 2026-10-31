package services

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

const (
	dropRateSectionName     = "basis of rarity dicision"
	dropRateGroupSize       = 7
	dropRateLegacyGroupSize = 6
	dropRate100CNGroupSize  = 8
	dropRate110USGroupSize  = 9
	dropRateValueCount      = 5
	dropRateTotal           = 10000 // 百分比的百分之一: 100.00% == 10000
	dropRateRawPerUnit      = 100   // PVF 原始刻度: 0.01% == 100
)

var (
	ErrDropRateUnsupported = errors.New("基础掉率编辑器仅支持 90US/90CN/TW/110US/100CN/115CN PVF")
	ErrDropRateStale       = errors.New("掉率数据已过期,请重新加载")
)

type dropRateFileSpec struct {
	key        string
	path       string
	offset     int
	groupCount int
}

var dropRateFileSpecs = []dropRateFileSpec{
	{key: "hell", path: "etc/itemdropinfo_monster_hell.etc", offset: 1, groupCount: 2},
	{key: "flip", path: "etc/itemdropinfo_clearreward.etc", offset: 0, groupCount: 1},
	{key: "monster", path: "etc/itemdropinfo_monseter.etc", offset: 0, groupCount: 4},
	{key: "elite", path: "etc/itemdropinfo_monseter_extra.etc", offset: 0, groupCount: 4},
}

// DropRateDocument is the structured view consumed by the drop-rate editor.
// Rates are percentage hundredths: 1234 means 12.34%.
type DropRateDocument struct {
	PVFVersion string             `json:"pvfVersion"`
	Revision   uint64             `json:"revision"`
	Sections   []*DropRateSection `json:"sections"`
}

// DropRateSection identifies one of the four supported drop files.
type DropRateSection struct {
	Key    string           `json:"key"`
	Groups []*DropRateGroup `json:"groups"`
}

// DropRateGroup contains five rarity probabilities in percentage hundredths.
type DropRateGroup struct {
	Rates []int `json:"rates"`
}

// DropRateApplyRequest applies the complete editor snapshot atomically.
type DropRateApplyRequest struct {
	Revision uint64             `json:"revision"`
	Sections []*DropRateSection `json:"sections"`
}

// DropRateApplyResult reports the in-memory archive state after applying.
type DropRateApplyResult struct {
	Revision      uint64  `json:"revision"`
	FileIndexes   []int32 `json:"fileIndexes"`
	ModifiedCount int     `json:"modifiedCount"`
}

// DropService reads and applies the four structured drop-rate files without
// opening them as editor tabs. Changes remain in the archive overlay until the
// regular editor save operation writes the PVF source file.
type DropService struct{ c *core }

func NewDropService(c *core) *DropService { return &DropService{c: c} }

// Read returns the current drop-rate snapshot for a verified client family.
func (s *DropService) Read() (*DropRateDocument, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		return nil, err
	}
	version := s.c.archive.ClientVersion()
	if !supportedDropRateVersion(version) {
		return nil, ErrDropRateUnsupported
	}

	document, err := readDropRateDocument(s.c.archive)
	if err != nil {
		return nil, err
	}
	document.PVFVersion = version
	document.Revision = s.c.batchRevision
	return document, nil
}

// Apply validates and applies a complete editor snapshot atomically. The
// revision check prevents a stale modal from overwriting a newer archive edit.
func (s *DropService) Apply(request DropRateApplyRequest) (DropRateApplyResult, error) {
	s.c.mu.Lock()
	if s.c.archive == nil {
		s.c.mu.Unlock()
		return DropRateApplyResult{}, ErrNoArchive
	}
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return DropRateApplyResult{}, err
	}
	version := s.c.archive.ClientVersion()
	if !supportedDropRateVersion(version) {
		s.c.mu.Unlock()
		return DropRateApplyResult{}, ErrDropRateUnsupported
	}
	if request.Revision != s.c.batchRevision {
		s.c.mu.Unlock()
		return DropRateApplyResult{}, ErrDropRateStale
	}

	replacements, err := prepareDropRateReplacements(s.c.archive, request.Sections)
	if err != nil {
		s.c.mu.Unlock()
		return DropRateApplyResult{}, err
	}
	if len(replacements) == 0 {
		result := DropRateApplyResult{
			Revision:      s.c.batchRevision,
			FileIndexes:   []int32{},
			ModifiedCount: s.c.archive.ModifiedCount(),
		}
		s.c.mu.Unlock()
		return result, nil
	}

	paths := make([]string, 0, len(replacements))
	for _, replacement := range replacements {
		paths = append(paths, replacement.path)
	}
	var before pvfversion.ContentSnapshot
	if s.c.versionRepo != nil {
		before, err = pvfversion.ContentSnapshotFromArchive(s.c.archive, paths)
		if err != nil {
			s.c.mu.Unlock()
			return DropRateApplyResult{}, err
		}
	}

	a := s.c.archive
	mutationCheckpoint := a.MutationCheckpoint()
	fileIndexes := make([]int32, 0, len(replacements))
	for _, replacement := range replacements {
		if err := a.SetRawBytes(replacement.index, replacement.raw); err != nil {
			s.c.mu.Unlock()
			return DropRateApplyResult{}, err
		}
		fileIndexes = append(fileIndexes, replacement.index)
	}

	if s.c.versionRepo != nil {
		after, snapshotErr := pvfversion.ContentSnapshotFromArchive(a, paths)
		if snapshotErr != nil {
			s.c.mu.Unlock()
			return DropRateApplyResult{}, snapshotErr
		}
		if recordErr := s.c.recordVersionMutationLocked("基础掉率", before, after); recordErr != nil {
			s.c.mu.Unlock()
			return DropRateApplyResult{}, recordErr
		}
	}

	mutationSummary := a.MutationsSince(mutationCheckpoint)
	s.c.markAdvancedSearchStaleLocked(a)
	a.ClearMutations()
	s.c.batchRevision++
	s.c.batchPlan = nil
	s.c.invalidateScriptLocked()
	s.c.editorAnnotation = editorAnnotationCache{}
	s.c.annotationRelations = make(map[string]map[string]*relationTarget)
	s.c.markAdvancedSearchStaleLocked(s.c.archive)
	for _, index := range fileIndexes {
		delete(s.c.editorText, index)
		delete(s.c.visualsByFile, index)
	}
	info := a.Info()
	revision := s.c.batchRevision
	versioned := s.c.versionRepo != nil
	s.c.mu.Unlock()

	if err := s.c.scheduleArchiveMutations(a, mutationSummary); err != nil {
		return DropRateApplyResult{}, err
	}
	emitEvent("archive:advanced-search-stale", map[string]any{"dropRate": true})
	emitEvent("archive:batch-applied", map[string]any{
		"fileIndexes": fileIndexes, "modifiedCount": info.ModifiedCount,
		"revision": revision, "source": "drop-rate", "structural": false,
	})
	if versioned {
		emitVersionState(s.c, "drop-rate-applied")
	}

	return DropRateApplyResult{
		Revision:      revision,
		FileIndexes:   fileIndexes,
		ModifiedCount: info.ModifiedCount,
	}, nil
}

type dropRateReplacement struct {
	index int32
	path  string
	raw   []byte
}

func supportedDropRateVersion(version string) bool {
	return version == "90US" || version == "90CN" || version == "TW" || version == "110US" || version == "100CN" || version == "115CN"
}

func readDropRateDocument(a *pvf.Archive) (*DropRateDocument, error) {
	sections := make([]*DropRateSection, 0, len(dropRateFileSpecs))
	for _, spec := range dropRateFileSpecs {
		section, err := readDropRateSection(a, spec)
		if err != nil {
			return nil, err
		}
		sections = append(sections, section)
	}
	return &DropRateDocument{Sections: sections}, nil
}

type parsedDropRateFile struct {
	index     int32
	raw       []byte
	document  *pvf.ScriptDocument
	section   *pvf.ScriptSection
	values    []int64
	groupSize int
}

func loadDropRateFile(a *pvf.Archive, spec dropRateFileSpec) (*parsedDropRateFile, error) {
	index, ok := a.Find(spec.path)
	if !ok {
		return nil, fmt.Errorf("找不到掉率文件: %s", spec.path)
	}
	if a.File(index).DataType != pvf.TypeScript {
		return nil, fmt.Errorf("掉率文件不是脚本类型: %s", spec.path)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		return nil, fmt.Errorf("读取掉率文件 %s 失败: %w", spec.path, err)
	}
	document, err := a.ParseScriptDocument(raw)
	if err != nil {
		return nil, fmt.Errorf("解析掉率文件 %s 失败: %w", spec.path, err)
	}
	section, ok := document.Section([]string{dropRateSectionName}, 0)
	if !ok {
		return nil, fmt.Errorf("掉率文件缺少 section [%s]: %s", dropRateSectionName, spec.path)
	}
	values, err := scriptIntegerValues(section.Values(), spec.path)
	if err != nil {
		return nil, err
	}
	groupSize, err := dropRateGroupWidth(values, spec)
	if err != nil {
		return nil, err
	}
	return &parsedDropRateFile{
		index:     index,
		raw:       raw,
		document:  document,
		section:   section,
		values:    values,
		groupSize: groupSize,
	}, nil
}

func readDropRateSection(a *pvf.Archive, spec dropRateFileSpec) (*DropRateSection, error) {
	file, err := loadDropRateFile(a, spec)
	if err != nil {
		return nil, err
	}

	groups := make([]*DropRateGroup, 0, spec.groupCount)
	for groupIndex := 0; groupIndex < spec.groupCount; groupIndex++ {
		start := spec.offset + groupIndex*file.groupSize
		rates, err := decodeDropRateGroup(file.values[start : start+file.groupSize])
		if err != nil {
			return nil, fmt.Errorf("掉率文件 %s 第 %d 组无效: %w", spec.path, groupIndex+1, err)
		}
		groups = append(groups, &DropRateGroup{Rates: rates})
	}
	return &DropRateSection{Key: spec.key, Groups: groups}, nil
}

func prepareDropRateReplacements(a *pvf.Archive, sections []*DropRateSection) ([]dropRateReplacement, error) {
	byKey := make(map[string]*DropRateSection, len(sections))
	for _, section := range sections {
		if section == nil || section.Key == "" {
			return nil, errors.New("掉率 section 不能为空")
		}
		if _, exists := byKey[section.Key]; exists {
			return nil, fmt.Errorf("掉率 section 重复: %s", section.Key)
		}
		byKey[section.Key] = section
	}
	if len(byKey) != len(dropRateFileSpecs) {
		return nil, fmt.Errorf("掉率 section 数量无效,需要 %d 个", len(dropRateFileSpecs))
	}

	replacements := make([]dropRateReplacement, 0, len(dropRateFileSpecs))
	for _, spec := range dropRateFileSpecs {
		sectionInput, ok := byKey[spec.key]
		if !ok {
			return nil, fmt.Errorf("缺少掉率 section: %s", spec.key)
		}
		replacement, err := prepareDropRateFileReplacement(a, spec, sectionInput)
		if err != nil {
			return nil, err
		}
		if replacement != nil {
			replacements = append(replacements, *replacement)
		}
	}
	return replacements, nil
}

func prepareDropRateFileReplacement(a *pvf.Archive, spec dropRateFileSpec, sectionInput *DropRateSection) (*dropRateReplacement, error) {
	if len(sectionInput.Groups) != spec.groupCount {
		return nil, fmt.Errorf("掉率 section %s 有 %d 组,需要 %d 组", spec.key, len(sectionInput.Groups), spec.groupCount)
	}

	file, err := loadDropRateFile(a, spec)
	if err != nil {
		return nil, err
	}

	for groupIndex, group := range sectionInput.Groups {
		if group == nil {
			return nil, fmt.Errorf("掉率 section %s 第 %d 组为空", spec.key, groupIndex+1)
		}
		if err := validateDropRates(group.Rates); err != nil {
			return nil, fmt.Errorf("掉率 section %s 第 %d 组无效: %w", spec.key, groupIndex+1, err)
		}
		start := spec.offset + groupIndex*file.groupSize
		groupSlice := file.values[start : start+file.groupSize]
		offset, err := detectDropRateGroupOffset(groupSlice)
		if err != nil {
			return nil, fmt.Errorf("掉率文件 %s 第 %d 组无效: %w", spec.path, groupIndex+1, err)
		}
		currentRates, err := decodeDropRateGroupWithOffset(groupSlice, offset)
		if err != nil {
			return nil, fmt.Errorf("掉率文件 %s 第 %d 组无效: %w", spec.path, groupIndex+1, err)
		}
		if equalDropRates(currentRates, group.Rates) {
			continue
		}
		cumulative := int64(0)
		for rateIndex := 0; rateIndex < dropRateValueCount-1; rateIndex++ {
			cumulative += int64(group.Rates[rateIndex]) * dropRateRawPerUnit
			if err := file.section.Set(pvf.ScriptValue{
				Type:  pvf.ScriptTokenInteger,
				Value: cumulative,
			}, start+offset+rateIndex); err != nil {
				return nil, fmt.Errorf("修改掉率文件 %s 第 %d 组失败: %w", spec.path, groupIndex+1, err)
			}
		}
	}
	encoded, err := a.EncodeScriptDocument(file.document)
	if err != nil {
		return nil, fmt.Errorf("编码掉率文件 %s 失败: %w", spec.path, err)
	}
	if !bytes.Equal(file.raw, encoded) {
		return &dropRateReplacement{index: file.index, path: spec.path, raw: encoded}, nil
	}
	return nil, nil
}

func equalDropRates(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func scriptIntegerValues(values []pvf.ScriptValue, path string) ([]int64, error) {
	result := make([]int64, len(values))
	for index, value := range values {
		if value.Type != pvf.ScriptTokenInteger {
			return nil, fmt.Errorf("掉率文件 %s 第 %d 个值不是整数", path, index+1)
		}
		number, ok := value.Value.(int64)
		if !ok {
			return nil, fmt.Errorf("掉率文件 %s 第 %d 个整数值类型无效", path, index+1)
		}
		result[index] = number
	}
	return result, nil
}

func isDropTerminal(val int64) bool {
	return val >= 1_000_000 && val <= 1_000_002
}

func detectDropRateGroupOffset(group []int64) (int, error) {
	if len(group) < dropRateLegacyGroupSize {
		return 0, fmt.Errorf("分组至少需要 %d 个值", dropRateLegacyGroupSize)
	}
	if isDropTerminal(group[4]) {
		return 0, nil
	}
	if group[0] == 0 {
		if isDropTerminal(group[5]) {
			return 1, nil
		}
		return 0, fmt.Errorf("终止阈值 %d 不在兼容范围内", group[5])
	}
	return 0, fmt.Errorf("终止阈值 %d 不在兼容范围内", group[4])
}

// decodeDropRateGroup accepts the legacy six-value group. The sixth value is
// intentionally kept unchanged by the five-rarity editor; newer revisions
// append more thresholds, which are also retained without reinterpretation.
// Hell drop files prepend a leading 0 threshold (skipping common drops), so
// five-rarity thresholds start at index 1 and terminate at index 5.
func decodeDropRateGroup(group []int64) ([]int, error) {
	offset, err := detectDropRateGroupOffset(group)
	if err != nil {
		return nil, err
	}
	return decodeDropRateGroupWithOffset(group, offset)
}

func decodeDropRateGroupWithOffset(group []int64, offset int) ([]int, error) {
	previous := int64(0)
	diffs := make([]int64, dropRateValueCount)
	for index := range diffs {
		current := group[offset+index]
		if current < previous {
			return nil, fmt.Errorf("累计阈值递减")
		}
		diffs[index] = current - previous
		previous = current
	}

	rates := make([]int, len(diffs))
	remainders := make([]int64, len(diffs))
	base := 0
	for index, diff := range diffs {
		rates[index] = int(diff / dropRateRawPerUnit)
		remainders[index] = diff % dropRateRawPerUnit
		base += rates[index]
	}
	if base > dropRateTotal {
		return nil, fmt.Errorf("量化后的掉率总和超过 100.00%%")
	}
	remaining := dropRateTotal - base
	if remaining > len(rates) {
		return nil, fmt.Errorf("量化后的掉率无法归一到 100.00%%")
	}
	order := []int{0, 1, 2, 3, 4}
	sort.SliceStable(order, func(left, right int) bool {
		if remainders[order[left]] != remainders[order[right]] {
			return remainders[order[left]] > remainders[order[right]]
		}
		return order[left] < order[right]
	})
	for index := 0; index < remaining; index++ {
		rates[order[index]]++
	}
	return rates, nil
}

func validateDropRates(rates []int) error {
	if len(rates) != dropRateValueCount {
		return fmt.Errorf("需要 %d 个概率值", dropRateValueCount)
	}
	sum := 0
	for index, rate := range rates {
		if rate < 0 || rate > dropRateTotal {
			return fmt.Errorf("第 %d 个概率超出 0.00%%~100.00%%", index+1)
		}
		sum += rate
	}
	if sum != dropRateTotal {
		return fmt.Errorf("概率总和为 %.2f%%,必须为 100.00%%", float64(sum)/100)
	}
	return nil
}

// Known group widths: TW=6, 90US/90CN=7, 100CN=8, 110US=9.
// Validate each section's exact layout before interpreting or changing it.
func dropRateGroupWidth(values []int64, spec dropRateFileSpec) (int, error) {
	for _, width := range []int{dropRateLegacyGroupSize, dropRateGroupSize, dropRate100CNGroupSize, dropRate110USGroupSize} {
		if len(values) == spec.offset+spec.groupCount*width {
			return width, nil
		}
	}
	return 0, fmt.Errorf("掉率文件 %s 的 section 数据长度为 %d,需要 %d、%d、%d 或 %d", spec.path, len(values), spec.offset+spec.groupCount*dropRateLegacyGroupSize, spec.offset+spec.groupCount*dropRateGroupSize, spec.offset+spec.groupCount*dropRate100CNGroupSize, spec.offset+spec.groupCount*dropRate110USGroupSize)
}

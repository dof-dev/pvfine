package services

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"pvfine/internal/pvf"
)

const (
	AdvancedSearchModeBinary = "binary"
	AdvancedSearchModeString = "string"

	AdvancedIndexStateIdle     = "idle"
	AdvancedIndexStateBuilding = "building"
	AdvancedIndexStateReady    = "ready"
	AdvancedIndexStateError    = "error"
)

var ErrAdvancedSearchIndexing = errors.New("高级搜索字符串索引正在构建")

// AdvancedSearchIndexStatus describes the lazy string reverse-index state.
type AdvancedSearchIndexStatus struct {
	State string `json:"state"`
	Stage string `json:"stage"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
	Error string `json:"error"`
}

// AdvancedSearchHit is one file-level advanced-search result.
type AdvancedSearchHit struct {
	Name      string                  `json:"name,omitempty"`
	Path      string                  `json:"path"`
	Size      int32                   `json:"size"`
	DataType  int32                   `json:"dataType"`
	FileIndex int32                   `json:"fileIndex"`
	Details   []*AdvancedSearchDetail `json:"details,omitempty"`
}

// AdvancedSearchDetail explains why a file matched the query.
type AdvancedSearchDetail struct {
	Kind             string   `json:"kind"`
	Value            string   `json:"value,omitempty"`
	Pool             string   `json:"pool,omitempty"`
	PoolOffset       int32    `json:"poolOffset,omitempty"`
	Occurrences      int      `json:"occurrences"`
	TokenTypes       []int32  `json:"tokenTypes,omitempty"`
	FileFields       []string `json:"fileFields,omitempty"`
	ByteOffsets      []int    `json:"byteOffsets,omitempty"`
	TokenOffsets     []int    `json:"tokenOffsets,omitempty"`
	Hex              string   `json:"hex,omitempty"`
	OffsetsTruncated bool     `json:"offsetsTruncated,omitempty"`
}

// AdvancedSearchResult is a paged file-level advanced-search response.
type AdvancedSearchResult struct {
	Hits       []*AdvancedSearchHit `json:"hits"`
	NextCursor int                  `json:"nextCursor"`
	Scanned    int                  `json:"scanned"`
}

type binarySearchKey struct {
	scope   string
	pattern string
}

type advancedFileMatch struct {
	fileIndex int32
	details   []*AdvancedSearchDetail
}

// AdvancedIndexStatus returns the lazy string reverse-index status.
func (s *ArchiveService) AdvancedIndexStatus() AdvancedSearchIndexStatus {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	return s.c.advancedStatus
}

// AdvancedSearch searches raw token bytes or string-pool references.
// cursor is the result offset from the previous response; limit is 1..1000.
func (s *ArchiveService) AdvancedSearch(mode, query, scopePath string, regex bool, cursor, limit int) (*AdvancedSearchResult, error) {
	result := &AdvancedSearchResult{Hits: []*AdvancedSearchHit{}, NextCursor: -1}
	query = strings.TrimSpace(query)
	if query == "" {
		return result, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if cursor < 0 {
		cursor = 0
	}
	scope := normalizeAdvancedScope(scopePath)

	switch strings.ToLower(strings.TrimSpace(mode)) {
	case AdvancedSearchModeBinary:
		return s.searchAdvancedBinary(query, scope, cursor, limit)
	case AdvancedSearchModeString:
		return s.searchAdvancedString(query, scope, regex, cursor, limit)
	default:
		return nil, fmt.Errorf("未知高级搜索模式: %s", mode)
	}
}

func (s *ArchiveService) searchAdvancedString(query, scope string, regex bool, cursor, limit int) (*AdvancedSearchResult, error) {
	if regex {
		if _, err := regexp.Compile(query); err != nil {
			return nil, fmt.Errorf("正则表达式无效: %w", err)
		}
	}
	index, err := s.c.ensureAdvancedStringIndex()
	if err != nil {
		return nil, err
	}
	poolMatches, err := index.MatchDetails(query, regex)
	if err != nil {
		return nil, fmt.Errorf("匹配字符串池失败: %w", err)
	}
	byFile := make(map[int32][]*AdvancedSearchDetail)
	for _, match := range poolMatches {
		byFile[match.FileIndex] = append(byFile[match.FileIndex], &AdvancedSearchDetail{
			Kind:        "string",
			Value:       match.Value,
			Pool:        match.Pool,
			PoolOffset:  match.Offset,
			Occurrences: match.Occurrences,
			TokenTypes:  append([]int32(nil), match.TokenTypes...),
			FileFields:  append([]string(nil), match.FileFields...),
		})
	}
	matches := make([]advancedFileMatch, 0, len(byFile))
	for fileIndex, details := range byFile {
		matches = append(matches, advancedFileMatch{fileIndex: fileIndex, details: details})
	}
	return s.paginateAdvancedFiles(matches, scope, cursor, limit)
}

func (s *ArchiveService) searchAdvancedBinary(query, scope string, cursor, limit int) (*AdvancedSearchResult, error) {
	s.c.mu.RLock()
	if s.c.archive == nil {
		s.c.mu.RUnlock()
		return nil, ErrNoArchive
	}
	pattern, err := s.c.archive.EncodeScriptQuery(query)
	s.c.mu.RUnlock()
	if errors.Is(err, pvf.ErrQueryStringNotInPool) {
		return &AdvancedSearchResult{Hits: []*AdvancedSearchHit{}, NextCursor: -1}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("编译二进制查询失败: %w", err)
	}
	if len(pattern) == 0 {
		return &AdvancedSearchResult{Hits: []*AdvancedSearchHit{}, NextCursor: -1}, nil
	}

	matched, err := s.c.binarySearch(pattern, scope)
	if err != nil {
		return nil, err
	}
	return s.paginateAdvancedFiles(matched, "", cursor, limit)
}

func (c *core) ensureAdvancedStringIndex() (*pvf.StringPoolIndex, error) {
	c.mu.Lock()
	if c.archive == nil {
		c.mu.Unlock()
		return nil, ErrNoArchive
	}
	if c.advancedIndex != nil && c.advancedStatus.State == AdvancedIndexStateReady {
		index := c.advancedIndex
		c.mu.Unlock()
		return index, nil
	}
	if c.advancedStatus.State == AdvancedIndexStateBuilding {
		c.mu.Unlock()
		return nil, ErrAdvancedSearchIndexing
	}
	if c.advancedStatus.State == AdvancedIndexStateError && c.advancedStatus.Error != "" {
		c.mu.Unlock()
		return nil, errors.New(c.advancedStatus.Error)
	}

	a := c.archive
	ctx, cancel := context.WithCancel(context.Background())
	c.advancedCancel = cancel
	c.advancedStatus = AdvancedSearchIndexStatus{
		State: AdvancedIndexStateBuilding,
		Stage: "references",
		Total: int(a.FileCount()),
	}
	buildingStatus := c.advancedStatus
	c.mu.Unlock()
	emitEvent("archive:advanced-index-progress", buildingStatus)

	// Keep the archive read-locked for the build so SetText/Save cannot mutate
	// the overlay or string pools while the reverse references are collected.
	c.mu.RLock()
	index, err := a.BuildStringPoolIndex(ctx)
	current := c.archive == a
	c.mu.RUnlock()
	cancel()

	c.mu.Lock()
	if !current || c.archive != a {
		c.mu.Unlock()
		return nil, ErrNoArchive
	}
	c.advancedCancel = nil
	if err != nil {
		c.advancedStatus = AdvancedSearchIndexStatus{
			State: AdvancedIndexStateError,
			Stage: "error",
			Error: err.Error(),
		}
		status := c.advancedStatus
		c.mu.Unlock()
		emitEvent("archive:advanced-index-error", status)
		return nil, err
	}
	c.advancedIndex = index
	c.advancedStatus = AdvancedSearchIndexStatus{
		State: AdvancedIndexStateReady,
		Stage: "ready",
		Done:  int(a.FileCount()),
		Total: int(a.FileCount()),
	}
	status := c.advancedStatus
	c.mu.Unlock()
	emitEvent("archive:advanced-index-ready", status)
	return index, nil
}

func (c *core) binarySearch(pattern []byte, scope string) ([]advancedFileMatch, error) {
	key := binarySearchKey{scope: scope, pattern: string(pattern)}
	c.mu.RLock()
	if c.archive == nil {
		c.mu.RUnlock()
		return nil, ErrNoArchive
	}
	if cached, ok := c.binaryCache[key]; ok {
		result := append([]advancedFileMatch(nil), cached...)
		c.mu.RUnlock()
		return result, nil
	}
	a := c.archive
	matched := make([]advancedFileMatch, 0)
	err := a.ForEachRawFile(context.Background(), func(index int32, path string, _ pvf.File, raw []byte) bool {
		if !advancedPathInScope(path, scope) {
			return true
		}
		occurrences, byteOffsets, tokenOffsets := binaryMatchOffsets(raw, pattern)
		if occurrences > 0 {
			matched = append(matched, advancedFileMatch{
				fileIndex: index,
				details: []*AdvancedSearchDetail{{
					Kind:             "binary",
					Occurrences:      occurrences,
					ByteOffsets:      byteOffsets,
					TokenOffsets:     tokenOffsets,
					Hex:              formatHex(pattern),
					OffsetsTruncated: occurrences > len(byteOffsets),
				}},
			})
		}
		return true
	})
	if err != nil {
		c.mu.RUnlock()
		return nil, err
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return a.Path(matched[i].fileIndex) < a.Path(matched[j].fileIndex)
	})
	c.mu.RUnlock()

	c.mu.Lock()
	if c.archive == a {
		if c.binaryCache == nil {
			c.binaryCache = make(map[binarySearchKey][]advancedFileMatch)
		}
		c.binaryCache[key] = append([]advancedFileMatch(nil), matched...)
	}
	c.mu.Unlock()
	return matched, nil
}

func (s *ArchiveService) paginateAdvancedFiles(matches []advancedFileMatch, scope string, cursor, limit int) (*AdvancedSearchResult, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}

	filtered := make([]advancedFileMatch, 0, len(matches))
	for _, match := range matches {
		index := match.fileIndex
		if index < 0 || index >= s.c.archive.FileCount() {
			continue
		}
		if scope != "" && !advancedPathInScope(s.c.archive.Path(index), scope) {
			continue
		}
		filtered = append(filtered, match)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return s.c.archive.Path(filtered[i].fileIndex) < s.c.archive.Path(filtered[j].fileIndex)
	})

	result := &AdvancedSearchResult{Hits: []*AdvancedSearchHit{}, NextCursor: -1}
	if cursor > len(filtered) {
		cursor = len(filtered)
	}
	i := cursor
	for ; i < len(filtered) && len(result.Hits) < limit; i++ {
		match := filtered[i]
		index := match.fileIndex
		file := s.c.archive.File(index)
		result.Hits = append(result.Hits, &AdvancedSearchHit{
			Name:      s.c.indexedFileNameLocked(index),
			Path:      s.c.archive.Path(index),
			Size:      file.DataSize,
			DataType:  file.DataType,
			FileIndex: index,
			Details:   match.details,
		})
	}
	if i < len(filtered) {
		result.NextCursor = i
	}
	result.Scanned = i
	return result, nil
}

func (c *core) indexedFileNameLocked(index int32) string {
	seen := make(map[string]struct{})
	names := make([]string, 0, 1)
	for _, recordIndex := range c.searchByFile[index] {
		if recordIndex < 0 || recordIndex >= len(c.searchRecords) {
			continue
		}
		record := c.searchRecords[recordIndex].hit
		if record.Category == SearchCategoryFile || record.Name == "" {
			continue
		}
		if _, ok := seen[record.Name]; ok {
			continue
		}
		seen[record.Name] = struct{}{}
		names = append(names, record.Name)
	}
	return strings.Join(names, " / ")
}

const maxAdvancedOffsets = 32

func binaryMatchOffsets(raw, pattern []byte) (int, []int, []int) {
	if len(pattern) == 0 {
		return 0, nil, nil
	}
	occurrences := 0
	byteOffsets := make([]int, 0, maxAdvancedOffsets)
	tokenOffsets := make([]int, 0, maxAdvancedOffsets)
	for start := 0; start <= len(raw)-len(pattern); {
		relative := bytes.Index(raw[start:], pattern)
		if relative < 0 {
			break
		}
		offset := start + relative
		occurrences++
		if len(byteOffsets) < maxAdvancedOffsets {
			byteOffsets = append(byteOffsets, offset)
			if offset%5 == 0 {
				tokenOffsets = append(tokenOffsets, offset/5)
			}
		}
		start = offset + 1
	}
	return occurrences, byteOffsets, tokenOffsets
}

func formatHex(raw []byte) string {
	encoded := strings.ToUpper(hex.EncodeToString(raw))
	parts := make([]string, 0, len(encoded)/2)
	for i := 0; i+1 < len(encoded); i += 2 {
		parts = append(parts, encoded[i:i+2])
	}
	return strings.Join(parts, " ")
}

func (c *core) invalidateAdvancedSearchLocked() {
	if c.advancedCancel != nil {
		c.advancedCancel()
		c.advancedCancel = nil
	}
	c.advancedIndex = nil
	c.advancedStatus = AdvancedSearchIndexStatus{State: AdvancedIndexStateIdle}
	c.binaryCache = make(map[binarySearchKey][]advancedFileMatch)
}

func normalizeAdvancedScope(scope string) string {
	scope = strings.TrimSpace(strings.ReplaceAll(scope, "\\", "/"))
	for strings.HasPrefix(scope, "./") || strings.HasPrefix(scope, "/") {
		if strings.HasPrefix(scope, "./") {
			scope = scope[2:]
		} else {
			scope = scope[1:]
		}
	}
	return strings.TrimRight(strings.ToLower(scope), "/")
}

func advancedPathInScope(path, scope string) bool {
	if scope == "" {
		return true
	}
	path = strings.ToLower(strings.TrimRight(strings.ReplaceAll(path, "\\", "/"), "/"))
	return path == scope || strings.HasPrefix(path, scope+"/")
}

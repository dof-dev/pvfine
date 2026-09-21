package services

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
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
// Pass NextCursor back unchanged; string-mode cursors identify the query
// session and position, while binary mode retains its legacy offset. Limit is 1..1000.
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
	return s.searchAdvancedStringSQLite(query, scope, regex, cursor, limit)
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
	s.c.mu.RLock()
	disk := s.c.diskIndex != nil
	s.c.mu.RUnlock()
	if disk {
		return s.searchAdvancedBinaryDisk(pattern, scope, cursor, limit)
	}

	matched, err := s.c.binarySearch(pattern, scope)
	if err != nil {
		return nil, err
	}
	return s.paginateAdvancedFiles(matched, "", cursor, limit)
}

func (s *ArchiveService) searchAdvancedBinaryDisk(pattern []byte, scope string, cursor, limit int) (*AdvancedSearchResult, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if cursor < 0 {
		cursor = 0
	}
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if s.c.archive == nil || s.c.diskIndex == nil {
		return nil, ErrNoArchive
	}
	result := &AdvancedSearchResult{Hits: []*AdvancedSearchHit{}, NextCursor: -1}
	matched := 0
	var scanErr error
	err := s.c.diskIndex.eachFileByPath(func(index int32, path string, size, dataType int32) bool {
		if scope != "" && !advancedPathInScope(path, scope) {
			return true
		}
		raw, err := s.c.archive.RawBytes(index)
		if err != nil {
			scanErr = err
			return false
		}
		occurrences, byteOffsets, tokenOffsets := binaryMatchOffsets(raw, pattern)
		if occurrences == 0 {
			return true
		}
		if matched < cursor {
			matched++
			return true
		}
		hit := &AdvancedSearchHit{Name: "", Path: path, Size: size, DataType: dataType, FileIndex: index, Details: []*AdvancedSearchDetail{{Kind: "binary", Occurrences: occurrences, ByteOffsets: byteOffsets, TokenOffsets: tokenOffsets, Hex: formatHex(pattern)}}}
		if name, nameErr := s.c.diskIndex.indexedNames(index); nameErr == nil {
			hit.Name = name
		}
		result.Hits = append(result.Hits, hit)
		matched++
		return len(result.Hits) < limit
	})
	if err != nil {
		return nil, err
	}
	if scanErr != nil {
		return nil, scanErr
	}
	if len(result.Hits) >= limit {
		result.NextCursor = matched
	}
	result.Scanned = matched
	return result, nil
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
	if c.diskIndex != nil {
		name, _ := c.diskIndex.indexedNames(index)
		return name
	}
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
	if disk := c.detachAdvancedSearchLocked(); disk != nil {
		go disk.close()
	}
}

// detachAdvancedSearchLocked cancels and detaches the current disk session.
// The caller owns closing the returned session after releasing core.mu.
func (c *core) detachAdvancedSearchLocked() *advancedSQLite {
	disk := c.advancedDisk
	if disk != nil {
		disk.cancel()
		c.advancedDisk = nil
	}
	if c.advancedCancel != nil {
		c.advancedCancel()
		c.advancedCancel = nil
	}
	c.advancedStatus = AdvancedSearchIndexStatus{State: AdvancedIndexStateIdle}
	c.binaryCache = make(map[binarySearchKey][]advancedFileMatch)
	return disk
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

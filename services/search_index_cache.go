package services

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"pvfine/internal/pvf"
)

// searchIndexCacheVersion covers both cache formats: the small JSON snapshot's
// Version field and the SQLite index identity string. Bump it whenever the
// persisted metadata shape changes, so older files are rebuilt instead of
// silently missing a newer field.
const searchIndexCacheVersion = 3

// searchIndexCacheIdentity is the cheap source identity used by the search
// cache. The archive itself is already read before the index starts, but the
// product-level policy intentionally uses the source file's size and mtime
// rather than hashing the whole 110page container again.
type searchIndexCacheIdentity struct {
	ArchivePath    string
	SourceSize     int64
	SourceModified int64
	FileCount      int32
	GroupCount     int32
	BodySize       int32
	Paged110       bool
}

type persistedIndexedMetadata struct {
	Name       string          `json:"name"`
	ID         string          `json:"id"`
	Category   string          `json:"category"`
	ListPath   string          `json:"listPath"`
	Path       string          `json:"path"`
	FileIndex  int32           `json:"fileIndex"`
	Size       int32           `json:"size"`
	DataType   int32           `json:"dataType"`
	Rarity     int32           `json:"rarity"`
	Icon       *ImageReference `json:"icon,omitempty"`
	FieldImage *ImageReference `json:"fieldImage,omitempty"`
}

type persistedSearchIndex struct {
	Version          int                        `json:"version"`
	ArchivePath      string                     `json:"archivePath"`
	SourceSize       int64                      `json:"sourceSize"`
	SourceModified   int64                      `json:"sourceModified"`
	FileCount        int32                      `json:"fileCount"`
	GroupCount       int32                      `json:"groupCount"`
	BodySize         int32                      `json:"bodySize"`
	Paged110         bool                       `json:"paged110"`
	SpecsFingerprint string                     `json:"specsFingerprint"`
	Total            int                        `json:"total"`
	Skipped          int                        `json:"skipped"`
	Metadata         []persistedIndexedMetadata `json:"metadata"`
}

type searchIndexCacheSnapshot struct {
	Identity         searchIndexCacheIdentity
	SpecsFingerprint string
	Total            int
	Skipped          int
	Metadata         []indexedMetadata
}

func searchIndexSpecFingerprint(specs []searchableListSpec) string {
	type entry struct {
		ListPath string `json:"listPath"`
		Category string `json:"category"`
	}
	entries := make([]entry, 0, len(specs))
	for _, spec := range specs {
		entries = append(entries, entry{ListPath: spec.listPath, Category: spec.category})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Category != entries[j].Category {
			return entries[i].Category < entries[j].Category
		}
		return entries[i].ListPath < entries[j].ListPath
	})
	data, _ := json.Marshal(entries)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func searchIndexCacheIdentityForArchive(a *pvf.Archive) (searchIndexCacheIdentity, error) {
	if a == nil || a.SourcePath() == "" {
		return searchIndexCacheIdentity{}, errors.New("归档没有可缓存的干净源文件")
	}
	path, err := canonicalSearchIndexPath(a.SourcePath())
	if err != nil {
		return searchIndexCacheIdentity{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return searchIndexCacheIdentity{}, err
	}
	archiveInfo := a.Info()
	return searchIndexCacheIdentity{
		ArchivePath:    path,
		SourceSize:     info.Size(),
		SourceModified: info.ModTime().UnixNano(),
		FileCount:      archiveInfo.FileCount,
		GroupCount:     archiveInfo.GroupCount,
		BodySize:       archiveInfo.BodySize,
		Paged110:       archiveInfo.Paged110,
	}, nil
}

func canonicalSearchIndexPath(value string) (string, error) {
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = filepath.Clean(resolved)
	}
	return abs, nil
}

func searchIndexCachePath(sourcePath, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	canonical, err := canonicalSearchIndexPath(sourcePath)
	if err != nil {
		return "", err
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	return filepath.Join(cacheDir, "pvfine", "search-index", hex.EncodeToString(sum[:])+".json.gz"), nil
}

func loadSearchIndexCache(a *pvf.Archive, specs []searchableListSpec, override string) (searchIndexCacheSnapshot, error) {
	identity, err := searchIndexCacheIdentityForArchive(a)
	if err != nil {
		return searchIndexCacheSnapshot{}, err
	}
	path, err := searchIndexCachePath(identity.ArchivePath, override)
	if err != nil {
		return searchIndexCacheSnapshot{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return searchIndexCacheSnapshot{}, err
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return searchIndexCacheSnapshot{}, err
	}
	var persisted persistedSearchIndex
	decodeErr := json.NewDecoder(reader).Decode(&persisted)
	closeErr := reader.Close()
	if decodeErr != nil {
		return searchIndexCacheSnapshot{}, decodeErr
	}
	if closeErr != nil {
		return searchIndexCacheSnapshot{}, closeErr
	}
	if persisted.Version != searchIndexCacheVersion {
		return searchIndexCacheSnapshot{}, errors.New("搜索索引缓存版本不匹配")
	}
	if persisted.ArchivePath != identity.ArchivePath ||
		persisted.SourceSize != identity.SourceSize ||
		persisted.SourceModified != identity.SourceModified ||
		persisted.FileCount != identity.FileCount ||
		persisted.GroupCount != identity.GroupCount ||
		persisted.BodySize != identity.BodySize ||
		persisted.Paged110 != identity.Paged110 {
		return searchIndexCacheSnapshot{}, errors.New("搜索索引缓存已过期")
	}
	if persisted.SpecsFingerprint != searchIndexSpecFingerprint(specs) {
		return searchIndexCacheSnapshot{}, errors.New("搜索索引缓存关系配置不匹配")
	}
	if persisted.Total < 0 || persisted.Skipped < 0 || len(persisted.Metadata) > persisted.Total {
		return searchIndexCacheSnapshot{}, errors.New("搜索索引缓存记录数量无效")
	}

	metadata := make([]indexedMetadata, 0, len(persisted.Metadata))
	for _, saved := range persisted.Metadata {
		if saved.Path == "" || saved.Category == "" || saved.ListPath == "" {
			return searchIndexCacheSnapshot{}, errors.New("搜索索引缓存包含空记录")
		}
		fileIndex, ok := a.Find(saved.Path)
		if !ok {
			return searchIndexCacheSnapshot{}, fmt.Errorf("缓存目标文件不存在: %s", saved.Path)
		}
		file := a.File(fileIndex)
		metadata = append(metadata, indexedMetadata{
			name:       saved.Name,
			id:         saved.ID,
			category:   saved.Category,
			listPath:   saved.ListPath,
			path:       a.Path(fileIndex),
			fileIndex:  fileIndex,
			size:       file.DataSize,
			dataType:   file.DataType,
			rarity:     saved.Rarity,
			icon:       cloneImageReference(saved.Icon),
			fieldImage: cloneImageReference(saved.FieldImage),
		})
	}
	return searchIndexCacheSnapshot{
		Identity:         identity,
		SpecsFingerprint: persisted.SpecsFingerprint,
		Total:            persisted.Total,
		Skipped:          persisted.Skipped,
		Metadata:         metadata,
	}, nil
}

func saveSearchIndexCache(snapshot searchIndexCacheSnapshot, override string) error {
	path, err := searchIndexCachePath(snapshot.Identity.ArchivePath, override)
	if err != nil {
		return err
	}
	data := persistedSearchIndex{
		Version:          searchIndexCacheVersion,
		ArchivePath:      snapshot.Identity.ArchivePath,
		SourceSize:       snapshot.Identity.SourceSize,
		SourceModified:   snapshot.Identity.SourceModified,
		FileCount:        snapshot.Identity.FileCount,
		GroupCount:       snapshot.Identity.GroupCount,
		BodySize:         snapshot.Identity.BodySize,
		Paged110:         snapshot.Identity.Paged110,
		SpecsFingerprint: snapshot.SpecsFingerprint,
		Total:            snapshot.Total,
		Skipped:          snapshot.Skipped,
		Metadata:         make([]persistedIndexedMetadata, 0, len(snapshot.Metadata)),
	}
	for _, value := range snapshot.Metadata {
		data.Metadata = append(data.Metadata, persistedIndexedMetadata{
			Name:       value.name,
			ID:         value.id,
			Category:   value.category,
			ListPath:   value.listPath,
			Path:       value.path,
			FileIndex:  value.fileIndex,
			Size:       value.size,
			DataType:   value.dataType,
			Rarity:     value.rarity,
			Icon:       cloneImageReference(value.icon),
			FieldImage: cloneImageReference(value.fieldImage),
		})
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".search-index-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	writer := gzip.NewWriter(temp)
	if err := json.NewEncoder(writer).Encode(data); err != nil {
		_ = writer.Close()
		_ = temp.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

// persistSearchIndexCacheAsync snapshots the small canonical metadata slice
// under the core lock and performs compression/file IO off the request path.
// The generation and archive revision checks prevent an older background build
// from overwriting a newer cache after a mutation or archive switch.
func (c *core) persistSearchIndexCacheAsync(a *pvf.Archive, gen uint64, metadata []indexedMetadata, total, skipped int) {
	c.mu.RLock()
	if c.archive != a || c.indexGen != gen || c.indexStatus.State != IndexStateReady ||
		c.indexStatus.Refreshing || a.Modified() {
		c.mu.RUnlock()
		return
	}
	revision := c.batchRevision
	identity, err := searchIndexCacheIdentityForArchive(a)
	if err != nil {
		c.mu.RUnlock()
		return
	}
	snapshot := searchIndexCacheSnapshot{
		Identity:         identity,
		SpecsFingerprint: c.searchSpecFingerprint,
		Total:            total,
		Skipped:          skipped,
		Metadata:         cloneIndexedMetadata(metadata),
	}
	override := c.searchIndexCachePath
	c.mu.RUnlock()

	go func() {
		c.mu.RLock()
		valid := c.archive == a && c.indexGen == gen && c.batchRevision == revision &&
			c.indexStatus.State == IndexStateReady && !c.indexStatus.Refreshing && !a.Modified()
		c.mu.RUnlock()
		if !valid {
			return
		}
		_ = saveSearchIndexCache(snapshot, override)
	}()
}

func (c *core) persistCurrentSearchIndexCacheAsync() {
	c.mu.RLock()
	if c.archive == nil || c.diskIndex != nil || c.indexStatus.State != IndexStateReady || c.indexStatus.Refreshing {
		c.mu.RUnlock()
		return
	}
	a := c.archive
	gen := c.indexGen
	metadata := cloneIndexedMetadata(c.searchMetadata)
	total := c.indexStatus.Total
	skipped := c.indexStatus.Skipped
	c.mu.RUnlock()
	c.persistSearchIndexCacheAsync(a, gen, metadata, total, skipped)
}

// persistCurrentSQLiteIndexAsync rekeys a completed disk index after Save or
// SaveAs. The archive contents are already reflected in the working database;
// only the source identity changes when the packed PVF is written.
func (c *core) persistCurrentSQLiteIndexAsync() {
	c.mu.Lock()
	if c.archive == nil || c.diskIndex == nil || !c.diskIndex.ready || c.archive.Modified() {
		c.mu.Unlock()
		return
	}
	index := c.diskIndex
	a := c.archive
	target, identity, err := archiveIndexCachePath(a)
	if err != nil || target == index.path {
		c.mu.Unlock()
		return
	}
	index.dbMu.Lock()
	defer index.dbMu.Unlock()
	if _, err := index.db.Exec(`UPDATE meta SET value=? WHERE key='identity'`, identity); err != nil {
		c.mu.Unlock()
		return
	}
	_, _ = index.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".index-save-*.db")
	if err == nil {
		tmpPath := tmp.Name()
		_ = tmp.Close()
		if err = copySQLiteIndexFile(index.path, tmpPath); err == nil {
			_ = os.Remove(target)
			err = os.Rename(tmpPath, target)
		}
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}
	if err == nil {
		index.path = target
		index.identity = identity
	}
	c.mu.Unlock()
}

func copySQLiteIndexFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

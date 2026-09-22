// Package services hosts the wails3-exposed application services. All
// services share one core that owns the loaded pvf archive and its derived
// indexes (directory tree, sorted path list for search).
package services

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	annotationrules "pvfine/internal/annotations"
	"pvfine/internal/pvf"
	renderingrules "pvfine/internal/rendering"
	pvfversion "pvfine/internal/version"
)

// emitEvent 安全地发事件:脱离 wails 运行时(如单元测试)时为 no-op。
func emitEvent(name string, data ...any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data...)
	}
}

var (
	ErrNoArchive      = errors.New("尚未打开归档文件")
	ErrSearchIndexing = errors.New("搜索索引正在构建")
	ErrBatchPlanStale = errors.New("批处理预览已过期,请重新预览")
)

const (
	ChangeKindAdded    = "added"
	ChangeKindModified = "modified"
)

// TreeNode is one entry in the explorer tree: either a directory or a file.
type TreeNode struct {
	Name        string           `json:"name"`
	Path        string           `json:"path"`
	IsDir       bool             `json:"isDir"`
	Size        int32            `json:"size"`
	DataType    int32            `json:"dataType"`
	ChildCount  int32            `json:"childCount"`
	FileIndex   int32            `json:"fileIndex"` // -1 for directories
	ChangeKind  string           `json:"changeKind,omitempty"`
	Tags        []TreeTag        `json:"tags,omitempty"`
	Annotations []TreeAnnotation `json:"annotations,omitempty"`
	Icon        *ImageReference  `json:"icon,omitempty"`
	FieldImage  *ImageReference  `json:"fieldImage,omitempty"`
}

// pathEntry feeds the search scanner.
type pathEntry struct {
	path       string
	lower      string
	idx        int32
	size       int32
	typ        int32
	changeKind string
}

// core owns the loaded archive plus derived indexes. Guarded by mu; all
// services take it per call.
type core struct {
	mu                  sync.RWMutex
	archive             *pvf.Archive
	diskIndex           *sqliteArchiveIndex
	annotationEngine    *annotationrules.Engine
	annotationErr       error
	renderingEngine     *renderingrules.Engine
	renderingErr        error
	annotationRelations map[string]map[string]*relationTarget
	editorText          map[int32]string
	editorAnnotation    editorAnnotationCache
	pathAnnotations     map[string][]TreeAnnotation
	dirChildren         map[string][]*TreeNode // dirPath -> ordered children ("" = root)
	directories         []string
	sortedPaths         []pathEntry
	searchRecords       []searchRecord
	searchByFile        map[int32][]int
	// searchMetadata is the canonical semantic snapshot used to rebuild the
	// derived search records after a path/list delta. It intentionally keeps
	// only list-backed records; ordinary file records are derived from
	// sortedPaths and are therefore not duplicated here.
	searchMetadata        []indexedMetadata
	searchSpecFingerprint string
	treeTagsByFile        map[int32][]TreeTag
	visualsByFile         map[int32]fileVisuals
	indexStatus           IndexStatus
	indexStartedAt        time.Time
	indexCancel           context.CancelFunc
	// indexRefreshPending coalesces mutations that arrive while a semantic
	// build is already running. The active build is allowed to publish its
	// candidate, then one follow-up build consumes the accumulated dirty state.
	// Keeping this separate from indexGen avoids cancelling a useful build and
	// avoids losing a mutation in the cancel/publish race.
	indexRefreshPending      bool
	indexRefreshPendingForce bool
	indexDirty               map[int32]struct{}
	indexGen                 uint64
	searchIndexDeltaPending  bool
	searchIndexListPending   map[int32]struct{}
	// searchIndexCachePath is only set by tests. Production cache files are
	// resolved from os.UserCacheDir by search_index_cache.go.
	searchIndexCachePath string
	batchRevision        uint64
	batchPlan            *batchPlan
	scriptPlan           *scriptPlan
	scriptCancel         context.CancelFunc
	versionRepo          *pvfversion.Repository
	versionHead          pvfversion.Commit
	versionHeadSnapshot  pvfversion.Snapshot
	versionWorking       pvfversion.Snapshot
	versionChanges       []pvfversion.FileChange
	versionChangeMap     map[string]pvfversion.FileChange
	versionUndo          []versionUndoRecord
	versionSavedSnapshot pvfversion.Snapshot
	versionArtifactDirty map[string]struct{}
	versionSavedTree     string
	versionSavedPVF      string
	versionSavedCommit   string
	versionViewCommit    string
	versionBaseArchive   *pvf.Archive
	versionLoadID        uint64
	versionLoading       bool
	versionLoadError     string
	advancedDisk         *advancedSQLite
	advancedStatus       AdvancedSearchIndexStatus
	advancedCancel       context.CancelFunc
	binaryCache          map[binarySearchKey][]advancedFileMatch
	unpackCancel         atomic.Bool
	unpackRunning        atomic.Bool
	// autosave mirrors unsaved edits into a backup cache. It is attached once
	// during startup (nil in tests) so save/close handlers can drop a backup
	// that no longer protects anything.
	autosave *AutosaveService
}

// attachAutosave wires the backup service into the shared core. It is called
// before the application starts serving requests.
func (c *core) attachAutosave(service *AutosaveService) {
	c.autosave = service
}

type editorAnnotationCache struct {
	valid       bool
	fileIndex   int32
	text        string
	annotations []EditorAnnotation
}

func newCore() *core { return makeCore() }

// NewCore creates the shared service state (one per application).
func NewCore() *core { return makeCore() }

func makeCore() *core {
	annotationEngine, annotationErr := annotationrules.LoadDefault()
	renderingEngine, renderingErr := renderingrules.LoadDefault()
	return &core{
		annotationEngine: annotationEngine,
		annotationErr:    annotationErr,
		renderingEngine:  renderingEngine,
		renderingErr:     renderingErr,
		visualsByFile:    make(map[int32]fileVisuals),
	}
}

func archiveChangeKind(a *pvf.Archive, index int32) string {
	if a == nil || index < 0 || index >= a.FileCount() || !a.IsModified(index) {
		return ""
	}
	if a.File(index).ChunkIndex < 0 {
		return ChangeKindAdded
	}
	return ChangeKindModified
}

// setArchive loads an archive and builds derived indexes. Large archives use a
// disk-backed projection so the in-memory tree is never materialized.
func (c *core) setArchive(a *pvf.Archive) error {
	if c.annotationErr != nil {
		return c.annotationErr
	}
	if c.renderingErr != nil {
		return c.renderingErr
	}
	// Archives constructed in tests/import pipelines may have mutations from
	// their preparation phase. They are the clean baseline once installed.
	a.ClearMutations()
	if a.FileCount() >= largeArchiveIndexThreshold {
		index, _, err := openSQLiteArchiveIndex(a)
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.detachVersionLocked()
		if c.diskIndex != nil {
			c.diskIndex.close()
		}
		c.diskIndex = index
		c.installDiskArchiveIndexesLocked(a)
		c.mu.Unlock()
		return nil
	}
	children, paths, err := buildIndex(a)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.detachVersionLocked()
	if c.diskIndex != nil {
		c.diskIndex.close()
		c.diskIndex = nil
	}
	c.installArchiveIndexesLocked(a, children, paths)
	c.mu.Unlock()
	return nil
}

func (c *core) installDiskArchiveIndexesLocked(a *pvf.Archive) {
	if c.indexCancel != nil {
		c.indexCancel()
		c.indexCancel = nil
	}
	c.indexRefreshPending = false
	c.indexRefreshPendingForce = false
	c.invalidateAdvancedSearchLocked()
	c.indexGen++
	c.batchRevision++
	c.batchPlan = nil
	c.invalidateScriptLocked()
	c.bindRenderingEngineLocked(a)
	c.archive = a
	c.annotationEngine = c.annotationEngine.ForVersion(a.ClientVersion())
	c.annotationRelations = make(map[string]map[string]*relationTarget)
	c.editorText = make(map[int32]string)
	c.editorAnnotation = editorAnnotationCache{}
	c.pathAnnotations = nil
	c.dirChildren = nil
	c.directories = nil
	c.sortedPaths = nil
	c.searchRecords = nil
	c.searchByFile = nil
	c.searchMetadata = nil
	c.searchSpecFingerprint = ""
	c.searchIndexDeltaPending = false
	c.searchIndexListPending = nil
	c.treeTagsByFile = nil
	c.visualsByFile = nil
	c.indexStartedAt = time.Time{}
	c.indexDirty = make(map[int32]struct{})
	c.indexStatus = IndexStatus{State: IndexStateIdle}
	c.advancedStatus = AdvancedSearchIndexStatus{State: AdvancedIndexStateIdle}
	c.binaryCache = make(map[binarySearchKey][]advancedFileMatch)
	c.unpackCancel.Store(false)
	c.unpackRunning.Store(false)
}

func (c *core) recordOpenDuration(duration time.Duration) {
	c.mu.Lock()
	if c.archive != nil {
		c.indexStatus.OpenDurationMs = durationMilliseconds(duration)
	}
	c.mu.Unlock()
}

// replaceArchiveLocked installs a newly materialized archive while retaining
// the current version repository session. The caller must hold c.mu.
func (c *core) replaceArchiveLocked(a *pvf.Archive) error {
	oldArchive := c.archive
	revision := c.batchRevision
	c.batchRevision++
	c.mu.Unlock()
	prepared, err := prepareArchiveDerivedIndexes(a)
	c.mu.Lock()
	if err != nil {
		closePreparedArchiveIndexes(prepared)
		return err
	}
	if c.archive != oldArchive || c.batchRevision != revision+1 {
		closePreparedArchiveIndexes(prepared)
		// The replacement was materialized from the previous working snapshot;
		// never install it over a concurrent mutation.
		return ErrBatchPlanStale
	}
	return c.installPreparedArchiveIndexesLocked(a, prepared, true)
}

// replaceArchivePayloadLocked installs an archive whose path and data-type
// structure is unchanged. Reusing the existing tree/path indexes avoids a
// second full directory walk when checkout or discard only changes payloads.
// The caller must hold c.mu.
func (c *core) replaceArchivePayloadLocked(a *pvf.Archive, changedIndexes map[int32]struct{}) error {
	if a == nil || c.archive == nil {
		return ErrNoArchive
	}
	if c.indexCancel != nil {
		c.indexCancel()
		c.indexCancel = nil
	}
	c.indexRefreshPending = false
	c.indexRefreshPendingForce = false
	c.invalidateAdvancedSearchLocked()
	preserveSearch := c.indexStatus.State == IndexStateReady && c.searchRecords != nil
	c.indexGen++
	c.batchRevision++
	c.batchPlan = nil
	c.invalidateScriptLocked()
	c.bindRenderingEngineLocked(a)
	c.archive = a
	previousAnnotations := c.annotationEngine
	c.annotationEngine = c.annotationEngine.ForVersion(a.ClientVersion())
	if c.annotationEngine != previousAnnotations {
		c.pathAnnotations = buildPathAnnotations(c.annotationEngine, c.dirChildren)
	}
	if c.diskIndex != nil {
		if err := c.diskIndex.refreshFileMetadata(a, changedIndexes); err != nil {
			return err
		}
	} else {
		refreshArchiveIndexMetadataLocked(c, changedIndexes)
	}
	if preserveSearch {
		// Keep the old semantic snapshot until the mutation classifier proves
		// that a registered record actually changed.
		c.searchIndexDeltaPending = false
		c.searchIndexListPending = nil
	} else {
		c.searchRecords = nil
		c.searchByFile = make(map[int32][]int)
		c.searchMetadata = nil
		c.searchSpecFingerprint = ""
		c.searchIndexDeltaPending = false
		c.searchIndexListPending = nil
		c.treeTagsByFile = make(map[int32][]TreeTag)
		c.visualsByFile = make(map[int32]fileVisuals)
		c.indexStatus = IndexStatus{State: IndexStateIdle}
	}
	c.indexStartedAt = time.Time{}
	c.indexDirty = make(map[int32]struct{})
	c.editorText = make(map[int32]string)
	c.editorAnnotation = editorAnnotationCache{}
	c.annotationRelations = make(map[string]map[string]*relationTarget)
	c.advancedStatus = AdvancedSearchIndexStatus{State: AdvancedIndexStateIdle}
	c.binaryCache = make(map[binarySearchKey][]advancedFileMatch)
	c.unpackCancel.Store(false)
	c.unpackRunning.Store(false)
	return nil
}

// rebuildArchiveIndexesLocked refreshes every derived view after the archive's
// file table changes. The caller must hold c.mu and must pass c.archive.
func (c *core) rebuildArchiveIndexesLocked(a *pvf.Archive) error {
	for {
		if c.archive != a {
			return ErrNoArchive
		}
		// A read lock protects the live archive from writers while still
		// allowing resource-tree readers to run alongside the expensive build.
		revision := c.batchRevision
		c.batchRevision++
		c.mu.Unlock()
		c.mu.RLock()
		prepared, err := prepareArchiveDerivedIndexes(a)
		c.mu.RUnlock()
		c.mu.Lock()
		if err != nil {
			closePreparedArchiveIndexes(prepared)
			return err
		}
		if c.archive != a || c.batchRevision != revision+1 {
			closePreparedArchiveIndexes(prepared)
			continue
		}
		return c.installPreparedArchiveIndexesLocked(a, prepared, true)
	}
}

type preparedArchiveIndexes struct {
	children map[string][]*TreeNode
	paths    []pathEntry
	disk     *sqliteArchiveIndex
}

func prepareArchiveDerivedIndexes(a *pvf.Archive) (*preparedArchiveIndexes, error) {
	if a == nil {
		return nil, ErrNoArchive
	}
	if a.FileCount() >= largeArchiveIndexThreshold {
		index, _, err := openSQLiteArchiveIndex(a)
		if err != nil {
			return nil, err
		}
		return &preparedArchiveIndexes{disk: index}, nil
	}
	children, paths, err := buildIndex(a)
	if err != nil {
		return nil, err
	}
	return &preparedArchiveIndexes{children: children, paths: paths}, nil
}

func closePreparedArchiveIndexes(prepared *preparedArchiveIndexes) {
	if prepared != nil && prepared.disk != nil {
		prepared.disk.close()
	}
}

func (c *core) installPreparedArchiveIndexesLocked(a *pvf.Archive, prepared *preparedArchiveIndexes, preserveSearch bool) error {
	if prepared == nil {
		return ErrNoArchive
	}
	if prepared.disk != nil {
		if c.diskIndex != nil && c.diskIndex != prepared.disk {
			c.diskIndex.close()
		}
		c.diskIndex = prepared.disk
		c.installDiskArchiveIndexesLocked(a)
		return nil
	}
	if c.diskIndex != nil {
		c.diskIndex.close()
		c.diskIndex = nil
	}
	c.installArchiveIndexesLockedWithSearch(a, prepared.children, prepared.paths, preserveSearch)
	return nil
}

// installArchiveIndexesLocked installs a complete set of derived indexes.
// The caller must hold c.mu.
func (c *core) installArchiveIndexesLocked(a *pvf.Archive, children map[string][]*TreeNode, paths []pathEntry) {
	c.installArchiveIndexesLockedWithSearch(a, children, paths, false)
}

func (c *core) installArchiveIndexesPreservingSearchLocked(a *pvf.Archive, children map[string][]*TreeNode, paths []pathEntry) {
	c.installArchiveIndexesLockedWithSearch(a, children, paths, true)
}

func (c *core) installArchiveIndexesLockedWithSearch(a *pvf.Archive, children map[string][]*TreeNode, paths []pathEntry, preserveSearch bool) {
	if c.diskIndex != nil || a.FileCount() >= largeArchiveIndexThreshold {
		if c.diskIndex != nil {
			c.diskIndex.close()
			c.diskIndex = nil
		}
		index, _, err := openSQLiteArchiveIndex(a)
		if err != nil {
			c.indexStatus = IndexStatus{State: IndexStateError, Stage: "sqlite", Error: err.Error()}
			return
		}
		c.diskIndex = index
		c.installDiskArchiveIndexesLocked(a)
		return
	}
	preserveSearch = preserveSearch && c.indexStatus.State == IndexStateReady && c.searchRecords != nil
	if c.indexCancel != nil {
		c.indexCancel()
		c.indexCancel = nil
	}
	c.indexRefreshPending = false
	c.indexRefreshPendingForce = false
	c.invalidateAdvancedSearchLocked()
	c.indexGen++
	c.batchRevision++
	c.batchPlan = nil
	c.invalidateScriptLocked()
	directories := make([]string, 0, len(children))
	for path := range children {
		if path != "" {
			directories = append(directories, path)
		}
	}
	sort.Strings(directories)
	c.bindRenderingEngineLocked(a)
	c.archive = a
	c.annotationEngine = c.annotationEngine.ForVersion(a.ClientVersion())
	c.annotationRelations = make(map[string]map[string]*relationTarget)
	c.editorText = make(map[int32]string)
	c.editorAnnotation = editorAnnotationCache{}
	c.pathAnnotations = buildPathAnnotations(c.annotationEngine, children)
	c.dirChildren = children
	c.directories = directories
	c.sortedPaths = paths
	if preserveSearch {
		// The old semantic snapshot remains readable until the caller starts a
		// path-delta refresh. This keeps structural edits asynchronous.
		c.searchIndexDeltaPending = true
		c.searchIndexListPending = nil
	} else {
		c.searchRecords = nil
		c.searchByFile = make(map[int32][]int)
		c.searchMetadata = nil
		c.searchSpecFingerprint = ""
		c.searchIndexDeltaPending = false
		c.searchIndexListPending = nil
		c.treeTagsByFile = make(map[int32][]TreeTag)
		c.visualsByFile = make(map[int32]fileVisuals)
		c.indexStatus = IndexStatus{State: IndexStateIdle}
	}
	c.indexStartedAt = time.Time{}
	c.indexDirty = make(map[int32]struct{})
	c.advancedStatus = AdvancedSearchIndexStatus{State: AdvancedIndexStateIdle}
	c.binaryCache = make(map[binarySearchKey][]advancedFileMatch)
	c.unpackCancel.Store(false)
	c.unpackRunning.Store(false)
}

// bindRenderingEngineLocked applies the current user-facing renderer to an
// archive that is about to become active. The caller must hold c.mu.
func (c *core) bindRenderingEngineLocked(a *pvf.Archive) {
	if a == nil {
		return
	}
	a.SetScriptRenderer(c.renderingEngine)
}

func (c *core) closeArchive() {
	c.mu.Lock()
	c.detachVersionLocked()
	if c.indexCancel != nil {
		c.indexCancel()
		c.indexCancel = nil
	}
	c.invalidateAdvancedSearchLocked()
	c.indexGen++
	c.batchRevision++
	c.batchPlan = nil
	c.invalidateScriptLocked()
	c.archive = nil
	if c.diskIndex != nil {
		c.diskIndex.close()
		c.diskIndex = nil
	}
	c.annotationRelations = nil
	c.editorText = nil
	c.editorAnnotation = editorAnnotationCache{}
	c.pathAnnotations = nil
	c.dirChildren = nil
	c.directories = nil
	c.sortedPaths = nil
	c.searchRecords = nil
	c.searchByFile = nil
	c.searchMetadata = nil
	c.searchSpecFingerprint = ""
	c.searchIndexDeltaPending = false
	c.searchIndexListPending = nil
	c.treeTagsByFile = nil
	c.visualsByFile = nil
	c.indexStatus = IndexStatus{State: IndexStateIdle}
	c.indexStartedAt = time.Time{}
	c.indexDirty = nil
	c.indexRefreshPending = false
	c.indexRefreshPendingForce = false
	c.advancedStatus = AdvancedSearchIndexStatus{State: AdvancedIndexStateIdle}
	c.binaryCache = nil
	c.unpackCancel.Store(false)
	c.unpackRunning.Store(false)
	c.mu.Unlock()
}

// withArchive runs fn with the loaded archive under read lock.
func (c *core) withArchive(fn func(a *pvf.Archive) error) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.archive == nil {
		return ErrNoArchive
	}
	return fn(c.archive)
}

// withArchiveWrite runs fn with the loaded archive under the write lock.
// Archive edits and saves must exclude background index reads because the
// archive overlay and rebuilt tables are mutable.
func (c *core) withArchiveWrite(fn func(a *pvf.Archive) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.archive == nil {
		return ErrNoArchive
	}
	return fn(c.archive)
}

func (c *core) archiveInfo() (pvf.ArchiveInfoView, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.archive == nil {
		return pvf.ArchiveInfoView{}, false
	}
	return c.archive.Info(), true
}

// buildIndex creates the lazy-tree directory index and the search path list.
// Duplicate paths are preserved (each maps to its own file index).
func buildIndex(a *pvf.Archive) (map[string][]*TreeNode, []pathEntry, error) {
	n := a.FileCount()
	dirChildren := make(map[string][]*TreeNode)
	dirSet := make(map[string]bool)
	paths := make([]pathEntry, 0, n)

	// registerDir adds p and all missing ancestors as directory nodes.
	// Invariant: registering p also registers every ancestor, so hitting an
	// already-registered dir means the whole ancestor chain is present.
	registerDir := func(p string) {
		for p != "" {
			if dirSet[p] {
				return
			}
			dirSet[p] = true
			parent, name := splitParent(p)
			dirChildren[parent] = append(dirChildren[parent], &TreeNode{
				Name: name, Path: p, IsDir: true, FileIndex: -1,
			})
			p = parent
		}
	}

	for i := int32(0); i < n; i++ {
		p := a.Path(i)
		if p == "" {
			continue
		}
		f := a.File(i)
		changeKind := archiveChangeKind(a, i)
		paths = append(paths, pathEntry{
			path:       p,
			lower:      strings.ToLower(p),
			idx:        i,
			size:       f.DataSize,
			typ:        f.DataType,
			changeKind: changeKind,
		})

		parent, name := splitParent(p)
		dirChildren[parent] = append(dirChildren[parent], &TreeNode{
			Name: name, Path: p, IsDir: false,
			Size: f.DataSize, DataType: f.DataType, FileIndex: i,
			ChangeKind: changeKind,
		})
		registerDir(parent)
	}

	for _, list := range dirChildren {
		sort.Slice(list, func(x, y int) bool {
			if list[x].IsDir != list[y].IsDir {
				return list[x].IsDir
			}
			return list[x].Name < list[y].Name
		})
		for _, t := range list {
			if t.IsDir {
				t.ChildCount = int32(len(dirChildren[t.Path]))
			}
		}
	}

	sort.Slice(paths, func(x, y int) bool { return paths[x].path < paths[y].path })
	return dirChildren, paths, nil
}

func splitParent(p string) (parent, name string) {
	if slash := strings.LastIndexByte(p, '/'); slash >= 0 {
		return p[:slash], p[slash+1:]
	}
	return "", p
}

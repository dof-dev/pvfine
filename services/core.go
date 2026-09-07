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

	"github.com/wailsapp/wails/v3/pkg/application"

	"pvfine/internal/pvf"
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
)

// TreeNode is one entry in the explorer tree: either a directory or a file.
type TreeNode struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	IsDir      bool   `json:"isDir"`
	Size       int32  `json:"size"`
	DataType   int32  `json:"dataType"`
	ChildCount int32  `json:"childCount"`
	FileIndex  int32  `json:"fileIndex"` // -1 for directories
}

// pathEntry feeds the search scanner.
type pathEntry struct {
	path  string
	lower string
	idx   int32
	size  int32
	typ   int32
}

// core owns the loaded archive plus derived indexes. Guarded by mu; all
// services take it per call.
type core struct {
	mu            sync.RWMutex
	archive       *pvf.Archive
	dirChildren   map[string][]*TreeNode // dirPath -> ordered children ("" = root)
	sortedPaths   []pathEntry
	searchRecords []searchRecord
	searchByFile  map[int32][]int
	indexStatus   IndexStatus
	indexCancel   context.CancelFunc
	indexDirty    map[int32]struct{}
	indexGen      uint64
	unpackCancel  atomic.Bool
	unpackRunning atomic.Bool
}

func newCore() *core { return &core{} }

// NewCore creates the shared service state (one per application).
func NewCore() *core { return &core{} }

// setArchive loads an archive and builds derived indexes. Index building
// walks every path once (~1M entries, well under a second in Go).
func (c *core) setArchive(a *pvf.Archive) error {
	children, paths, err := buildIndex(a)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.indexCancel != nil {
		c.indexCancel()
		c.indexCancel = nil
	}
	c.indexGen++
	c.archive = a
	c.dirChildren = children
	c.sortedPaths = paths
	c.searchRecords = nil
	c.searchByFile = make(map[int32][]int)
	c.indexStatus = IndexStatus{State: IndexStateIdle}
	c.indexDirty = make(map[int32]struct{})
	c.unpackCancel.Store(false)
	c.unpackRunning.Store(false)
	c.mu.Unlock()
	return nil
}

func (c *core) closeArchive() {
	c.mu.Lock()
	if c.indexCancel != nil {
		c.indexCancel()
		c.indexCancel = nil
	}
	c.indexGen++
	c.archive = nil
	c.dirChildren = nil
	c.sortedPaths = nil
	c.searchRecords = nil
	c.searchByFile = nil
	c.indexStatus = IndexStatus{State: IndexStateIdle}
	c.indexDirty = nil
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
		paths = append(paths, pathEntry{path: p, lower: strings.ToLower(p), idx: i, size: f.DataSize, typ: f.DataType})

		parent, name := splitParent(p)
		dirChildren[parent] = append(dirChildren[parent], &TreeNode{
			Name: name, Path: p, IsDir: false,
			Size: f.DataSize, DataType: f.DataType, FileIndex: i,
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

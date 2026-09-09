package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"pvfine/internal/pvf"
	pvfversion "pvfine/internal/version"
)

var (
	ErrVersionNotEnabled      = errors.New("当前归档尚未初始化版本库")
	ErrVersionLoading         = errors.New("版本控制正在后台加载,请稍候")
	ErrVersionArtifactChanged = errors.New("版本库关联的 PVF 产物已被外部修改,请先处理后再继续")
)

const (
	VersionOperationAdd    = string(pvfversion.OperationAdd)
	VersionOperationModify = string(pvfversion.OperationModify)
	VersionOperationDelete = string(pvfversion.OperationDelete)
)

// VersionService exposes local logical-file version control to the Wails
// frontend.
type VersionService struct{ c *core }

func NewVersionService(c *core) *VersionService { return &VersionService{c: c} }

// VersionStatus is the UI-facing version session state.
type VersionStatus struct {
	Enabled           bool   `json:"enabled"`
	Loading           bool   `json:"loading"`
	RepositoryPath    string `json:"repositoryPath"`
	Branch            string `json:"branch"`
	HeadID            string `json:"headId"`
	HeadMessage       string `json:"headMessage"`
	ChangedFiles      int    `json:"changedFiles"`
	PendingChangeSets int    `json:"pendingChangeSets"`
	NeedsSave         bool   `json:"needsSave"`
	ViewCommitID      string `json:"viewCommitId"`
	Error             string `json:"error"`
}

// VersionChange is a path-level worktree or commit change.
type VersionChange struct {
	Path       string `json:"path"`
	Operation  string `json:"operation"`
	BeforeHash string `json:"beforeHash,omitempty"`
	AfterHash  string `json:"afterHash,omitempty"`
	BeforeType int32  `json:"beforeType,omitempty"`
	AfterType  int32  `json:"afterType,omitempty"`
}

type VersionChangePage struct {
	Changes    []*VersionChange `json:"changes"`
	NextCursor int              `json:"nextCursor"`
	Total      int              `json:"total"`
}

type VersionCommit struct {
	ID          string `json:"id"`
	ParentID    string `json:"parentId"`
	Message     string `json:"message"`
	CreatedAt   int64  `json:"createdAt"`
	ChangeCount int    `json:"changeCount"`
}

type VersionHistoryPage struct {
	Commits    []*VersionCommit `json:"commits"`
	NextCursor int              `json:"nextCursor"`
}

// VersionFileDiff contains one commit change and its logical text values
// when the underlying PVF type is editable.
type VersionFileDiff struct {
	Path          string `json:"path"`
	Operation     string `json:"operation"`
	BeforeHash    string `json:"beforeHash,omitempty"`
	AfterHash     string `json:"afterHash,omitempty"`
	BeforeType    int32  `json:"beforeType,omitempty"`
	AfterType     int32  `json:"afterType,omitempty"`
	BeforeText    string `json:"beforeText,omitempty"`
	AfterText     string `json:"afterText,omitempty"`
	TextAvailable bool   `json:"textAvailable"`
}

type versionUndoRecord struct {
	Label  string
	Before pvfversion.ContentSnapshot
	After  pvfversion.ContentSnapshot
}

// detachVersionLocked closes the repository session. The caller must hold
// c.mu; it is called when an archive is replaced or closed.
func (c *core) detachVersionLocked() {
	c.versionLoadID++
	c.versionLoading = false
	c.versionLoadError = ""
	if c.versionRepo != nil {
		_ = c.versionRepo.Close()
	}
	c.versionRepo = nil
	c.versionHead = pvfversion.Commit{}
	c.versionHeadSnapshot = nil
	c.versionWorking = nil
	c.versionChanges = nil
	c.versionChangeMap = nil
	c.versionUndo = nil
	c.versionSavedSnapshot = nil
	c.versionArtifactDirty = nil
	c.versionSavedTree = ""
	c.versionSavedPVF = ""
	c.versionSavedCommit = ""
	c.versionViewCommit = ""
	c.versionBaseArchive = nil
}

type preparedVersionSession struct {
	archive       *pvf.Archive
	repo          *pvfversion.Repository
	head          pvfversion.Commit
	headSnapshot  pvfversion.Snapshot
	working       pvfversion.Snapshot
	savedSnapshot pvfversion.Snapshot
	meta          pvfversion.Meta
	baseArchive   *pvf.Archive
	changes       []pvfversion.FileChange
	changeMap     map[string]pvfversion.FileChange
	artifactDirty map[string]struct{}
}

// attachVersionSessionLocked installs a fully prepared version session. All
// repository IO, PVF parsing and snapshot diffing must happen before entering
// this method so opening an archive does not hold c.mu during the slow work.
func (c *core) attachVersionSessionLocked(session *preparedVersionSession) error {
	if session == nil || session.repo == nil {
		return ErrVersionNotEnabled
	}
	if session.working == nil {
		return errors.New("版本工作区快照为空")
	}
	c.versionRepo = session.repo
	c.versionHead = session.head
	// These snapshots and derived maps are detached values prepared by the
	// background loader; attaching only swaps pointers under the short lock.
	c.versionHeadSnapshot = session.headSnapshot
	c.versionWorking = session.working
	c.versionSavedSnapshot = session.savedSnapshot
	c.versionChanges = session.changes
	c.versionChangeMap = session.changeMap
	c.versionArtifactDirty = session.artifactDirty
	c.versionUndo = nil
	c.versionSavedTree = session.meta.Artifact.TreeHash
	c.versionSavedPVF = session.meta.Artifact.PVFHash
	c.versionSavedCommit = session.meta.Artifact.CommitID
	c.versionViewCommit = ""
	c.versionBaseArchive = session.baseArchive
	return nil
}

func (c *core) currentVersionChangesLocked() []pvfversion.FileChange {
	if c.versionRepo == nil {
		return nil
	}
	return append([]pvfversion.FileChange(nil), c.versionChanges...)
}

func (c *core) versionStatusLocked() VersionStatus {
	status := VersionStatus{Branch: "main", Loading: c.versionLoading, Error: c.versionLoadError}
	if c.versionRepo == nil {
		return status
	}
	status.Enabled = true
	status.RepositoryPath = c.versionRepo.Root()
	status.HeadID = c.versionHead.ID
	status.HeadMessage = c.versionHead.Message
	status.ChangedFiles = len(c.versionChanges)
	status.PendingChangeSets = len(c.versionUndo)
	status.ViewCommitID = c.versionViewCommit
	status.NeedsSave = len(c.versionArtifactDirty) > 0
	status.Error = ""
	return status
}

func (c *core) ensureVersionReadyLocked() error {
	if c.versionLoading {
		return ErrVersionLoading
	}
	return nil
}

func sameVersionEntry(left, right pvfversion.Entry) bool {
	return left.DataType == right.DataType && left.Hash == right.Hash
}

func versionChangeForPath(before, after pvfversion.Snapshot, key string) (pvfversion.FileChange, bool) {
	oldEntry, hadOld := before[key]
	newEntry, hasNew := after[key]
	switch {
	case !hadOld && hasNew:
		return pvfversion.FileChange{
			Path: key, DisplayPath: newEntry.Path,
			Operation: pvfversion.OperationAdd,
			AfterHash: newEntry.Hash, AfterType: newEntry.DataType,
		}, true
	case hadOld && !hasNew:
		return pvfversion.FileChange{
			Path: key, DisplayPath: oldEntry.Path,
			Operation:  pvfversion.OperationDelete,
			BeforeHash: oldEntry.Hash, BeforeType: oldEntry.DataType,
		}, true
	case hadOld && hasNew && !sameVersionEntry(oldEntry, newEntry):
		return pvfversion.FileChange{
			Path: key, DisplayPath: newEntry.Path,
			Operation:  pvfversion.OperationModify,
			BeforeHash: oldEntry.Hash, AfterHash: newEntry.Hash,
			BeforeType: oldEntry.DataType, AfterType: newEntry.DataType,
		}, true
	default:
		return pvfversion.FileChange{}, false
	}
}

func (c *core) rebuildVersionChangesLocked() {
	if c.versionRepo == nil {
		c.versionChanges = nil
		c.versionChangeMap = nil
		return
	}
	c.versionChanges, c.versionChangeMap = versionChangesForSnapshots(c.versionHeadSnapshot, c.versionWorking)
}

func versionChangesForSnapshots(head, working pvfversion.Snapshot) ([]pvfversion.FileChange, map[string]pvfversion.FileChange) {
	changes := pvfversion.Diff(head, working)
	changeMap := make(map[string]pvfversion.FileChange, len(changes))
	for _, change := range changes {
		changeMap[change.Path] = change
	}
	return changes, changeMap
}

func (c *core) refreshVersionChangeLocked(key string) {
	if c.versionChangeMap == nil {
		c.versionChangeMap = make(map[string]pvfversion.FileChange)
	}
	if change, ok := versionChangeForPath(c.versionHeadSnapshot, c.versionWorking, key); ok {
		c.versionChangeMap[key] = change
	} else {
		delete(c.versionChangeMap, key)
	}
}

func (c *core) sortVersionChangesLocked() {
	c.versionChanges = c.versionChanges[:0]
	for _, change := range c.versionChangeMap {
		c.versionChanges = append(c.versionChanges, change)
	}
	sort.Slice(c.versionChanges, func(left, right int) bool {
		return c.versionChanges[left].Path < c.versionChanges[right].Path
	})
}

func (c *core) rebuildVersionArtifactDirtyLocked() {
	if c.versionRepo == nil {
		c.versionArtifactDirty = nil
		return
	}
	c.versionArtifactDirty = versionArtifactDirtyForSnapshots(c.versionSavedSnapshot, c.versionWorking)
}

func versionArtifactDirtyForSnapshots(saved, working pvfversion.Snapshot) map[string]struct{} {
	dirty := make(map[string]struct{})
	keys := make(map[string]struct{}, len(saved)+len(working))
	for key := range saved {
		keys[key] = struct{}{}
	}
	for key := range working {
		keys[key] = struct{}{}
	}
	for key := range keys {
		savedEntry, hasSaved := saved[key]
		workingEntry, hasWorking := working[key]
		if hasSaved != hasWorking || (hasSaved && !sameVersionEntry(savedEntry, workingEntry)) {
			dirty[key] = struct{}{}
		}
	}
	return dirty
}

func (c *core) refreshVersionArtifactDirtyLocked(key string) {
	if c.versionArtifactDirty == nil {
		c.versionArtifactDirty = make(map[string]struct{})
	}
	saved, hasSaved := c.versionSavedSnapshot[key]
	working, hasWorking := c.versionWorking[key]
	if hasSaved != hasWorking || (hasSaved && !sameVersionEntry(saved, working)) {
		c.versionArtifactDirty[key] = struct{}{}
	} else {
		delete(c.versionArtifactDirty, key)
	}
}

func sameVersionStructure(before, after pvfversion.Snapshot) bool {
	if len(before) != len(after) {
		return false
	}
	for key, oldEntry := range before {
		newEntry, ok := after[key]
		if !ok || oldEntry.DataType != newEntry.DataType {
			return false
		}
	}
	return true
}

func changedVersionIndexesForPaths(c *core, paths []string) map[int32]struct{} {
	indexes := make(map[int32]struct{}, len(paths))
	for _, path := range paths {
		if index, exists := c.archive.Find(path); exists {
			indexes[index] = struct{}{}
		}
	}
	return indexes
}

func payloadOnlyForPaths(before, after pvfversion.Snapshot, paths []string) bool {
	for _, rawPath := range paths {
		key := pvfversion.CanonicalPath(rawPath)
		oldEntry, hadOld := before[key]
		newEntry, hasNew := after[key]
		if !hadOld || !hasNew || oldEntry.DataType != newEntry.DataType {
			return false
		}
	}
	return true
}

func (s *VersionService) materializePathsLocked(viewCommit string) ([]string, error) {
	paths := make(map[string]struct{}, len(s.c.versionChanges))
	for _, change := range s.c.versionChanges {
		paths[change.Path] = struct{}{}
	}
	if viewCommit != "" {
		fromCommit := s.c.versionViewCommit
		if fromCommit == "" {
			fromCommit = s.c.versionHead.ID
		}
		if fromCommit != viewCommit {
			changed, err := s.c.versionRepo.ChangedPathsBetween(fromCommit, viewCommit)
			if err != nil {
				return nil, err
			}
			for _, path := range changed {
				paths[path] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

func (c *core) markVersionArtifactSavedLocked(path string) error {
	if c.versionRepo == nil {
		return nil
	}
	pvfHash := ""
	if c.archive != nil {
		pvfHash = c.archive.SourceHash()
	}
	if pvfHash == "" {
		var err error
		pvfHash, err = pvfversion.HashFile(path)
		if err != nil {
			return fmt.Errorf("计算已保存 PVF 的 hash 失败: %w", err)
		}
	}
	treeHash := pvfversion.TreeHash(c.versionWorking)
	state := pvfversion.ArtifactState{PVFHash: pvfHash, TreeHash: treeHash, CommitID: c.versionHead.ID}
	if err := c.versionRepo.SetArtifactState(state); err != nil {
		return fmt.Errorf("更新版本库产物状态失败: %w", err)
	}
	c.versionSavedSnapshot = pvfversion.CloneSnapshot(c.versionWorking)
	c.versionArtifactDirty = make(map[string]struct{})
	c.versionSavedPVF = pvfHash
	c.versionSavedTree = treeHash
	c.versionSavedCommit = c.versionHead.ID
	return nil
}

// recordVersionMutationLocked records one atomic mutation for the worktree
// undo stack and updates the current logical snapshot. The caller must hold
// c.mu and must already have applied the mutation to c.archive.
func (c *core) recordVersionMutationLocked(label string, before, after pvfversion.ContentSnapshot) error {
	if c.versionRepo == nil {
		return nil
	}
	beforeSnapshot := pvfversion.SnapshotFromContent(before)
	afterSnapshot := pvfversion.SnapshotFromContent(after)
	keys := make(map[string]struct{}, len(beforeSnapshot)+len(afterSnapshot))
	for key := range beforeSnapshot {
		keys[key] = struct{}{}
	}
	for key := range afterSnapshot {
		keys[key] = struct{}{}
	}
	changed := false
	for key := range keys {
		beforeEntry, hasBefore := beforeSnapshot[key]
		afterEntry, hasAfter := afterSnapshot[key]
		if hasBefore != hasAfter || (hasBefore && !sameVersionEntry(beforeEntry, afterEntry)) {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	c.versionUndo = append(c.versionUndo, versionUndoRecord{
		Label:  label,
		Before: pvfversion.CloneContentSnapshot(before),
		After:  pvfversion.CloneContentSnapshot(after),
	})
	if c.versionWorking == nil {
		c.versionWorking = make(pvfversion.Snapshot)
	}
	for key := range beforeSnapshot {
		if _, exists := afterSnapshot[key]; !exists {
			delete(c.versionWorking, key)
		}
	}
	for key, entry := range afterSnapshot {
		c.versionWorking[key] = entry
	}
	for key := range keys {
		c.refreshVersionChangeLocked(key)
		c.refreshVersionArtifactDirtyLocked(key)
	}
	c.sortVersionChangesLocked()
	return nil
}

// Initialize creates the sidecar repository for the currently opened PVF.
func (s *VersionService) Initialize() (*VersionStatus, error) {
	s.c.mu.RLock()
	a := s.c.archive
	if a == nil {
		s.c.mu.RUnlock()
		return nil, ErrNoArchive
	}
	if s.c.versionLoading {
		s.c.mu.RUnlock()
		return nil, ErrVersionLoading
	}
	if s.c.versionRepo != nil {
		status := s.c.versionStatusLocked()
		s.c.mu.RUnlock()
		return &status, nil
	}
	if a.SourcePath() == "" {
		s.c.mu.RUnlock()
		return nil, errors.New("归档没有源文件,请先保存 PVF")
	}
	if a.Modified() {
		s.c.mu.RUnlock()
		return nil, errors.New("初始化版本库前请先保存或放弃当前修改")
	}
	working, err := pvfversion.SnapshotFromArchive(a)
	if err != nil {
		s.c.mu.RUnlock()
		return nil, err
	}
	path := a.SourcePath()
	archiveHash := a.SourceHash()
	revision := s.c.batchRevision
	s.c.mu.RUnlock()

	baseHash, err := pvfversion.HashFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 PVF 基线失败: %w", err)
	}
	if archiveHash != "" && archiveHash != baseHash {
		return nil, ErrVersionArtifactChanged
	}
	repo, err := pvfversion.Init(pvfversion.SidecarPath(path), path, baseHash, working)
	if err != nil {
		return nil, err
	}
	head, headSnapshot, meta, err := loadVersionRepositoryState(repo)
	if err != nil {
		_ = repo.Close()
		return nil, err
	}
	baseArchive, err := pvf.Open(repo.BasePath())
	if err != nil {
		_ = repo.Close()
		return nil, fmt.Errorf("打开版本基线失败: %w", err)
	}
	session, err := buildPreparedVersionSession(repo, a, head, headSnapshot, meta, working, baseArchive)
	if err != nil {
		_ = repo.Close()
		return nil, err
	}

	s.c.mu.Lock()
	if s.c.archive != a || s.c.batchRevision != revision || s.c.versionRepo != nil {
		s.c.mu.Unlock()
		_ = repo.Close()
		return nil, errors.New("初始化版本库期间归档状态已变化,请重试")
	}
	if err := s.c.attachVersionSessionLocked(session); err != nil {
		s.c.mu.Unlock()
		_ = repo.Close()
		return nil, err
	}
	status := s.c.versionStatusLocked()
	s.c.mu.Unlock()
	s.emitVersionChanged("initialized")
	return &status, nil
}

// Status returns the current version session without modifying disk state.
func (s *VersionService) Status() VersionStatus {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	return s.c.versionStatusLocked()
}

// ListChanges lists the current working-tree changes relative to HEAD.
func (s *VersionService) ListChanges(cursor, limit int) (*VersionChangePage, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		return nil, err
	}
	if s.c.versionRepo == nil {
		return nil, ErrVersionNotEnabled
	}
	changes := s.c.versionChanges
	if cursor < 0 {
		cursor = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if cursor >= len(changes) {
		return &VersionChangePage{Changes: []*VersionChange{}, NextCursor: -1, Total: len(changes)}, nil
	}
	end := cursor + limit
	if end > len(changes) {
		end = len(changes)
	}
	result := &VersionChangePage{Changes: make([]*VersionChange, 0, end-cursor), Total: len(changes), NextCursor: -1}
	for _, change := range changes[cursor:end] {
		result.Changes = append(result.Changes, versionChangeDTO(change))
	}
	if end < len(changes) {
		result.NextCursor = end
	}
	return result, nil
}

// Commit freezes the current working tree as one commit without saving the
// packed PVF artifact.
func (s *VersionService) Commit(message string) (*VersionCommit, error) {
	s.c.mu.Lock()
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if s.c.versionRepo == nil {
		s.c.mu.Unlock()
		return nil, ErrVersionNotEnabled
	}
	if s.c.archive == nil {
		s.c.mu.Unlock()
		return nil, ErrNoArchive
	}
	working := s.c.versionWorking
	changes := s.c.currentVersionChangesLocked()
	if len(changes) == 0 {
		s.c.mu.Unlock()
		return nil, errors.New("没有可提交的变更")
	}
	objects := make(map[string][]byte)
	for _, change := range changes {
		if change.AfterHash == "" {
			continue
		}
		index, ok := s.c.archive.Find(change.Path)
		if !ok {
			s.c.mu.Unlock()
			return nil, fmt.Errorf("提交文件不存在: %s", change.DisplayPath)
		}
		content, err := pvfversion.ContentSnapshotFromArchive(s.c.archive, []string{change.Path})
		if err != nil {
			s.c.mu.Unlock()
			return nil, err
		}
		value, ok := content[pvfversion.CanonicalPath(change.Path)]
		if !ok || value.Hash != change.AfterHash || index < 0 {
			s.c.mu.Unlock()
			return nil, fmt.Errorf("提交内容已变化: %s", change.DisplayPath)
		}
		objects[change.AfterHash] = pvfversion.ContentObject(value)
	}
	commit, err := s.c.versionRepo.Commit(message, changes, objects)
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	s.c.versionHead = commit
	s.c.versionHeadSnapshot = pvfversion.CloneSnapshot(working)
	// Keep the existing working snapshot. The new HEAD snapshot above is the
	// detached copy that protects it from subsequent in-memory edits.
	s.c.versionWorking = working
	s.c.versionChanges = nil
	s.c.versionChangeMap = make(map[string]pvfversion.FileChange)
	s.c.versionUndo = nil
	s.c.versionViewCommit = ""
	result := versionCommitDTO(commit)
	s.c.mu.Unlock()
	s.emitVersionChanged("committed")
	emitEvent("version:committed", result)
	return result, nil
}

// History returns main-branch commits from newest to oldest.
func (s *VersionService) History(cursor, limit int) (*VersionHistoryPage, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		return nil, err
	}
	if s.c.versionRepo == nil {
		return nil, ErrVersionNotEnabled
	}
	commits, next, err := s.c.versionRepo.History(cursor, limit)
	if err != nil {
		return nil, err
	}
	result := &VersionHistoryPage{Commits: make([]*VersionCommit, 0, len(commits)), NextCursor: next}
	for _, commit := range commits {
		result.Commits = append(result.Commits, versionCommitDTO(commit))
	}
	return result, nil
}

// ListCommitChanges lists the compact path changes belonging to one commit.
func (s *VersionService) ListCommitChanges(commitID string, cursor, limit int) (*VersionChangePage, error) {
	commitID = strings.TrimSpace(commitID)
	if commitID == "" {
		return nil, errors.New("提交 ID 不能为空")
	}
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		return nil, err
	}
	if s.c.versionRepo == nil {
		return nil, ErrVersionNotEnabled
	}
	if _, err := s.c.versionRepo.GetCommit(commitID); err != nil {
		return nil, err
	}
	changes, err := s.c.versionRepo.Changes(commitID)
	if err != nil {
		return nil, err
	}
	if cursor < 0 {
		cursor = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if cursor >= len(changes) {
		return &VersionChangePage{Changes: []*VersionChange{}, NextCursor: -1, Total: len(changes)}, nil
	}
	end := cursor + limit
	if end > len(changes) {
		end = len(changes)
	}
	result := &VersionChangePage{Changes: make([]*VersionChange, 0, end-cursor), NextCursor: -1, Total: len(changes)}
	for _, change := range changes[cursor:end] {
		result.Changes = append(result.Changes, versionChangeDTO(change))
	}
	if end < len(changes) {
		result.NextCursor = end
	}
	return result, nil
}

// Remove disables version control for the current artifact and deletes only
// its sidecar repository. The loaded PVF and its in-memory edits are kept.
func (s *VersionService) Remove() (*VersionStatus, error) {
	s.c.mu.Lock()
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if s.c.versionRepo == nil {
		s.c.mu.Unlock()
		return nil, ErrVersionNotEnabled
	}
	if s.c.archive == nil || s.c.archive.SourcePath() == "" {
		s.c.mu.Unlock()
		return nil, errors.New("当前归档没有可关联的源文件")
	}
	repositoryRoot := filepath.Clean(s.c.versionRepo.Root())
	expectedRoot := filepath.Clean(pvfversion.SidecarPath(s.c.archive.SourcePath()))
	if repositoryRoot != expectedRoot || filepath.Base(repositoryRoot) == "." || filepath.Base(repositoryRoot) == string(filepath.Separator) || !strings.HasSuffix(repositoryRoot, ".pvfine") {
		s.c.mu.Unlock()
		return nil, errors.New("版本库路径校验失败,为安全起见未删除任何文件")
	}
	if info, err := os.Lstat(repositoryRoot); err == nil && info.Mode()&os.ModeSymlink != 0 {
		s.c.mu.Unlock()
		return nil, errors.New("版本库目录是符号链接,为安全起见未删除任何文件")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		s.c.mu.Unlock()
		return nil, err
	}
	s.c.detachVersionLocked()
	removeErr := os.RemoveAll(repositoryRoot)
	status := s.c.versionStatusLocked()
	s.c.mu.Unlock()

	s.emitVersionChanged("disabled")
	if removeErr != nil {
		return &status, fmt.Errorf("取消版本控制失败,部分版本文件可能仍然存在: %w", removeErr)
	}
	return &status, nil
}

// ExportCommitFilesDialog exports the files touched by one commit. Add/modify
// entries use their after-state; deleted entries use their before-state so the
// exported directory represents every file involved in that version.
func (s *VersionService) ExportCommitFilesDialog(commitID string) (string, error) {
	commitID = strings.TrimSpace(commitID)
	if commitID == "" {
		return "", errors.New("提交 ID 不能为空")
	}
	s.c.mu.RLock()
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.RUnlock()
		return "", err
	}
	repo := s.c.versionRepo
	if repo == nil {
		s.c.mu.RUnlock()
		return "", ErrVersionNotEnabled
	}
	if _, err := repo.GetCommit(commitID); err != nil {
		s.c.mu.RUnlock()
		return "", err
	}
	changes, err := repo.Changes(commitID)
	s.c.mu.RUnlock()
	if err != nil {
		return "", err
	}
	if len(changes) == 0 {
		return "", errors.New("该版本没有可导出的文件")
	}

	dir, err := application.Get().Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(true).
		SetTitle("选择版本文件导出目录").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if s.c.versionRepo != repo {
		return "", ErrVersionNotEnabled
	}
	base := s.c.versionBaseArchive
	if base == nil {
		base, err = pvf.Open(repo.BasePath())
		if err != nil {
			return "", err
		}
	}
	seenDestinations := make(map[string]struct{}, len(changes))
	exported := 0
	for _, change := range changes {
		hash := change.AfterHash
		dataType := change.AfterType
		path := change.DisplayPath
		if hash == "" {
			hash = change.BeforeHash
			dataType = change.BeforeType
			if path == "" {
				path = change.OldDisplayPath
			}
		}
		if hash == "" || path == "" {
			continue
		}
		content, err := readVersionContent(repo, base, pvfversion.Entry{
			Path: path, DataType: dataType, Hash: hash,
		})
		if err != nil {
			return "", fmt.Errorf("读取版本文件 %q 失败: %w", path, err)
		}
		destination, err := safeExportPath(dir, path)
		if err != nil {
			return "", err
		}
		if _, exists := seenDestinations[destination]; exists {
			return "", fmt.Errorf("版本文件导出路径冲突: %s", path)
		}
		seenDestinations[destination] = struct{}{}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(destination, content.Raw, 0o644); err != nil {
			return "", err
		}
		exported++
	}
	if exported == 0 {
		return "", errors.New("该版本没有可导出的文件")
	}
	return dir, nil
}

// Undo reverses the most recent uncommitted mutation as one atomic operation.
func (s *VersionService) Undo() (*VersionStatus, error) {
	s.c.mu.Lock()
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if s.c.versionRepo == nil {
		s.c.mu.Unlock()
		return nil, ErrVersionNotEnabled
	}
	if len(s.c.versionUndo) == 0 {
		s.c.mu.Unlock()
		return nil, errors.New("没有可撤销的版本变更")
	}
	record := s.c.versionUndo[len(s.c.versionUndo)-1]
	paths := make([]string, 0, len(record.Before)+len(record.After))
	seenPaths := make(map[string]struct{}, len(record.Before)+len(record.After))
	for key := range record.Before {
		seenPaths[key] = struct{}{}
		paths = append(paths, key)
	}
	for key := range record.After {
		if _, exists := seenPaths[key]; exists {
			continue
		}
		paths = append(paths, key)
	}
	if err := applyVersionContentPathsLocked(s.c, paths, record.Before); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	for key, content := range record.Before {
		s.c.versionWorking[key] = content.Entry
	}
	for key := range record.After {
		if _, exists := record.Before[key]; !exists {
			delete(s.c.versionWorking, key)
		}
	}
	for key := range record.Before {
		s.c.refreshVersionChangeLocked(key)
		s.c.refreshVersionArtifactDirtyLocked(key)
	}
	for key := range record.After {
		s.c.refreshVersionChangeLocked(key)
		s.c.refreshVersionArtifactDirtyLocked(key)
	}
	s.c.sortVersionChangesLocked()
	s.c.versionUndo = s.c.versionUndo[:len(s.c.versionUndo)-1]
	status := s.c.versionStatusLocked()
	info := s.c.archive.Info()
	s.c.mu.Unlock()
	s.c.startSearchIndex()
	emitEvent("archive:reloaded", info)
	s.emitVersionChanged("undo")
	return &status, nil
}

// Discard restores the current HEAD into the in-memory working archive.
func (s *VersionService) Discard() (*VersionStatus, error) {
	s.c.mu.Lock()
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if s.c.versionRepo == nil {
		s.c.mu.Unlock()
		return nil, ErrVersionNotEnabled
	}
	target := pvfversion.CloneSnapshot(s.c.versionHeadSnapshot)
	if err := s.materializeLocked(target, ""); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	status := s.c.versionStatusLocked()
	info := s.c.archive.Info()
	s.c.mu.Unlock()
	s.c.startSearchIndex()
	emitEvent("archive:reloaded", info)
	s.emitVersionChanged("discarded")
	return &status, nil
}

// Checkout loads a historical logical snapshot into the working tree. V1
// keeps main HEAD unchanged; committing afterwards creates a new commit on
// the current main line.
func (s *VersionService) Checkout(commitID string) (*VersionStatus, error) {
	commitID = strings.TrimSpace(commitID)
	if commitID == "" {
		return nil, errors.New("提交 ID 不能为空")
	}
	s.c.mu.Lock()
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if s.c.versionRepo == nil {
		s.c.mu.Unlock()
		return nil, ErrVersionNotEnabled
	}
	target, err := s.c.versionRepo.Snapshot(commitID)
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if err := s.materializeLocked(target, commitID); err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	status := s.c.versionStatusLocked()
	info := s.c.archive.Info()
	s.c.mu.Unlock()
	s.c.startSearchIndex()
	emitEvent("archive:reloaded", info)
	s.emitVersionChanged("checkout")
	emitEvent("version:checked-out", status)
	return &status, nil
}

// Diff returns a single commit change, including logical text where possible.
func (s *VersionService) Diff(commitID, path string) (*VersionFileDiff, error) {
	commitID = strings.TrimSpace(commitID)
	path = pvfversion.CanonicalPath(path)
	if commitID == "" || path == "" {
		return nil, errors.New("提交 ID 和文件路径不能为空")
	}
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if err := s.c.ensureVersionReadyLocked(); err != nil {
		return nil, err
	}
	if s.c.versionRepo == nil {
		return nil, ErrVersionNotEnabled
	}
	if _, err := s.c.versionRepo.GetCommit(commitID); err != nil {
		return nil, err
	}
	changes, err := s.c.versionRepo.Changes(commitID)
	if err != nil {
		return nil, err
	}
	var change *pvfversion.FileChange
	for index := range changes {
		if pvfversion.CanonicalPath(changes[index].Path) == path {
			change = &changes[index]
			break
		}
	}
	if change == nil {
		return nil, fmt.Errorf("提交中没有文件变更: %s", path)
	}
	result := &VersionFileDiff{
		Path:       change.DisplayPath,
		Operation:  string(change.Operation),
		BeforeHash: change.BeforeHash,
		AfterHash:  change.AfterHash,
		BeforeType: change.BeforeType,
		AfterType:  change.AfterType,
	}
	base, err := pvf.Open(s.c.versionRepo.BasePath())
	if err != nil {
		return nil, err
	}
	if change.BeforeHash != "" {
		before, err := readVersionContent(s.c.versionRepo, base, pvfversion.Entry{
			Path: change.DisplayPath, DataType: change.BeforeType, Hash: change.BeforeHash,
		})
		if err != nil {
			return nil, err
		}
		if before.DataType == pvf.TypeScript || before.DataType == pvf.TypeUnicode {
			result.BeforeText = string(before.Raw)
			result.TextAvailable = true
		}
	}
	if change.AfterHash != "" {
		after, err := readVersionContent(s.c.versionRepo, base, pvfversion.Entry{
			Path: change.DisplayPath, DataType: change.AfterType, Hash: change.AfterHash,
		})
		if err != nil {
			return nil, err
		}
		if after.DataType == pvf.TypeScript || after.DataType == pvf.TypeUnicode {
			result.AfterText = string(after.Raw)
			result.TextAvailable = true
		}
	}
	return result, nil
}

func (s *VersionService) materializeLocked(target pvfversion.Snapshot, viewCommit string) error {
	if s.c.archive == nil || s.c.versionRepo == nil {
		return ErrNoArchive
	}
	changePaths, err := s.materializePathsLocked(viewCommit)
	if err != nil {
		return err
	}
	payloadOnly := payloadOnlyForPaths(s.c.versionWorking, target, changePaths)
	if len(changePaths) == 0 {
		payloadOnly = sameVersionStructure(s.c.versionWorking, target)
	}
	materializePaths := changePaths
	if len(changePaths) == 0 && !payloadOnly {
		// This is only a defensive fallback for a malformed/incomplete change
		// set; a normal main-branch transition always has path identities.
		materializePaths = nil
	}
	materialized, err := materializeVersionArchive(
		s.c.versionRepo.BasePath(), s.c.versionBaseArchive, target,
		s.c.archive, s.c.versionWorking, s.c.versionRepo, materializePaths, s.c.archive.SourcePath(),
	)
	if err != nil {
		return err
	}
	if payloadOnly {
		changedIndexes := changedVersionIndexesForPaths(s.c, changePaths)
		if err := s.c.replaceArchivePayloadLocked(materialized, changedIndexes); err != nil {
			return err
		}
	} else if err := s.c.replaceArchiveLocked(materialized); err != nil {
		return err
	}
	// target is a detached snapshot owned by this operation; retaining it
	// avoids another full map clone after materialization.
	s.c.versionWorking = target
	if materializePaths == nil {
		s.c.rebuildVersionChangesLocked()
		s.c.rebuildVersionArtifactDirtyLocked()
	} else {
		for _, path := range materializePaths {
			key := pvfversion.CanonicalPath(path)
			s.c.refreshVersionChangeLocked(key)
			s.c.refreshVersionArtifactDirtyLocked(key)
		}
		s.c.sortVersionChangesLocked()
	}
	s.c.versionUndo = nil
	s.c.versionViewCommit = viewCommit
	return nil
}

func (s *VersionService) emitVersionChanged(reason string) {
	emitVersionState(s.c, reason)
}

func emitVersionState(c *core, reason string) {
	c.mu.RLock()
	status := c.versionStatusLocked()
	changes := c.currentVersionChangesLocked()
	changeDTOs := make([]*VersionChange, 0, len(changes))
	for _, change := range changes {
		changeDTOs = append(changeDTOs, versionChangeDTO(change))
	}
	c.mu.RUnlock()
	emitEvent("version:changed", map[string]any{
		"reason": reason, "status": status, "changes": changeDTOs,
	})
}

func (c *core) startVersionLoad(path string, archive *pvf.Archive) {
	if !versionSidecarExists(path) {
		return
	}

	c.mu.Lock()
	if c.archive != archive {
		c.mu.Unlock()
		return
	}
	c.versionLoadID++
	loadID := c.versionLoadID
	c.versionLoading = true
	c.versionLoadError = ""
	c.mu.Unlock()

	go func() {
		session, err := prepareVersionedSession(path, archive)
		if err != nil {
			c.finishVersionLoad(archive, loadID, err)
			return
		}
		if session == nil {
			c.finishVersionLoad(archive, loadID, nil)
			return
		}

		var children map[string][]*TreeNode
		var paths []pathEntry
		if session.archive != archive {
			children, paths, err = buildIndex(session.archive)
			if err != nil {
				_ = session.repo.Close()
				c.finishVersionLoad(archive, loadID, err)
				return
			}
		}

		c.mu.Lock()
		if !c.versionLoadCurrentLocked(archive, loadID) {
			c.mu.Unlock()
			_ = session.repo.Close()
			return
		}
		if session.archive != archive {
			c.installArchiveIndexesLocked(session.archive, children, paths)
		}
		if err := c.attachVersionSessionLocked(session); err != nil {
			c.versionLoading = false
			c.versionLoadError = err.Error()
			c.mu.Unlock()
			_ = session.repo.Close()
			emitVersionState(c, "load-error")
			return
		}
		c.versionLoading = false
		c.versionLoadError = ""
		info := c.archive.Info()
		reloaded := session.archive != archive
		c.mu.Unlock()

		if reloaded {
			c.startSearchIndex()
			emitEvent("archive:reloaded", info)
		}
		emitVersionState(c, "loaded")
	}()
}

func versionSidecarExists(path string) bool {
	_, err := os.Stat(pvfversion.SidecarPath(path))
	return err == nil
}

func (c *core) versionLoadCurrentLocked(archive *pvf.Archive, loadID uint64) bool {
	return c.archive == archive && c.versionLoading && c.versionLoadID == loadID
}

func (c *core) finishVersionLoad(archive *pvf.Archive, loadID uint64, loadErr error) {
	c.mu.Lock()
	if !c.versionLoadCurrentLocked(archive, loadID) {
		c.mu.Unlock()
		return
	}
	c.versionLoading = false
	if loadErr != nil {
		c.versionLoadError = loadErr.Error()
	} else {
		c.versionLoadError = ""
	}
	c.mu.Unlock()
	if loadErr != nil {
		emitVersionState(c, "load-error")
		return
	}
	emitVersionState(c, "loaded")
}

func loadVersionRepositoryState(repo *pvfversion.Repository) (pvfversion.Commit, pvfversion.Snapshot, pvfversion.Meta, error) {
	if repo == nil {
		return pvfversion.Commit{}, nil, pvfversion.Meta{}, ErrVersionNotEnabled
	}
	head, err := repo.Head()
	if err != nil {
		return pvfversion.Commit{}, nil, pvfversion.Meta{}, err
	}
	headSnapshot, err := repo.Snapshot(head.ID)
	if err != nil {
		return pvfversion.Commit{}, nil, pvfversion.Meta{}, err
	}
	return head, headSnapshot, repo.Meta(), nil
}

func buildPreparedVersionSession(
	repo *pvfversion.Repository,
	archive *pvf.Archive,
	head pvfversion.Commit,
	headSnapshot pvfversion.Snapshot,
	meta pvfversion.Meta,
	working pvfversion.Snapshot,
	baseArchive *pvf.Archive,
) (*preparedVersionSession, error) {
	if repo == nil {
		return nil, ErrVersionNotEnabled
	}
	if working == nil {
		return nil, errors.New("版本工作区快照为空")
	}
	savedSnapshot := pvfversion.CloneSnapshot(working)
	if meta.Artifact.CommitID != "" {
		var err error
		savedSnapshot, err = repo.Snapshot(meta.Artifact.CommitID)
		if err != nil {
			return nil, err
		}
	}
	changes, changeMap := versionChangesForSnapshots(headSnapshot, working)
	return &preparedVersionSession{
		archive:       archive,
		repo:          repo,
		head:          head,
		headSnapshot:  headSnapshot,
		working:       working,
		savedSnapshot: savedSnapshot,
		meta:          meta,
		baseArchive:   baseArchive,
		changes:       changes,
		changeMap:     changeMap,
		artifactDirty: versionArtifactDirtyForSnapshots(savedSnapshot, working),
	}, nil
}

// prepareVersionedSession discovers an existing sidecar and restores an
// unsaved HEAD in memory. It is called only from the background loader; all
// disk IO, snapshot reconstruction and the baseline PVF parse stay off c.mu.
func prepareVersionedSession(path string, archive *pvf.Archive) (*preparedVersionSession, error) {
	repo, err := pvfversion.Open(pvfversion.SidecarPath(path))
	if errors.Is(err, pvfversion.ErrRepositoryNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = repo.Close()
		}
	}()
	head, headSnapshot, meta, err := loadVersionRepositoryState(repo)
	if err != nil {
		return nil, err
	}
	sourceHash := archive.SourceHash()
	if meta.Artifact.PVFHash != "" && sourceHash != meta.Artifact.PVFHash {
		return nil, ErrVersionArtifactChanged
	}
	working := pvfversion.Snapshot(nil)
	preparedArchive := archive
	var baseArchive *pvf.Archive
	if meta.Artifact.CommitID != "" && meta.Artifact.CommitID != head.ID {
		artifactSnapshot, err := repo.Snapshot(meta.Artifact.CommitID)
		if err != nil {
			return nil, err
		}
		changedPaths, err := repo.ChangedPathsBetween(meta.Artifact.CommitID, head.ID)
		if err != nil {
			return nil, err
		}
		baseArchive, err = pvf.Open(repo.BasePath())
		if err != nil {
			return nil, fmt.Errorf("打开版本基线失败: %w", err)
		}
		preparedArchive, err = materializeVersionArchive(
			repo.BasePath(), baseArchive, headSnapshot, archive, artifactSnapshot, repo,
			changedPaths, path,
		)
		if err != nil {
			return nil, err
		}
		working = pvfversion.CloneSnapshot(headSnapshot)
	} else if meta.Artifact.CommitID == head.ID && sourceHash == meta.Artifact.PVFHash {
		// The packed artifact already represents HEAD, so re-use the restored
		// manifest instead of decoding every logical file again.
		working = pvfversion.CloneSnapshot(headSnapshot)
	} else {
		working, err = pvfversion.SnapshotFromArchive(archive)
		if err != nil {
			return nil, err
		}
		if meta.Artifact.TreeHash != "" && pvfversion.TreeHash(working) != meta.Artifact.TreeHash {
			return nil, ErrVersionArtifactChanged
		}
	}
	if baseArchive == nil {
		baseArchive, err = pvf.Open(repo.BasePath())
		if err != nil {
			return nil, fmt.Errorf("打开版本基线失败: %w", err)
		}
	}
	session, err := buildPreparedVersionSession(
		repo, preparedArchive, head, headSnapshot, meta, working, baseArchive,
	)
	if err != nil {
		return nil, err
	}
	closeOnError = false
	return session, nil
}

func materializeVersionArchive(
	basePath string,
	baseArchive *pvf.Archive,
	target pvfversion.Snapshot,
	currentArchive *pvf.Archive,
	currentSnapshot pvfversion.Snapshot,
	repo *pvfversion.Repository,
	changePaths []string,
	outputPath string,
) (*pvf.Archive, error) {
	base := baseArchive
	if base == nil {
		var err error
		base, err = pvf.Open(basePath)
		if err != nil {
			return nil, err
		}
	}
	archive := base
	if currentArchive != nil {
		archive = currentArchive
	}
	archive = archive.CloneForBatch()
	keys := make([]string, 0)
	if changePaths == nil {
		remove := make([]int32, 0)
		for index := int32(0); index < archive.FileCount(); index++ {
			if _, exists := target[pvfversion.CanonicalPath(archive.Path(index))]; !exists {
				remove = append(remove, index)
			}
		}
		if len(remove) > 0 {
			if _, err := archive.RemoveFiles(remove); err != nil {
				return nil, err
			}
		}
		keys = make([]string, 0, len(target))
		for key := range target {
			keys = append(keys, key)
		}
	} else {
		seen := make(map[string]struct{}, len(changePaths))
		for _, rawPath := range changePaths {
			key := pvfversion.CanonicalPath(rawPath)
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	for _, key := range keys {
		entry, targetExists := target[key]
		index, exists := archive.Find(key)
		if !targetExists {
			if exists {
				if _, err := archive.RemoveFiles([]int32{index}); err != nil {
					return nil, err
				}
			}
			continue
		}
		if exists {
			if value, ok := currentSnapshot[key]; ok && sameVersionEntry(value, entry) {
				continue
			}
		}
		content, err := readVersionContent(repo, base, entry)
		if err != nil {
			return nil, err
		}
		if exists && archive.File(index).DataType != entry.DataType {
			if _, err := archive.RemoveFiles([]int32{index}); err != nil {
				return nil, err
			}
			exists = false
		}
		if !exists {
			index := archive.AddFile(entry.Path, nil, entry.DataType)
			if err := setVersionContent(archive, index, content); err != nil {
				return nil, err
			}
			continue
		}
		if err := setVersionContent(archive, index, content); err != nil {
			return nil, err
		}
	}
	archive.SetSourcePath(outputPath)
	return archive, nil
}

func readVersionContent(repo *pvfversion.Repository, base *pvf.Archive, entry pvfversion.Entry) (pvfversion.Content, error) {
	if object, err := repo.ReadObject(entry.Hash); err == nil {
		if pvfversion.HashBytes(object) != entry.Hash {
			return pvfversion.Content{}, fmt.Errorf("版本对象 hash 校验失败: %s", entry.Path)
		}
		dataType, data, decodeErr := pvfversion.DecodeContent(object)
		if decodeErr != nil {
			return pvfversion.Content{}, decodeErr
		}
		if dataType != entry.DataType {
			return pvfversion.Content{}, fmt.Errorf("版本对象类型不匹配: %s", entry.Path)
		}
		return pvfversion.Content{Entry: entry, Raw: data}, nil
	}
	values, err := pvfversion.ContentSnapshotFromArchive(base, []string{entry.Path})
	if err != nil {
		return pvfversion.Content{}, err
	}
	value, ok := values[pvfversion.CanonicalPath(entry.Path)]
	if !ok || value.Hash != entry.Hash || value.DataType != entry.DataType {
		return pvfversion.Content{}, fmt.Errorf("找不到版本对象: %s", entry.Path)
	}
	return value, nil
}

func setVersionContent(archive *pvf.Archive, index int32, content pvfversion.Content) error {
	switch content.DataType {
	case pvf.TypeScript, pvf.TypeUnicode:
		return archive.SetText(index, string(content.Raw))
	default:
		return archive.SetRawBytes(index, content.Raw)
	}
}

func applyVersionContentPathsLocked(c *core, paths []string, desired pvfversion.ContentSnapshot) error {
	remove := make([]int32, 0)
	for _, key := range paths {
		index, exists := c.archive.Find(key)
		want, hasWant := desired[pvfversion.CanonicalPath(key)]
		if !exists {
			continue
		}
		if !hasWant || c.archive.File(index).DataType != want.DataType {
			remove = append(remove, index)
		}
	}
	structural := len(remove) > 0
	if len(remove) > 0 {
		if _, err := c.archive.RemoveFiles(remove); err != nil {
			return err
		}
	}
	touchedIndexes := make(map[int32]struct{}, len(desired))
	for key, content := range desired {
		index, exists := c.archive.Find(key)
		if !exists {
			index := c.archive.AddFile(content.Path, nil, content.DataType)
			if err := setVersionContent(c.archive, index, content); err != nil {
				return err
			}
			structural = true
			continue
		}
		if current, ok := c.versionWorking[key]; ok && sameVersionEntry(current, content.Entry) {
			continue
		}
		if err := setVersionContent(c.archive, index, content); err != nil {
			return err
		}
		touchedIndexes[index] = struct{}{}
	}
	if structural {
		if err := c.rebuildArchiveIndexesLocked(c.archive); err != nil {
			return err
		}
	} else {
		refreshArchiveIndexMetadataLocked(c, touchedIndexes)
	}
	c.editorText = make(map[int32]string)
	c.editorAnnotation = editorAnnotationCache{}
	c.annotationRelations = make(map[string]map[string]*relationTarget)
	c.invalidateAdvancedSearchLocked()
	return nil
}

// refreshArchiveIndexMetadataLocked updates size/type fields after payload-only
// changes. Paths and directory membership are unchanged, so rebuilding the
// complete tree index is unnecessary for ordinary text undo operations.
func refreshArchiveIndexMetadataLocked(c *core, indexes map[int32]struct{}) {
	if len(indexes) == 0 || c.archive == nil {
		return
	}
	for index := range c.sortedPaths {
		entry := &c.sortedPaths[index]
		if _, ok := indexes[entry.idx]; !ok {
			continue
		}
		file := c.archive.File(entry.idx)
		entry.size = file.DataSize
		entry.typ = file.DataType
	}
	for _, children := range c.dirChildren {
		for _, node := range children {
			if node.IsDir {
				continue
			}
			if _, ok := indexes[node.FileIndex]; !ok {
				continue
			}
			file := c.archive.File(node.FileIndex)
			node.Size = file.DataSize
			node.DataType = file.DataType
		}
	}
}

func versionChangeDTO(change pvfversion.FileChange) *VersionChange {
	return &VersionChange{
		Path: change.DisplayPath, Operation: string(change.Operation),
		BeforeHash: change.BeforeHash, AfterHash: change.AfterHash,
		BeforeType: change.BeforeType, AfterType: change.AfterType,
	}
}

func versionCommitDTO(commit pvfversion.Commit) *VersionCommit {
	return &VersionCommit{
		ID: commit.ID, ParentID: commit.ParentID, Message: commit.Message,
		CreatedAt: commit.CreatedAt, ChangeCount: commit.ChangeCount,
	}
}

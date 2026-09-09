package version

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	repositoryFormat = 1
	hashAlgorithm    = "sha256"
	mainRef          = "main"
	snapshotCacheMax = 4
)

var (
	ErrRepositoryNotFound = errors.New("版本库不存在")
	ErrRepositoryCorrupt  = errors.New("版本库损坏")
)

// Repository is a local sidecar version database and immutable object store.
type Repository struct {
	mu            sync.Mutex
	root          string
	db            *sql.DB
	objects       *ObjectStore
	meta          Meta
	baseSnapshot  Snapshot
	snapshotCache map[string]Snapshot
	snapshotOrder []string
	commitCache   map[string]Commit
}

// SidecarPath returns the repository directory for an artifact path.
func SidecarPath(artifactPath string) string { return artifactPath + ".pvfine" }

// Open opens an existing sidecar repository.
func Open(root string) (*Repository, error) {
	root = filepath.Clean(root)
	if _, err := os.Stat(filepath.Join(root, "repo.db")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrRepositoryNotFound
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "repo.db"))
	if err != nil {
		return nil, err
	}
	repo := &Repository{
		root:          root,
		db:            db,
		objects:       newObjectStore(filepath.Join(root, "objects")),
		snapshotCache: make(map[string]Snapshot),
		commitCache:   make(map[string]Commit),
	}
	if err := repo.configure(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repo.loadMeta(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

// Init creates a repository with one immutable base snapshot and an initial
// commit.  The caller must pass an unmodified archive snapshot.
func Init(root, baseFilePath, baseHash string, base Snapshot) (*Repository, error) {
	root = filepath.Clean(root)
	if _, err := os.Stat(filepath.Join(root, "repo.db")); err == nil {
		return nil, fmt.Errorf("版本库已存在: %s", root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "objects"), 0o755); err != nil {
		return nil, err
	}
	basePath := filepath.Join(root, "base.pvf")
	if err := copyFileAtomic(baseFilePath, basePath); err != nil {
		return nil, fmt.Errorf("保存版本基线失败: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "repo.db"))
	if err != nil {
		_ = os.Remove(basePath)
		return nil, err
	}
	repo := &Repository{
		root:          root,
		db:            db,
		objects:       newObjectStore(filepath.Join(root, "objects")),
		snapshotCache: make(map[string]Snapshot),
		commitCache:   make(map[string]Commit),
	}
	if err := repo.configure(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repo.initialize(baseFilePath, baseHash, base); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

func (r *Repository) configure() error {
	statements := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS refs (
			name TEXT PRIMARY KEY,
			commit_id TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS commits (
			id TEXT PRIMARY KEY,
			parent_id TEXT,
			message TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			change_count INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS changes (
			commit_id TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			path TEXT NOT NULL,
			display_path TEXT NOT NULL,
			old_path TEXT NOT NULL DEFAULT '',
			old_display_path TEXT NOT NULL DEFAULT '',
			operation TEXT NOT NULL,
			before_hash TEXT NOT NULL DEFAULT '',
			after_hash TEXT NOT NULL DEFAULT '',
			before_type INTEGER NOT NULL DEFAULT 0,
			after_type INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (commit_id, sequence),
			FOREIGN KEY (commit_id) REFERENCES commits(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS changes_path_idx ON changes(path, commit_id)`,
		`CREATE TABLE IF NOT EXISTS base_entries (
			path TEXT PRIMARY KEY,
			display_path TEXT NOT NULL,
			data_type INTEGER NOT NULL,
			hash TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := r.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) initialize(baseFilePath, baseHash string, base Snapshot) error {
	rootID, err := newID()
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixNano()
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()
	meta := map[string]string{
		"format":             fmt.Sprint(repositoryFormat),
		"base_file":          filepath.Base(baseFilePath),
		"base_hash":          baseHash,
		"hash_algorithm":     hashAlgorithm,
		"artifact_hash":      baseHash,
		"artifact_tree_hash": TreeHash(base),
		"artifact_commit_id": rootID,
	}
	for key, value := range meta {
		if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)`, key, value); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO commits(id, parent_id, message, created_at, change_count) VALUES(?, NULL, ?, ?, 0)`,
		rootID, "初始版本", now,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO refs(name, commit_id) VALUES(?, ?)`, mainRef, rootID); err != nil {
		return err
	}
	keys := make([]string, 0, len(base))
	for key := range base {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry := base[key]
		if _, err := tx.Exec(
			`INSERT INTO base_entries(path, display_path, data_type, hash) VALUES(?, ?, ?, ?)`,
			key, entry.Path, entry.DataType, entry.Hash,
		); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	rollback = false
	r.baseSnapshot = CloneSnapshot(base)
	r.snapshotCache = make(map[string]Snapshot)
	r.snapshotOrder = nil
	r.commitCache = map[string]Commit{
		rootID: {ID: rootID, Message: "初始版本", CreatedAt: now},
	}
	return r.loadMeta()
}

func (r *Repository) loadMeta() error {
	values := make(map[string]string)
	rows, err := r.db.Query(`SELECT key, value FROM meta`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if values["format"] != "1" || values["hash_algorithm"] != hashAlgorithm || values["base_hash"] == "" {
		return fmt.Errorf("%w:不支持的格式或元数据不完整", ErrRepositoryCorrupt)
	}
	if _, err := os.Stat(filepath.Join(r.root, "base.pvf")); err != nil {
		return fmt.Errorf("%w:基线文件不存在", ErrRepositoryCorrupt)
	}
	r.meta = Meta{
		Format:        repositoryFormat,
		BaseFile:      values["base_file"],
		BaseHash:      values["base_hash"],
		HashAlgorithm: values["hash_algorithm"],
		Artifact: ArtifactState{
			PVFHash:  values["artifact_hash"],
			TreeHash: values["artifact_tree_hash"],
			CommitID: values["artifact_commit_id"],
		},
	}
	return nil
}

// Close closes the SQLite handle. It is safe to call more than once.
func (r *Repository) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db == nil {
		return nil
	}
	err := r.db.Close()
	r.db = nil
	r.baseSnapshot = nil
	r.snapshotCache = nil
	r.snapshotOrder = nil
	r.commitCache = nil
	return err
}

func (r *Repository) Root() string { return r.root }

func (r *Repository) BasePath() string { return filepath.Join(r.root, "base.pvf") }

func (r *Repository) Meta() Meta {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.meta
}

func (r *Repository) Head() (Commit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.headLocked()
}

// GetCommit returns a commit by ID and verifies that it exists in the
// repository's commit graph.
func (r *Repository) GetCommit(id string) (Commit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(id) == "" {
		return Commit{}, errors.New("提交 ID 不能为空")
	}
	return r.commitLocked(strings.TrimSpace(id))
}

func (r *Repository) headLocked() (Commit, error) {
	var id string
	if err := r.db.QueryRow(`SELECT commit_id FROM refs WHERE name = ?`, mainRef).Scan(&id); err != nil {
		return Commit{}, err
	}
	return r.commitLocked(id)
}

func (r *Repository) commitLocked(id string) (Commit, error) {
	if commit, ok := r.commitCache[id]; ok {
		return commit, nil
	}
	var commit Commit
	var parent sql.NullString
	if err := r.db.QueryRow(
		`SELECT id, parent_id, message, created_at, change_count FROM commits WHERE id = ?`, id,
	).Scan(&commit.ID, &parent, &commit.Message, &commit.CreatedAt, &commit.ChangeCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Commit{}, fmt.Errorf("%w:找不到提交 %s", ErrRepositoryCorrupt, id)
		}
		return Commit{}, err
	}
	if parent.Valid {
		commit.ParentID = parent.String
	}
	if r.commitCache == nil {
		r.commitCache = make(map[string]Commit)
	}
	r.commitCache[id] = commit
	return commit, nil
}

// History walks the main branch from HEAD backwards. Cursor is an offset in
// that chain rather than a timestamp, which keeps pagination deterministic.
func (r *Repository) History(cursor, limit int) ([]Commit, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cursor < 0 {
		cursor = 0
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	head, err := r.headLocked()
	if err != nil {
		return nil, -1, err
	}
	chain := make([]Commit, 0)
	current := head
	for {
		chain = append(chain, current)
		if current.ParentID == "" {
			break
		}
		current, err = r.commitLocked(current.ParentID)
		if err != nil {
			return nil, -1, err
		}
		if len(chain) > 1_000_000 {
			return nil, -1, fmt.Errorf("%w:提交链过长或存在循环", ErrRepositoryCorrupt)
		}
	}
	if cursor >= len(chain) {
		return []Commit{}, -1, nil
	}
	end := cursor + limit
	if end > len(chain) {
		end = len(chain)
	}
	result := make([]Commit, end-cursor)
	copy(result, chain[cursor:end])
	next := -1
	if end < len(chain) {
		next = end
	}
	return result, next, nil
}

// Changes returns one commit's compact path changes.
func (r *Repository) Changes(commitID string) ([]FileChange, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.changesLocked(commitID)
}

// ChangedPathsBetween returns the union of paths changed between two commits
// on the single main branch. It intentionally returns only identities, so a
// caller can materialize the target snapshot without scanning every file.
func (r *Repository) ChangedPathsBetween(fromID, toID string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fromID = strings.TrimSpace(fromID)
	toID = strings.TrimSpace(toID)
	if fromID == "" || toID == "" {
		return nil, errors.New("起止提交 ID 不能为空")
	}
	if fromID == toID {
		return []string{}, nil
	}

	fromAncestors := make(map[string]struct{})
	for currentID := fromID; currentID != ""; {
		if _, exists := fromAncestors[currentID]; exists {
			return nil, fmt.Errorf("%w:提交链存在循环", ErrRepositoryCorrupt)
		}
		fromAncestors[currentID] = struct{}{}
		commit, err := r.commitLocked(currentID)
		if err != nil {
			return nil, err
		}
		currentID = commit.ParentID
		if len(fromAncestors) > 1_000_000 {
			return nil, fmt.Errorf("%w:提交链过长", ErrRepositoryCorrupt)
		}
	}

	pathSet := make(map[string]struct{})
	addCommitChanges := func(commitID string) error {
		changes, err := r.changesLocked(commitID)
		if err != nil {
			return err
		}
		for _, change := range changes {
			pathSet[CanonicalPath(change.Path)] = struct{}{}
		}
		return nil
	}

	// Walk from the target backwards until the first common ancestor. This
	// covers the target-only side of the interval.
	targetOnly := make([]string, 0)
	commonID := ""
	for currentID := toID; currentID != ""; {
		if _, common := fromAncestors[currentID]; common {
			commonID = currentID
			break
		}
		targetOnly = append(targetOnly, currentID)
		commit, err := r.commitLocked(currentID)
		if err != nil {
			return nil, err
		}
		currentID = commit.ParentID
		if len(targetOnly) > 1_000_000 {
			return nil, fmt.Errorf("%w:提交链过长", ErrRepositoryCorrupt)
		}
	}
	if commonID == "" {
		return nil, fmt.Errorf("%w:提交不属于同一主分支", ErrRepositoryCorrupt)
	}
	for _, commitID := range targetOnly {
		if err := addCommitChanges(commitID); err != nil {
			return nil, err
		}
	}

	// Walk from the source back to the common ancestor. These are the source
	// commits that must be undone when moving to the target.
	for currentID := fromID; currentID != commonID; {
		if err := addCommitChanges(currentID); err != nil {
			return nil, err
		}
		commit, err := r.commitLocked(currentID)
		if err != nil {
			return nil, err
		}
		currentID = commit.ParentID
		if currentID == "" {
			return nil, fmt.Errorf("%w:找不到共同祖先", ErrRepositoryCorrupt)
		}
	}

	result := make([]string, 0, len(pathSet))
	for path := range pathSet {
		if path != "" {
			result = append(result, path)
		}
	}
	sort.Strings(result)
	return result, nil
}

func (r *Repository) changesLocked(commitID string) ([]FileChange, error) {
	rows, err := r.db.Query(`
		SELECT path, display_path, old_path, old_display_path, operation,
		       before_hash, after_hash, before_type, after_type
		FROM changes WHERE commit_id = ? ORDER BY sequence`, commitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]FileChange, 0)
	for rows.Next() {
		var change FileChange
		if err := rows.Scan(
			&change.Path, &change.DisplayPath, &change.OldPath, &change.OldDisplayPath,
			&change.Operation, &change.BeforeHash, &change.AfterHash,
			&change.BeforeType, &change.AfterType,
		); err != nil {
			return nil, err
		}
		result = append(result, change)
	}
	return result, rows.Err()
}

// Snapshot reconstructs a commit's logical tree from the base manifest and
// its parent deltas.
func (r *Repository) Snapshot(commitID string) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if commitID == "" {
		commit, err := r.headLocked()
		if err != nil {
			return nil, err
		}
		commitID = commit.ID
	}
	return r.snapshotLocked(commitID)
}

func (r *Repository) baseSnapshotLocked() (Snapshot, error) {
	if r.baseSnapshot != nil {
		return CloneSnapshot(r.baseSnapshot), nil
	}
	rows, err := r.db.Query(`SELECT path, display_path, data_type, hash FROM base_entries ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(Snapshot)
	for rows.Next() {
		var key, path, hash string
		var dataType int32
		if err := rows.Scan(&key, &path, &dataType, &hash); err != nil {
			return nil, err
		}
		result[key] = Entry{Path: path, DataType: dataType, Hash: hash}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	r.baseSnapshot = CloneSnapshot(result)
	return result, nil
}

// PutObject stores one after-state payload before its commit transaction.
func (r *Repository) PutObject(raw []byte) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.objects.Put(raw)
}

func (r *Repository) ReadObject(hash string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.objects.Read(hash)
}

// Commit appends a commit to main after validating its parent and before
// hashes. Object writes are immutable; an interrupted database transaction
// can leave harmless unreachable objects which can be collected later.
func (r *Repository) Commit(message string, changes []FileChange, objects map[string][]byte) (Commit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	message = strings.TrimSpace(message)
	if message == "" {
		return Commit{}, errors.New("提交说明不能为空")
	}
	if len(changes) == 0 {
		return Commit{}, errors.New("没有可提交的变更")
	}
	head, err := r.headLocked()
	if err != nil {
		return Commit{}, err
	}
	headSnapshot, err := r.snapshotLocked(head.ID)
	if err != nil {
		return Commit{}, err
	}
	for _, change := range changes {
		if err := validateChange(headSnapshot, change); err != nil {
			return Commit{}, err
		}
		if change.AfterHash != "" {
			raw, ok := objects[change.AfterHash]
			if !ok {
				existing, readErr := r.objects.Read(change.AfterHash)
				if readErr != nil {
					return Commit{}, fmt.Errorf("提交 %q 缺少对象 %s: %w", change.DisplayPath, change.AfterHash, readErr)
				}
				if HashBytes(existing) != change.AfterHash {
					return Commit{}, fmt.Errorf("对象 hash 校验失败: %s", change.AfterHash)
				}
				continue
			}
			if HashBytes(raw) != change.AfterHash {
				return Commit{}, fmt.Errorf("对象 hash 校验失败: %s", change.AfterHash)
			}
			if _, err := r.objects.Put(raw); err != nil {
				return Commit{}, err
			}
		}
	}
	id, err := newID()
	if err != nil {
		return Commit{}, err
	}
	now := time.Now().UTC().UnixNano()
	tx, err := r.db.Begin()
	if err != nil {
		return Commit{}, err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec(
		`INSERT INTO commits(id, parent_id, message, created_at, change_count) VALUES(?, ?, ?, ?, ?)`,
		id, head.ID, message, now, len(changes),
	); err != nil {
		return Commit{}, err
	}
	for index, change := range changes {
		if _, err := tx.Exec(`
			INSERT INTO changes(
				commit_id, sequence, path, display_path, old_path, old_display_path,
				operation, before_hash, after_hash, before_type, after_type
			) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, index, CanonicalPath(change.Path), DisplayPath(change.DisplayPath),
			CanonicalPath(change.OldPath), DisplayPath(change.OldDisplayPath),
			string(change.Operation), change.BeforeHash, change.AfterHash,
			change.BeforeType, change.AfterType,
		); err != nil {
			return Commit{}, err
		}
	}
	if _, err := tx.Exec(`UPDATE refs SET commit_id = ? WHERE name = ?`, id, mainRef); err != nil {
		return Commit{}, err
	}
	if err := tx.Commit(); err != nil {
		return Commit{}, err
	}
	rollback = false
	commit := Commit{ID: id, ParentID: head.ID, Message: message, CreatedAt: now, ChangeCount: len(changes)}
	if r.commitCache == nil {
		r.commitCache = make(map[string]Commit)
	}
	r.commitCache[id] = commit
	nextSnapshot := CloneSnapshot(headSnapshot)
	for _, change := range changes {
		key := CanonicalPath(change.Path)
		if change.Operation == OperationDelete {
			delete(nextSnapshot, key)
			continue
		}
		nextSnapshot[key] = Entry{
			Path: change.DisplayPath, DataType: change.AfterType, Hash: change.AfterHash,
		}
	}
	if r.snapshotCache == nil {
		r.snapshotCache = make(map[string]Snapshot)
	}
	r.cacheSnapshotLocked(id, nextSnapshot)
	return commit, nil
}

func validateChange(head Snapshot, change FileChange) error {
	key := CanonicalPath(change.Path)
	entry, exists := head[key]
	switch change.Operation {
	case OperationAdd:
		if exists {
			return fmt.Errorf("新增文件已存在: %s", change.DisplayPath)
		}
		if change.AfterHash == "" {
			return fmt.Errorf("新增文件缺少内容: %s", change.DisplayPath)
		}
	case OperationModify:
		if !exists {
			return fmt.Errorf("修改文件不存在: %s", change.DisplayPath)
		}
		if entry.Hash != change.BeforeHash || entry.DataType != change.BeforeType {
			return fmt.Errorf("文件在提交前已变化: %s", change.DisplayPath)
		}
		if change.AfterHash == "" {
			return fmt.Errorf("修改文件缺少新内容: %s", change.DisplayPath)
		}
	case OperationDelete:
		if !exists {
			return fmt.Errorf("删除文件不存在: %s", change.DisplayPath)
		}
		if entry.Hash != change.BeforeHash || entry.DataType != change.BeforeType {
			return fmt.Errorf("文件在删除前已变化: %s", change.DisplayPath)
		}
	default:
		return fmt.Errorf("V1 暂不支持版本操作: %s", change.Operation)
	}
	return nil
}

func (r *Repository) snapshotLocked(commitID string) (Snapshot, error) {
	if cached, ok := r.snapshotCache[commitID]; ok {
		return CloneSnapshot(cached), nil
	}
	base, err := r.baseSnapshotLocked()
	if err != nil {
		return nil, err
	}
	chain := make([]Commit, 0)
	for currentID := commitID; currentID != ""; {
		commit, err := r.commitLocked(currentID)
		if err != nil {
			return nil, err
		}
		chain = append(chain, commit)
		currentID = commit.ParentID
		if len(chain) > 1_000_000 {
			return nil, fmt.Errorf("%w:提交链过长或存在循环", ErrRepositoryCorrupt)
		}
	}
	for index := len(chain) - 1; index >= 0; index-- {
		changes, err := r.changesLocked(chain[index].ID)
		if err != nil {
			return nil, err
		}
		for _, change := range changes {
			key := CanonicalPath(change.Path)
			if change.Operation == OperationDelete {
				delete(base, key)
				continue
			}
			base[key] = Entry{Path: change.DisplayPath, DataType: change.AfterType, Hash: change.AfterHash}
		}
	}
	r.cacheSnapshotLocked(commitID, base)
	return base, nil
}

func (r *Repository) cacheSnapshotLocked(commitID string, snapshot Snapshot) {
	if r.snapshotCache == nil {
		r.snapshotCache = make(map[string]Snapshot)
	}
	if _, exists := r.snapshotCache[commitID]; exists {
		for index, cachedID := range r.snapshotOrder {
			if cachedID == commitID {
				r.snapshotOrder = append(r.snapshotOrder[:index], r.snapshotOrder[index+1:]...)
				break
			}
		}
	}
	r.snapshotCache[commitID] = CloneSnapshot(snapshot)
	r.snapshotOrder = append(r.snapshotOrder, commitID)
	for len(r.snapshotOrder) > snapshotCacheMax {
		oldest := r.snapshotOrder[0]
		r.snapshotOrder = r.snapshotOrder[1:]
		delete(r.snapshotCache, oldest)
	}
}

// SetArtifactState records the last successful PVF save. It is intentionally
// separate from HEAD so save and commit remain independent operations.
func (r *Repository) SetArtifactState(state ArtifactState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := map[string]string{
		"artifact_hash":      state.PVFHash,
		"artifact_tree_hash": state.TreeHash,
		"artifact_commit_id": state.CommitID,
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()
	for key, value := range values {
		if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	rollback = false
	r.meta.Artifact = state
	return nil
}

func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

package mod

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const CN115Format = "115CN-mod"

var modIDPattern = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)
var resourceHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidateID(id string) error {
	if !modIDPattern.MatchString(id) {
		return fmt.Errorf("MOD 和组件 ID 仅支持小写字母、数字、-、_，长度为 1–64")
	}
	return nil
}

type cn115Manifest struct {
	Schema      int              `json:"schema"`
	Client      string           `json:"client"`
	Kind        string           `json:"kind"`
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Version     string           `json:"version"`
	Author      string           `json:"author"`
	Description string           `json:"description"`
	Components  []Component      `json:"components"`
	Requires    []string         `json:"requires,omitempty"`
	Conflicts   []string         `json:"conflicts,omitempty"`
	Files       []cn115Resource  `json:"files"`
	PVF         []cn115Operation `json:"pvf"`
}

type cn115Resource struct {
	Component string `json:"component"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Size      *int64 `json:"size"`
	SHA256    string `json:"sha256"`
}

type cn115Operation struct {
	Component string `json:"component"`
	Source    string `json:"source"`
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	Type      int32  `json:"type,omitempty"`
}

type CN115Reader struct{}
type CN115Writer struct{}

func cn115Root(root fs.FS) (fs.FS, error) {
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name() == "mod.json" {
			if !e.Type().IsRegular() {
				return nil, fmt.Errorf("mod.json 必须是普通文件")
			}
			return root, nil
		}
	}
	if len(entries) == 1 && entries[0].IsDir() {
		sub, err := fs.Sub(root, entries[0].Name())
		if err == nil {
			nested, readErr := fs.ReadDir(sub, ".")
			if readErr != nil {
				return nil, readErr
			}
			for _, e := range nested {
				if e.Name() == "mod.json" && e.Type().IsRegular() {
					return sub, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("根目录或唯一的一层包目录内缺少 mod.json")
}

func (CN115Reader) Detect(root fs.FS) (bool, error) {
	_, err := cn115Root(root)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (CN115Reader) Read(root fs.FS) (Package, error) {
	var pkg Package
	root, err := cn115Root(root)
	if err != nil {
		return pkg, err
	}
	data, err := readPackageFile(root, "mod.json", 4<<20)
	if err != nil {
		return pkg, err
	}
	var manifest cn115Manifest
	if !utf8.Valid(data) {
		return pkg, fmt.Errorf("mod.json 必须使用 UTF-8")
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return pkg, fmt.Errorf("解析 mod.json: %w", err)
	}
	if manifest.Schema != 1 || manifest.Client != "115CN" || manifest.Kind != "resource" {
		return pkg, fmt.Errorf("只支持 schema=1、client=115CN、kind=resource 的 MOD")
	}
	if err := ValidateID(manifest.ID); err != nil {
		return pkg, err
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return pkg, fmt.Errorf("MOD 名称不能为空")
	}
	if err := ValidateVersion(manifest.Version); err != nil {
		return pkg, err
	}
	pkg.Metadata = Metadata{ID: manifest.ID, Name: manifest.Name, Version: manifest.Version}
	pkg.Components = manifest.Components
	if err := validateComponents(pkg.Components); err != nil {
		return pkg, err
	}
	components := map[string]bool{}
	for _, c := range pkg.Components {
		components[c.ID] = true
	}
	for _, id := range append(append([]string{}, manifest.Requires...), manifest.Conflicts...) {
		if err := ValidateID(id); err != nil {
			return pkg, err
		}
		if id == manifest.ID {
			return pkg, fmt.Errorf("MOD 不能依赖自身或与自身冲突")
		}
	}
	if len(manifest.Requires)+len(manifest.Conflicts) > 0 {
		pkg.Warnings = append(pkg.Warnings, "包级依赖/冲突需在登录器中核对；本工具仅将所选 PVF 内容导入当前归档，不管理 MOD 安装状态")
	}
	for _, r := range manifest.Files {
		if !components[r.Component] || ValidatePath(r.Source) != nil || protectedPackagePath(r.Source) || !validResourceTarget(r.Target) || r.Size == nil || *r.Size < 0 || !resourceHashPattern.MatchString(r.SHA256) {
			return pkg, fmt.Errorf("无效的资源声明: %s", r.Target)
		}
	}
	if len(manifest.Files) > 0 {
		pkg.Warnings = append(pkg.Warnings, fmt.Sprintf("包内 %d 个 NPK/BK2 客户端资源不导入 PVF，请由登录器安装、关闭或恢复", len(manifest.Files)))
	}
	var total int
	for _, op := range manifest.PVF {
		if !components[op.Component] || ValidatePath(op.Path) != nil || op.Path != strings.ToLower(op.Path) || protectedPackagePath(op.Path) {
			return pkg, fmt.Errorf("无效的 PVF 路径或组件: %s", op.Path)
		}
		if op.Mode != "add" && op.Mode != "replace" && op.Mode != "merge" {
			return pkg, fmt.Errorf("不支持的 115CN PVF 操作 %q: %s；patch 标签补丁需完整协议，当前不执行", op.Mode, op.Path)
		}
		content, err := readPackageFile(root, op.Source, maxPayloadSize)
		if err != nil {
			return pkg, err
		}
		total += len(content)
		if total > maxPackageSize {
			return pkg, fmt.Errorf("MOD PVF 内容大小超出限制")
		}
		if !utf8.Valid(content) || strings.HasPrefix(string(content), "\ufeff") {
			return pkg, fmt.Errorf("PVF 内容必须使用无 BOM 的 UTF-8: %s", op.Source)
		}
		e := Entry{Component: op.Component, Path: op.Path, Operation: ReplaceFile, Encoding: Text, DataType: op.Type, Data: content}
		if e.DataType == 0 {
			e.DataType = 1
			if ext := path.Ext(op.Path); ext == ".str" || ext == ".nut" {
				e.DataType = 3
			}
		}
		if e.DataType != 1 && e.DataType != 3 {
			return pkg, fmt.Errorf("不支持的 PVF 类型: %s", op.Path)
		}
		switch op.Mode {
		case "add":
			e.Operation = AddFile
		case "merge":
			switch path.Ext(op.Path) {
			case ".lst":
				e.Operation = MergeList
				if e.DataType != 1 {
					return pkg, fmt.Errorf("lst 合并类型必须为 1: %s", op.Path)
				}
			case ".str":
				e.Operation = MergeStrings
				if e.DataType != 3 {
					return pkg, fmt.Errorf("str 合并类型必须为 3: %s", op.Path)
				}
			default:
				return pkg, fmt.Errorf("merge 只支持 lst/str: %s", op.Path)
			}
			e.Pairs, err = ParseMergeText(string(content), e.Operation)
			if err != nil {
				return pkg, fmt.Errorf("%s: %w", op.Source, err)
			}
			e.Data = nil
		}
		checked := e
		if checked.Operation == AddFile {
			checked.Operation = ReplaceFile
		}
		if err := validateTextEntries([]Entry{checked}, CN115Format); err != nil {
			return pkg, err
		}
		pkg.Entries = append(pkg.Entries, e)
	}
	if len(manifest.PVF)+len(manifest.Files) == 0 {
		return pkg, fmt.Errorf("MOD 没有 PVF 操作或资源")
	}
	return pkg, nil
}

func validResourceTarget(target string) bool {
	if ValidatePath(target) != nil || protectedPackagePath(target) {
		return false
	}
	p := strings.ToLower(target)
	if strings.HasPrefix(p, "imagepacks2/") || strings.HasPrefix(p, "soundpacks/") {
		return strings.Count(p, "/") == 1 && path.Ext(p) == ".npk"
	}
	return (strings.HasPrefix(p, "video/character/") || strings.HasPrefix(p, "stream/video/")) && path.Ext(p) == ".bk2"
}

func validateComponents(components []Component) error {
	if len(components) == 0 {
		return fmt.Errorf("MOD 必须声明组件")
	}
	byID := map[string]Component{}
	for _, c := range components {
		if ValidateID(c.ID) != nil || c.Title == "" {
			return fmt.Errorf("无效的组件 ID 或标题: %s", c.ID)
		}
		if _, exists := byID[c.ID]; exists {
			return fmt.Errorf("重复的组件 ID: %s", c.ID)
		}
		byID[c.ID] = c
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("组件存在循环依赖: %s", id)
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		for _, dep := range byID[id].Requires {
			if _, exists := byID[dep]; !exists {
				return fmt.Errorf("组件 %s 依赖不存在的组件 %s", id, dep)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// SelectComponents requires an explicit, valid selection. nil uses defaults;
// an empty non-nil list means no components, not "select everything".
func SelectComponents(pkg Package, ids []string) (Package, []string, error) {
	if err := validateComponents(pkg.Components); err != nil {
		return Package{}, nil, err
	}
	known := map[string]Component{}
	for _, c := range pkg.Components {
		known[c.ID] = c
	}
	if ids == nil {
		ids = []string{}
		for _, c := range pkg.Components {
			if c.DefaultSelected {
				ids = append(ids, c.ID)
			}
		}
	}
	selected := map[string]bool{}
	groups := map[string]string{}
	for _, id := range ids {
		c, exists := known[id]
		if !exists || selected[id] {
			return Package{}, nil, fmt.Errorf("未知或重复的组件: %s", id)
		}
		selected[id] = true
		if c.ExclusiveGroup != "" {
			if previous := groups[c.ExclusiveGroup]; previous != "" {
				return Package{}, nil, fmt.Errorf("互斥组件不能同时选择: %s / %s", previous, id)
			}
			groups[c.ExclusiveGroup] = id
		}
	}
	for _, id := range ids {
		for _, dep := range known[id].Requires {
			if !selected[dep] {
				return Package{}, nil, fmt.Errorf("组件 %s 需要同时选择 %s", id, dep)
			}
		}
	}
	result := pkg
	result.Entries = nil
	seen := map[string]bool{}
	for _, e := range pkg.Entries {
		if !selected[e.Component] {
			continue
		}
		if seen[e.Path] {
			return Package{}, nil, fmt.Errorf("所选组件存在重复 PVF 目标: %s", e.Path)
		}
		seen[e.Path] = true
		result.Entries = append(result.Entries, e)
	}
	for p := range seen {
		for parent := path.Dir(p); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return Package{}, nil, fmt.Errorf("PVF 文件与目录路径冲突: %s", p)
			}
		}
	}
	return result, append([]string{}, ids...), nil
}

func (CN115Writer) Validate(pkg Package) error {
	if err := ValidateID(pkg.Metadata.ID); err != nil {
		return err
	}
	plain := pkg
	plain.Entries = append([]Entry(nil), pkg.Entries...)
	for i := range plain.Entries {
		e := &plain.Entries[i]
		if e.Path != strings.ToLower(e.Path) || protectedPackagePath(e.Path) {
			return fmt.Errorf("115CN PVF 路径必须为小写且不能是敏感路径: %s", e.Path)
		}
		if e.Operation == AddFile {
			e.Operation = ReplaceFile
		}
	}
	return validateTextPackage(plain, CN115Format)
}

func (CN115Writer) Files(pkg Package) []OutputFile {
	files := []OutputFile{{Path: "mod.json"}}
	for _, e := range pkg.Entries {
		files = append(files, OutputFile{Path: "pvf/" + e.Path, EntryPath: e.Path})
	}
	return files
}

func cn115Output(pkg Package) (cn115Manifest, map[string][]byte) {
	version := pkg.Metadata.Version
	if version == "" {
		version = "1.0.0"
	}
	m := cn115Manifest{Schema: 1, Client: "115CN", Kind: "resource", ID: pkg.Metadata.ID, Name: pkg.Metadata.Name, Version: version,
		Components: []Component{{ID: "base", Title: "基础内容", DefaultSelected: true}}, Files: []cn115Resource{}, PVF: []cn115Operation{}}
	payloads := map[string][]byte{}
	for _, e := range pkg.Entries {
		mode := "replace"
		data := e.Data
		dataType := e.DataType
		switch e.Operation {
		case AddFile:
			mode = "add"
		case MergeList, MergeStrings:
			mode = "merge"
			dataType = 1
			if e.Operation == MergeStrings {
				dataType = 3
			}
			var text strings.Builder
			for _, pair := range e.Pairs {
				if e.Operation == MergeList {
					fmt.Fprintf(&text, "%s\t`%s`\n", pair.Key, pair.Value)
				} else {
					fmt.Fprintf(&text, "%s>%s\n", pair.Key, pair.Value)
				}
			}
			data = []byte(text.String())
		}
		source := "pvf/" + e.Path
		m.PVF = append(m.PVF, cn115Operation{Component: "base", Source: source, Path: e.Path, Mode: mode, Type: dataType})
		payloads[source] = data
	}
	return m, payloads
}

func (w CN115Writer) Write(root string, pkg Package) error {
	if err := w.Validate(pkg); err != nil {
		return err
	}
	m, payloads := cn115Output(pkg)
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	payloads["mod.json"] = append(manifest, '\n')
	for _, file := range w.Files(pkg) {
		dest := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, payloads[file.Path], 0644); err != nil {
			return err
		}
	}
	return nil
}

func (w CN115Writer) WriteArchive(output io.Writer, pkg Package) error {
	if err := w.Validate(pkg); err != nil {
		return err
	}
	m, payloads := cn115Output(pkg)
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	payloads["mod.json"] = append(manifest, '\n')
	z := zip.NewWriter(output)
	for _, file := range w.Files(pkg) {
		f, err := z.Create(file.Path)
		if err != nil {
			z.Close()
			return err
		}
		if _, err = f.Write(payloads[file.Path]); err != nil {
			z.Close()
			return err
		}
	}
	return z.Close()
}

package mod

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Extend110Reader reads only the package manifest and its PVF payloads.
// Resource directories are deliberately outside the archive import contract.
type Extend110Reader struct{}

func (Extend110Reader) Detect(root fs.FS) (bool, error) {
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Name() == "pack.json" {
			if !entry.Type().IsRegular() {
				return false, fmt.Errorf("pack.json 必须是普通文件")
			}
			return true, nil
		}
	}
	return false, nil
}

func (Extend110Reader) Read(root fs.FS) (Package, error) {
	var pkg Package
	if ok, err := (Extend110Reader{}).Detect(root); err != nil {
		return pkg, err
	} else if !ok {
		return pkg, fmt.Errorf("缺少 pack.json")
	}
	data, err := fs.ReadFile(root, "pack.json")
	if err != nil {
		return pkg, err
	}
	var manifest struct {
		Name    string           `json:"name"`
		Version string           `json:"version"`
		Types   map[string]int32 `json:"types"`
	}
	if !utf8.Valid(data) {
		return pkg, fmt.Errorf("pack.json 不是有效的 UTF-8")
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return pkg, fmt.Errorf("解析 pack.json: %w", err)
	}
	pkg.Metadata = Metadata{Name: manifest.Name, Version: manifest.Version}
	for p, t := range manifest.Types {
		if err := ValidatePath(p); err != nil {
			return pkg, err
		}
		if t != 1 && t != 3 {
			return pkg, fmt.Errorf("不支持的 PVF 类型: %s", p)
		}
	}
	usedTypes := map[string]bool{}
	for _, dir := range []string{"pvf", "merge"} {
		err := fs.WalkDir(root, dir, func(p string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if p == dir && errors.Is(walkErr, fs.ErrNotExist) {
					return nil
				}
				return walkErr
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("mod 不允许符号链接: %s", p)
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("mod 内容不是普通文件: %s", p)
			}
			target := strings.TrimPrefix(p, dir+"/")
			if target == p {
				return fmt.Errorf("mod 内容目录无效: %s", p)
			}
			if err := ValidatePath(target); err != nil {
				return err
			}
			// Never read local secrets accidentally placed in a package.
			if protectedPackagePath(target) {
				return fmt.Errorf("mod 包含敏感配置路径，拒绝读取")
			}
			content, err := fs.ReadFile(root, p)
			if err != nil {
				return err
			}
			if !utf8.Valid(content) || strings.HasPrefix(string(content), "\ufeff") {
				return fmt.Errorf("mod 文件必须为无 BOM 的 UTF-8: %s", p)
			}
			e := Entry{Path: target, Operation: ReplaceFile, Encoding: Text, Data: content}
			if dir == "pvf" {
				t, ok := manifest.Types[target]
				if !ok {
					return fmt.Errorf("pack.json 缺少文件类型: %s", target)
				}
				e.DataType = t
				usedTypes[target] = true
			} else {
				switch strings.ToLower(path.Ext(target)) {
				case ".lst":
					e.Operation = MergeList
				case ".str":
					e.Operation = MergeStrings
				default:
					return fmt.Errorf("不支持的合并文件: %s", target)
				}
				e.Data = nil
				e.Pairs, err = ParseMergeText(string(content), e.Operation)
				if err != nil {
					return fmt.Errorf("%s: %w", p, err)
				}
			}
			pkg.Entries = append(pkg.Entries, e)
			return nil
		})
		if err != nil {
			return pkg, err
		}
	}
	for p := range manifest.Types {
		if !usedTypes[p] {
			return pkg, fmt.Errorf("pack.json 引用了不存在的文件: %s", p)
		}
	}
	if err := (Extend110Writer{}).Validate(pkg); err != nil {
		return pkg, err
	}
	return pkg, nil
}

func protectedPackagePath(p string) bool {
	for _, part := range strings.Split(strings.ToLower(p), "/") {
		if part == ".env" || strings.HasPrefix(part, ".env.") || part == "credentials" || part == "credentials.json" || part == ".ssh" || part == ".aws" || part == ".gnupg" || part == "id_rsa" || part == "id_ed25519" || strings.HasSuffix(part, ".key") || strings.HasSuffix(part, ".pem") {
			return true
		}
	}
	return false
}

// ParseMergeText keeps string values verbatim, including spaces and empty values.
// Malformed or duplicate incoming rows fail the whole import rather than losing data.
func ParseMergeText(text string, operation Operation) ([]Pair, error) {
	if operation != MergeList && operation != MergeStrings {
		return nil, fmt.Errorf("不支持的合并操作: %s", operation)
	}
	pairs := []Pair{}
	seen := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || (operation == MergeList && trimmed == "#PVF_File") {
			continue
		}
		var pair Pair
		if operation == MergeStrings {
			key, value, ok := strings.Cut(line, ">")
			if !ok || key == "" {
				return nil, fmt.Errorf("第 %d 行不是有效的文字表条目", i+1)
			}
			pair = Pair{key, value}
		} else {
			start := strings.IndexByte(trimmed, '`')
			if start <= 0 || !strings.HasSuffix(trimmed, "`") {
				return nil, fmt.Errorf("第 %d 行不是有效的列表条目", i+1)
			}
			id := strings.TrimSpace(trimmed[:start])
			n, err := strconv.ParseUint(id, 10, 32)
			if err != nil || strconv.FormatUint(n, 10) != id {
				return nil, fmt.Errorf("第 %d 行列表编号无效", i+1)
			}
			pair = Pair{id, trimmed[start+1 : len(trimmed)-1]}
		}
		if seen[pair.Key] {
			return nil, fmt.Errorf("第 %d 行合并键重复", i+1)
		}
		seen[pair.Key] = true
		pairs = append(pairs, pair)
	}
	return pairs, nil
}

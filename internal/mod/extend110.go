package mod

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Extend110Writer struct{}

func (Extend110Writer) Files(pkg Package) []OutputFile {
	files := []OutputFile{{Path: "pack.json"}}
	for _, e := range pkg.Entries {
		dir := "pvf"
		if e.Operation != ReplaceFile {
			dir = "merge"
		}
		files = append(files, OutputFile{Path: dir + "/" + e.Path, EntryPath: e.Path})
	}
	return files
}

func (Extend110Writer) Validate(pkg Package) error {
	if err := ValidateName(pkg.Metadata.Name); err != nil {
		return err
	}
	if err := ValidateVersion(pkg.Metadata.Version); err != nil {
		return err
	}
	if len(pkg.Entries) == 0 {
		return fmt.Errorf("没有可应用的 mod 内容")
	}
	seen := make(map[string]bool)
	for _, e := range pkg.Entries {
		if err := ValidatePath(e.Path); err != nil {
			return err
		}
		key := strings.ToLower(e.Path)
		if seen[key] {
			return fmt.Errorf("mod 路径冲突: %s", e.Path)
		}
		seen[key] = true
		switch e.Operation {
		case ReplaceFile:
			if e.Encoding != Text || (e.DataType != 1 && e.DataType != 3) || !utf8.Valid(e.Data) || strings.HasPrefix(string(e.Data), "\ufeff") {
				return fmt.Errorf("110USextend 不支持文件的内容类型或编码: %s", e.Path)
			}
		case MergeList, MergeStrings:
			want := ".lst"
			if e.Operation == MergeStrings {
				want = ".str"
			}
			if !strings.EqualFold(filepath.Ext(e.Path), want) || len(e.Pairs) == 0 {
				return fmt.Errorf("无效的合并文件: %s", e.Path)
			}
			keys := make(map[string]bool)
			for _, pair := range e.Pairs {
				if pair.Key == "" || strings.ContainsAny(pair.Key+pair.Value, "\r\n\x00") || !utf8.ValidString(pair.Key+pair.Value) || keys[pair.Key] {
					return fmt.Errorf("无效或重复的合并条目: %s / %s", e.Path, pair.Key)
				}
				keys[pair.Key] = true
				if e.Operation == MergeList {
					n, err := strconv.ParseUint(pair.Key, 10, 32)
					if err != nil || strconv.FormatUint(n, 10) != pair.Key || strings.Contains(pair.Value, "`") {
						return fmt.Errorf("110USextend 列表编号或路径无法表达: %s / %s", e.Path, pair.Key)
					}
					if err := ValidatePath(pair.Value); err != nil {
						return err
					}
				} else if strings.Contains(pair.Key, ">") || strings.HasPrefix(pair.Key, "//") || strings.HasPrefix(pair.Key, "\ufeff") {
					return fmt.Errorf("110USextend 文字表键无法表达: %s / %s", e.Path, pair.Key)
				}
			}
		default:
			return fmt.Errorf("110USextend 不支持操作 %s: %s", e.Operation, e.Path)
		}
	}
	// Detect file/directory collisions too, including across pvf and merge.
	for key := range seen {
		for p := key; strings.Contains(p, "/"); {
			p = p[:strings.LastIndex(p, "/")]
			if seen[p] {
				return fmt.Errorf("mod 文件与目录路径冲突: %s", key)
			}
		}
	}
	return nil
}

func (w Extend110Writer) Write(root string, pkg Package) error {
	if err := w.Validate(pkg); err != nil {
		return err
	}
	types := make(map[string]int32)
	for _, e := range pkg.Entries {
		dir, data := "pvf", e.Data
		if e.Operation != ReplaceFile {
			dir = "merge"
			var text strings.Builder
			for _, pair := range e.Pairs {
				if e.Operation == MergeList {
					fmt.Fprintf(&text, "%s\t`%s`\n", pair.Key, pair.Value)
				} else {
					fmt.Fprintf(&text, "%s>%s\n", pair.Key, pair.Value)
				}
			}
			data = []byte(text.String())
		} else {
			types[e.Path] = e.DataType
		}
		dest := filepath.Join(root, dir, filepath.FromSlash(e.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return err
		}
	}
	version := pkg.Metadata.Version
	if version == "" {
		version = "1.0"
	}
	manifest := struct {
		Name     string           `json:"name"`
		Version  string           `json:"version"`
		Priority int              `json:"priority"`
		Enabled  bool             `json:"enabled"`
		Types    map[string]int32 `json:"types"`
	}{pkg.Metadata.Name, version, 100, true, types}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "pack.json"), append(data, '\n'), 0o644)
}

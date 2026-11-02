package services

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	modpkg "pvfine/internal/mod"
)

type ExportMergeSource struct {
	Path          string `json:"path"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	VersionNotice string `json:"versionNotice"`
}

var numericModVersion = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*$`)

func nextModVersion(version string) (string, string) {
	if !numericModVersion.MatchString(version) {
		return version, "无法自动递增此版本号，请手动修改版本号"
	}
	parts := strings.Split(version, ".")
	last := parts[len(parts)-1]
	n, _ := new(big.Int).SetString(last, 10)
	n.Add(n, big.NewInt(1))
	next := n.String()
	if len(next) < len(last) {
		next = strings.Repeat("0", len(last)-len(next)) + next
	}
	parts[len(parts)-1] = next
	result := strings.Join(parts, ".")
	if err := modpkg.ValidateVersion(result); err != nil {
		return version, "递增后的版本号超过长度限制，请手动修改版本号"
	}
	return result, ""
}

// SelectMergeDialog reads metadata only after a source has been selected and validated.
func (s *ExportService) SelectMergeDialog(formatID string) (*ExportMergeSource, error) {
	f, err := s.registry.Get(formatID)
	if err != nil {
		return nil, err
	}
	if f.Reader == nil {
		return nil, fmt.Errorf("该 mod 格式不支持读取")
	}
	source, err := application.Get().Dialog.OpenFile().CanChooseFiles(formatID == modpkg.CN115Format).
		CanChooseDirectories(true).SetTitle("选择要合并的同格式 MOD 目录或 ZIP / .dnfmod").PromptForSingleSelection()
	if err != nil || source == "" {
		return nil, err
	}
	return s.mergeSourceMetadata(source, formatID)
}

func (s *ExportService) mergeSourceMetadata(source, formatID string) (*ExportMergeSource, error) {
	pkg, err := s.readMergePackage(source, formatID)
	if err != nil {
		return nil, err
	}
	version, notice := nextModVersion(pkg.Metadata.Version)
	return &ExportMergeSource{Path: source, Name: pkg.Metadata.Name, Version: version, VersionNotice: notice}, nil
}

func (s *ExportService) readMergePackage(source, formatID string) (modpkg.Package, error) {
	f, err := s.registry.Get(formatID)
	if err != nil {
		return modpkg.Package{}, err
	}
	if f.Reader == nil {
		return modpkg.Package{}, fmt.Errorf("该 mod 格式不支持读取")
	}
	root, closer, err := modpkg.OpenSource(source, formatID == modpkg.CN115Format)
	if err != nil {
		return modpkg.Package{}, err
	}
	defer closer.Close()
	detected, err := f.Reader.Detect(root)
	if err != nil {
		return modpkg.Package{}, err
	}
	if !detected {
		return modpkg.Package{}, fmt.Errorf("所选 MOD 不符合导出格式 %s", formatID)
	}
	pkg, err := f.Reader.Read(root)
	if err != nil {
		return modpkg.Package{}, err
	}
	if len(pkg.Components) > 0 {
		ids := make([]string, 0, len(pkg.Components))
		for _, c := range pkg.Components {
			ids = append(ids, c.ID)
		}
		pkg, _, err = modpkg.SelectComponents(pkg, ids)
		if err != nil {
			return modpkg.Package{}, fmt.Errorf("无法合并已有 MOD 的全部组件: %w", err)
		}
	}
	return pkg, nil
}

// Keep base-only files and keys. Current export wins every collision.
func mergeExportPackages(base, current modpkg.Package) (modpkg.Package, error) {
	result := current
	result.Entries = append([]modpkg.Entry(nil), base.Entries...)
	positions := make(map[string]int, len(result.Entries))
	for i, e := range result.Entries {
		positions[strings.ToLower(e.Path)] = i
	}
	for _, entry := range current.Entries {
		key := strings.ToLower(entry.Path)
		i, exists := positions[key]
		if !exists {
			positions[key] = len(result.Entries)
			result.Entries = append(result.Entries, entry)
			continue
		}
		previous := result.Entries[i]
		fullTable := (previous.Operation == modpkg.ReplaceFile || previous.Operation == modpkg.AddFile) &&
			(entry.Operation == modpkg.MergeList || entry.Operation == modpkg.MergeStrings)
		if fullTable {
			pairs, err := modpkg.ParseMergeText(string(previous.Data), entry.Operation)
			if err != nil {
				return modpkg.Package{}, fmt.Errorf("无法合并已有整表 %s: %w", previous.Path, err)
			}
			previous.Pairs = pairs
		}
		if (entry.Operation == previous.Operation || fullTable) && (entry.Operation == modpkg.MergeList || entry.Operation == modpkg.MergeStrings) {
			pairs := append([]modpkg.Pair(nil), previous.Pairs...)
			keys := make(map[string]int, len(pairs))
			for j, pair := range pairs {
				keys[pair.Key] = j
			}
			for _, pair := range entry.Pairs {
				if j, ok := keys[pair.Key]; ok {
					pairs[j] = pair
				} else {
					keys[pair.Key] = len(pairs)
					pairs = append(pairs, pair)
				}
			}
			if fullTable {
				var data strings.Builder
				for _, pair := range pairs {
					if entry.Operation == modpkg.MergeList {
						fmt.Fprintf(&data, "%s\t`%s`\n", pair.Key, pair.Value)
					} else {
						fmt.Fprintf(&data, "%s>%s\n", pair.Key, pair.Value)
					}
				}
				entry.Operation, entry.Encoding, entry.DataType = previous.Operation, previous.Encoding, previous.DataType
				entry.Data, entry.Pairs = []byte(data.String()), nil
			} else {
				entry.Pairs = pairs
			}
		}
		result.Entries[i] = entry
	}
	return result, nil
}

// Package mod defines format-independent packages and separately registered
// readers and writers. It does not know about the editor or version repository.
package mod

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type Operation string
type Encoding string

const (
	ReplaceFile  Operation = "replace"
	MergeList    Operation = "merge-list"
	MergeStrings Operation = "merge-strings"
	DeleteFile   Operation = "delete"
	Text         Encoding  = "text"
	Bytes        Encoding  = "bytes"
)

type Metadata struct {
	Name    string
	Version string
}

func ValidateVersion(version string) error {
	if version == "" {
		return nil // Older callers retain the format's default version.
	}
	if !utf8.ValidString(version) || version != strings.TrimSpace(version) || len(version) > 128 {
		return fmt.Errorf("mod 版本号不能包含首尾空格或超过 128 字节")
	}
	for _, r := range version {
		if r < 32 || r == 127 {
			return fmt.Errorf("mod 版本号不能包含控制字符")
		}
	}
	return nil
}

type Pair struct {
	Key   string
	Value string
}

// Entry keeps payload representation explicit. Merge entries contain semantic
// pairs rather than format-specific source lines.
type Entry struct {
	Path      string
	Operation Operation
	Encoding  Encoding
	DataType  int32
	Data      []byte
	Pairs     []Pair
}

type Package struct {
	Metadata Metadata
	Entries  []Entry
}

func ValidateName(name string) error {
	if name == "" || name != strings.TrimSpace(name) || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return fmt.Errorf("mod 名称不能为空、包含首尾空格或以 . / _ 开头")
	}
	return validateComponent(name)
}

// ValidatePath rejects names instead of sanitizing them: renaming package
// files would invalidate references inside scripts and lists.
func ValidatePath(p string) error {
	if p == "" || strings.Contains(p, "\\") {
		return fmt.Errorf("无效的 mod 路径: %q", p)
	}
	for _, part := range strings.Split(p, "/") {
		if err := validateComponent(part); err != nil {
			return fmt.Errorf("无效的 mod 路径 %q: %w", p, err)
		}
	}
	return nil
}

func validateComponent(name string) error {
	if name == "" || name == "." || name == ".." || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("无效的文件名: %q", name)
	}
	if len(utf16.Encode([]rune(name))) > 255 {
		return fmt.Errorf("文件名过长")
	}
	for _, r := range name {
		if r < 32 || r == 127 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return fmt.Errorf("文件名包含非法字符: %q", name)
		}
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	stem = strings.NewReplacer("¹", "1", "²", "2", "³", "3").Replace(stem)
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return fmt.Errorf("文件名是 Windows 保留名称: %q", name)
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && strings.ContainsRune("123456789", rune(stem[3])) {
		return fmt.Errorf("文件名是 Windows 保留名称: %q", name)
	}
	return nil
}

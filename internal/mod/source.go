package mod

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const maxPayloadSize = 64 << 20
const maxPackageSize = 512 << 20

// OpenSource exposes a directory or a ZIP without extracting untrusted paths.
func OpenSource(name string, allowZIP bool) (fs.FS, io.Closer, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		root, err := os.OpenRoot(name)
		if err != nil {
			return nil, nil, err
		}
		return root.FS(), root, nil
	}
	if !allowZIP || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("请选择 mod 目录或支持的 ZIP / .dnfmod 文件")
	}
	if ext := strings.ToLower(filepath.Ext(name)); ext != ".zip" && ext != ".dnfmod" {
		return nil, nil, fmt.Errorf("MOD 压缩包仅支持 .zip 或 .dnfmod，不支持 RAR、7z、JAR")
	}
	z, err := zip.OpenReader(name)
	if err != nil {
		return nil, nil, fmt.Errorf("读取 MOD ZIP: %w", err)
	}
	fail := func(err error) (fs.FS, io.Closer, error) {
		z.Close()
		return nil, nil, err
	}
	if len(z.File) > 20000 {
		return fail(fmt.Errorf("MOD ZIP 文件数量超出限制"))
	}
	var total uint64
	seen := map[string]bool{}
	files := map[string]bool{}
	for _, f := range z.File {
		if f.NonUTF8 && !utf8.ValidString(f.Name) {
			decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes([]byte(f.Name))
			if err != nil {
				return fail(fmt.Errorf("ZIP 文件名编码无效"))
			}
			f.Name = string(decoded)
		}
		p := strings.TrimSuffix(f.Name, "/")
		if !utf8.ValidString(p) || ValidatePath(p) != nil || protectedPackagePath(p) {
			return fail(fmt.Errorf("ZIP 包含非法或敏感路径"))
		}
		key := strings.ToLower(p)
		if seen[key] || (!f.Mode().IsRegular() && !f.Mode().IsDir()) {
			return fail(fmt.Errorf("ZIP 路径冲突或包含非普通文件: %s", p))
		}
		seen[key] = true
		files[key] = !f.FileInfo().IsDir()
		if f.UncompressedSize64 > maxPayloadSize || total > maxPackageSize-f.UncompressedSize64 {
			return fail(fmt.Errorf("MOD ZIP 解压大小超出限制"))
		}
		total += f.UncompressedSize64
	}
	for key := range seen {
		for p := key; strings.Contains(p, "/"); {
			p = p[:strings.LastIndex(p, "/")]
			if files[p] {
				return fail(fmt.Errorf("ZIP 文件与目录冲突: %s", key))
			}
		}
	}
	return z, z, nil
}

// readPackageFile checks every path component before opening, including parent
// symlinks in directory packages. The returned content is always bounded.
func readPackageFile(root fs.FS, name string, limit int64) ([]byte, error) {
	if err := ValidatePath(name); err != nil {
		return nil, err
	}
	if protectedPackagePath(name) {
		return nil, fmt.Errorf("mod 包含敏感配置路径，拒绝读取")
	}
	dir := "."
	parts := strings.Split(name, "/")
	for i, part := range parts {
		entries, err := fs.ReadDir(root, dir)
		if err != nil {
			return nil, err
		}
		var found fs.DirEntry
		for _, entry := range entries {
			if entry.Name() == part {
				found = entry
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("mod 缺少文件: %s", name)
		}
		if found.Type()&fs.ModeSymlink != 0 || (i < len(parts)-1 && !found.IsDir()) || (i == len(parts)-1 && !found.Type().IsRegular()) {
			return nil, fmt.Errorf("mod 路径不是普通文件或包含符号链接: %s", name)
		}
		if dir == "." {
			dir = part
		} else {
			dir += "/" + part
		}
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(data)) > limit {
		return nil, fmt.Errorf("mod 文件超出大小限制: %s", name)
	}
	return data, err
}

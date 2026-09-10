package rendering

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	appconfig "pvfine/config"
)

const sourceRelativePath = "config/rendering.json"

// LoadDefault loads the embedded rendering rules used at application start.
func LoadDefault() (*Engine, error) {
	return Parse(appconfig.RenderingJSON)
}

// LoadFile loads and validates a rendering rule document from disk.
func LoadFile(filePath string) (*Engine, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取渲染规则失败: %w", err)
	}
	engine, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return engine, nil
}

// Parse parses and validates one complete JSON rendering document.
func Parse(data []byte) (*Engine, error) {
	document, err := parseDocument(data)
	if err != nil {
		return nil, err
	}
	return Compile(document)
}

// ParseDocument parses one rendering document without compiling its rules.
func parseDocument(data []byte) (Document, error) {
	var document Document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("解析渲染规则失败: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return Document{}, fmt.Errorf("渲染规则只能包含一个 JSON 文档")
		}
		return Document{}, fmt.Errorf("解析渲染规则失败: %w", err)
	}
	return document, nil
}

// Marshal validates and formats a rendering document.
func Marshal(document Document) ([]byte, error) {
	if err := Validate(document); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Validate checks the V1 rendering schema and all matching constraints.
func Validate(document Document) error {
	problems := make([]string, 0)
	if document.Version != 1 {
		problems = append(problems, fmt.Sprintf("version 必须为 1，当前为 %d", document.Version))
	}

	seenIDs := make(map[string]bool, len(document.Rules))
	for index, rule := range document.Rules {
		prefix := fmt.Sprintf("rules[%d]", index)
		id := strings.TrimSpace(rule.ID)
		if id == "" {
			problems = append(problems, prefix+".id 不能为空")
		} else if seenIDs[id] {
			problems = append(problems, prefix+".id 重复: "+rule.ID)
		}
		seenIDs[id] = true

		for _, rawExtension := range rule.Match.Extensions {
			extension := strings.ToLower(strings.TrimSpace(rawExtension))
			if extension == "" || !strings.HasPrefix(extension, ".") || strings.ContainsAny(extension, "/\\") {
				problems = append(problems, prefix+".match.extensions 必须是以点开头的文件后缀")
				break
			}
		}
		if strings.TrimSpace(rule.Match.Glob) != "" {
			if err := validateGlob(rule.Match.Glob); err != nil {
				problems = append(problems, prefix+".match.glob 无效: "+err.Error())
			}
		}

		switch rule.Target.Kind {
		case "file":
			if strings.TrimSpace(rule.Target.Section) != "" {
				problems = append(problems, prefix+".target.section 仅允许用于 section 规则")
			}
			if rule.Format.TokensPerLineIndex != nil {
				problems = append(problems, prefix+".format.tokensPerLineIndex 只允许用于 section 规则")
			}
		case "section":
			if strings.TrimSpace(rule.Target.Section) == "" {
				problems = append(problems, prefix+".target.section 不能为空")
			}
		default:
			problems = append(problems, prefix+".target.kind 必须是 file 或 section")
		}
		if rule.Format.Offset < 0 {
			problems = append(problems, prefix+".format.offset 不能为负数")
		}
		if rule.Format.TokensPerLine <= 0 {
			problems = append(problems, prefix+".format.tokensPerLine 必须是正数")
		}
		if rule.Format.TokensPerLineIndex != nil && *rule.Format.TokensPerLineIndex < 0 {
			problems = append(problems, prefix+".format.tokensPerLineIndex 不能为负数")
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("渲染规则校验失败:\n- %s", strings.Join(problems, "\n- "))
	}
	return nil
}

func validateGlob(pattern string) error {
	for _, segment := range strings.Split(normalizePath(pattern), "/") {
		if segment == "**" {
			continue
		}
		if _, err := path.Match(segment, segment); err != nil {
			return err
		}
	}
	return nil
}

// RuntimePath is the writable rule location used by packaged applications.
func RuntimePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("获取用户配置目录失败: %w", err)
	}
	return filepath.Join(configDir, "pvfine", "rendering.json"), nil
}

// FindSourcePath locates the repository rendering file during development.
func FindSourcePath() (string, bool) {
	starts := make([]string, 0, 2)
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(executable))
	}
	seen := make(map[string]struct{})
	for _, start := range starts {
		for dir := filepath.Clean(start); ; dir = filepath.Dir(dir) {
			if _, ok := seen[dir]; !ok {
				seen[dir] = struct{}{}
				candidate := filepath.Join(dir, filepath.FromSlash(sourceRelativePath))
				if fileExists(filepath.Join(dir, "go.mod")) && fileExists(candidate) {
					return candidate, true
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return "", false
}

func fileExists(filePath string) bool {
	info, err := os.Stat(filePath)
	return err == nil && !info.IsDir()
}

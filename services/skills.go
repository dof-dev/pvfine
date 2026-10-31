package services

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed builtin_skills
var builtinSkills embed.FS

type BuiltinSkill struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type SkillInstallResult struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type SkillService struct{ mu sync.Mutex }

func NewSkillService() *SkillService { return &SkillService{} }

func (s *SkillService) List() []BuiltinSkill {
	return []BuiltinSkill{
		{"pvfine-drop-rates", "掉率读取与修改", "深渊、翻牌、普通与精英怪物基础掉率，以及全局掉落权重。"},
		{"pvfine-skill-values", "技能数值读取与修改", "区分地下城与 PVP，定位静态参数、等级数据和技能说明绑定。"},
		{"pvfine-upgrade", "强化、增幅与锻造", "识别不同版本的概率刻度、失败处理、材料与金币成本。"},
		{"pvfine-npc-shops", "NPC 商店读取与修改", "定位商店分页、分类与商品，修改金币价格和兑换材料。"},
		{"pvfine-quests", "任务读取与修改", "读取与修改任务条件、NPC、对话、固定奖励与自选奖励。"},
	}
}

// ResolveDirectory returns the exact skill root shown before installation.
func (s *SkillService) ResolveDirectory(target, directory string) (string, error) {
	switch target {
	case "global", "claude":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		tool := ".agents"
		if target == "claude" {
			tool = ".claude"
		}
		return filepath.Join(home, tool, "skills"), nil
	case "project", "custom":
		directory = strings.TrimSpace(directory)
		if !filepath.IsAbs(directory) {
			return "", fmt.Errorf("请选择或输入绝对目录路径")
		}
		directory = filepath.Clean(directory)
		if target == "project" {
			return filepath.Join(directory, ".agents", "skills"), nil
		}
		return directory, nil
	default:
		return "", fmt.Errorf("未知的 skill 安装位置")
	}
}

func (s *SkillService) ChooseDirectory() (string, error) {
	return application.Get().Dialog.OpenFile().CanChooseFiles(false).CanChooseDirectories(true).
		CanCreateDirectories(true).SetTitle("选择 skill 安装目录或项目根目录").PromptForSingleSelection()
}

// Install skips existing skills unless overwrite is selected. Updates replace
// bundled files while preserving extra user files and rejecting symlink conflicts.
func (s *SkillService) Install(ids []string, target, directory string, overwrite bool) ([]SkillInstallResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.ResolveDirectory(target, directory)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("请至少选择一个 skill")
	}
	allowed := map[string]bool{}
	for _, skill := range s.List() {
		allowed[skill.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !allowed[id] || seen[id] {
			return nil, fmt.Errorf("skill 选择无效或重复")
		}
		seen[id] = true
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("创建 skill 根目录失败: %w", err)
	}
	results := make([]SkillInstallResult, 0, len(ids))
	for _, id := range ids {
		destination := filepath.Join(root, id)
		result := SkillInstallResult{ID: id, Path: destination, Status: "installed", Message: "安装成功"}
		created := false
		if err := os.Mkdir(destination, 0755); err != nil {
			if os.IsExist(err) && !overwrite {
				result.Status = "skipped"
				result.Message = "已存在，保留原内容"
			} else if os.IsExist(err) {
				result.Status = "updated"
				result.Message = "已覆盖更新"
			} else {
				result.Status = "failed"
				result.Message = err.Error()
			}
		} else {
			created = true
		}
		if result.Status == "installed" || result.Status == "updated" {
			if err := writeBuiltinSkill(id, destination); err != nil {
				if created {
					_ = os.RemoveAll(destination)
				}
				result.Status = "failed"
				result.Message = err.Error()
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func writeBuiltinSkill(id, destination string) error {
	source := "builtin_skills/" + id
	// Check every managed path before writing, so structural conflicts cannot
	// leave the entrypoint updated while a bundled reference remains outdated.
	err := fs.WalkDir(builtinSkills, source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(path, source), "/")
		output := filepath.Join(destination, filepath.FromSlash(relative))
		info, err := os.Lstat(output)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (entry.IsDir() && !info.IsDir()) || (!entry.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("安装路径存在符号链接或文件类型冲突: %s", output)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return fs.WalkDir(builtinSkills, source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(path, source), "/")
		output := filepath.Join(destination, filepath.FromSlash(relative))
		if entry.IsDir() {
			return os.MkdirAll(output, 0755)
		}
		content, err := builtinSkills.ReadFile(path)
		if err != nil {
			return err
		}
		return replaceBuiltinSkillFile(output, content)
	})
}

func replaceBuiltinSkillFile(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".pvfine-skill-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Chmod(0644); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

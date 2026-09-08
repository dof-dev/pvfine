package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	AnnotationTagAfterTarget = "after-target"
	AnnotationTagLineEnd     = "line-end"
	AnnotationTagHidden      = "hidden"
)

type AppSettings struct {
	AnnotationTagPlacement string `json:"annotationTagPlacement"`
}

func DefaultAppSettings() AppSettings {
	return AppSettings{AnnotationTagPlacement: AnnotationTagAfterTarget}
}

type SettingsService struct {
	mu      sync.Mutex
	path    string
	initErr error
}

func NewSettingsService() *SettingsService {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return &SettingsService{initErr: fmt.Errorf("获取用户配置目录失败: %w", err)}
	}
	return newSettingsService(filepath.Join(configDir, "pvfine", "settings.json"))
}

func newSettingsService(path string) *SettingsService {
	return &SettingsService{path: path}
}

func (s *SettingsService) GetSettings() (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return AppSettings{}, s.initErr
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return DefaultAppSettings(), nil
	}
	if err != nil {
		return AppSettings{}, fmt.Errorf("读取设置失败: %w", err)
	}
	settings := DefaultAppSettings()
	if err := json.Unmarshal(data, &settings); err != nil {
		return AppSettings{}, fmt.Errorf("解析设置失败: %w", err)
	}
	if err := validateSettings(settings); err != nil {
		return AppSettings{}, err
	}
	return settings, nil
}

func (s *SettingsService) SaveSettings(settings AppSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return s.initErr
	}
	if err := validateSettings(settings); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("创建设置目录失败: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("创建设置临时文件失败: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("写入设置失败: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("同步设置失败: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("关闭设置文件失败: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("替换设置文件失败: %w", err)
	}
	return nil
}

func validateSettings(settings AppSettings) error {
	switch settings.AnnotationTagPlacement {
	case AnnotationTagAfterTarget, AnnotationTagLineEnd, AnnotationTagHidden:
		return nil
	default:
		return fmt.Errorf("无效的标注 Tag 显示位置: %q", settings.AnnotationTagPlacement)
	}
}

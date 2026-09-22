package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	AnnotationTagAfterTarget = "after-target"
	AnnotationTagLineEnd     = "line-end"
	AnnotationTagHidden      = "hidden"
	ExplorerOpenSingleClick  = "single-click"
	ExplorerOpenDoubleClick  = "double-click"
	ThemeDark                = "dark"
	ThemeLight               = "light"
	ThemeSystem              = "system"

	DefaultAutosaveIntervalSeconds = 300
	minAutosaveIntervalSeconds     = 30
	maxAutosaveIntervalSeconds     = 7200
)

type AppSettings struct {
	AnnotationTagPlacement string `json:"annotationTagPlacement"`
	ExplorerOpenMode       string `json:"explorerOpenMode"`
	VimMode                bool   `json:"vimMode"`
	BackupSourceOnSave     bool   `json:"backupSourceOnSave"`
	NPKDirectory           string `json:"npkDirectory"`
	Theme                  string `json:"theme"`
	// AutosaveEnabled turns the timed workspace snapshot on. It is off by
	// default: the snapshot rewrites a whole PVF, so the user opts in.
	AutosaveEnabled bool `json:"autosaveEnabled"`
	// AutosavePath is the single-slot snapshot file. Empty means the platform
	// cache directory resolved by DefaultAutosavePath.
	AutosavePath            string `json:"autosavePath"`
	AutosaveIntervalSeconds int    `json:"autosaveIntervalSeconds"`
}

func DefaultAppSettings() AppSettings {
	return AppSettings{
		AnnotationTagPlacement:  AnnotationTagAfterTarget,
		ExplorerOpenMode:        ExplorerOpenSingleClick,
		VimMode:                 false,
		BackupSourceOnSave:      true,
		NPKDirectory:            "",
		Theme:                   ThemeDark,
		AutosaveEnabled:         false,
		AutosavePath:            "",
		AutosaveIntervalSeconds: DefaultAutosaveIntervalSeconds,
	}
}

// DefaultAutosavePath is the backup location used when the user has not
// configured one. It mirrors the search-index cache root so all derived state
// of the application stays out of the game directory.
func DefaultAutosavePath() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("获取用户缓存目录失败: %w", err)
	}
	return filepath.Join(cacheDir, "pvfine", "autosave.pvf"), nil
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
	return s.getSettingsLocked()
}

func (s *SettingsService) getSettingsLocked() (AppSettings, error) {
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
	settings = normalizeSettings(settings)
	if err := validateSettings(settings); err != nil {
		return AppSettings{}, err
	}
	return settings, nil
}

func (s *SettingsService) SaveSettings(settings AppSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveSettingsLocked(settings)
}

func (s *SettingsService) saveSettingsLocked(settings AppSettings) error {
	if s.initErr != nil {
		return s.initErr
	}
	settings = normalizeSettings(settings)
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

// UpdateNPKDirectory changes only the image resource directory while
// preserving settings added by newer versions of the application.
func (s *SettingsService) UpdateNPKDirectory(directory string) (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, err := s.getSettingsLocked()
	if err != nil {
		return AppSettings{}, err
	}
	settings.NPKDirectory = directory
	if err := s.saveSettingsLocked(settings); err != nil {
		return AppSettings{}, err
	}
	return settings, nil
}

// normalizeSettings trims typed paths and fills in the autosave interval for
// callers that never set it (legacy settings files, partial API payloads).
func normalizeSettings(settings AppSettings) AppSettings {
	settings.AutosavePath = strings.TrimSpace(settings.AutosavePath)
	if settings.AutosaveIntervalSeconds <= 0 {
		settings.AutosaveIntervalSeconds = DefaultAutosaveIntervalSeconds
	}
	return settings
}

func validateSettings(settings AppSettings) error {
	switch settings.AnnotationTagPlacement {
	case AnnotationTagAfterTarget, AnnotationTagLineEnd, AnnotationTagHidden:
	default:
		return fmt.Errorf("无效的标注 Tag 显示位置: %q", settings.AnnotationTagPlacement)
	}
	switch settings.ExplorerOpenMode {
	case ExplorerOpenSingleClick, ExplorerOpenDoubleClick:
		// Keep theme validation beside the other persisted enum settings so
		// invalid values cannot be written to the user configuration.
	default:
		return fmt.Errorf("无效的资源管理器打开方式: %q", settings.ExplorerOpenMode)
	}
	switch settings.Theme {
	case ThemeDark, ThemeLight, ThemeSystem:
	default:
		return fmt.Errorf("无效的主题: %q", settings.Theme)
	}
	if settings.AutosaveIntervalSeconds < minAutosaveIntervalSeconds ||
		settings.AutosaveIntervalSeconds > maxAutosaveIntervalSeconds {
		return fmt.Errorf(
			"定时缓存间隔需在 %d-%d 秒之间: %d",
			minAutosaveIntervalSeconds, maxAutosaveIntervalSeconds, settings.AutosaveIntervalSeconds,
		)
	}
	if settings.AutosavePath != "" && !strings.EqualFold(filepath.Ext(settings.AutosavePath), ".pvf") {
		return fmt.Errorf("定时缓存路径需要以 .pvf 结尾: %q", settings.AutosavePath)
	}
	return nil
}

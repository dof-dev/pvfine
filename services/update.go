package services

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// UpdateService exposes the native updater to platforms without a visible
// application menu, such as the Windows desktop shell.
type UpdateService struct {
	app *application.App
}

func NewUpdateService(app *application.App) *UpdateService {
	return &UpdateService{app: app}
}

// CheckForUpdates checks the configured release source and opens the Wails
// updater flow when an update is available.
func (s *UpdateService) CheckForUpdates() error {
	if s == nil || s.app == nil || s.app.Updater == nil {
		return errors.New("更新功能未初始化")
	}
	return s.app.Updater.CheckAndInstall(context.Background())
}

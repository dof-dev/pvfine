package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

type updateClient interface {
	Check(context.Context) (*updater.Release, error)
	CheckAndInstall(context.Context) error
}

// UpdateService shares one confirmation flow between startup, menu and settings.
type UpdateService struct {
	version   string
	client    updateClient
	busy      atomic.Bool
	requestID atomic.Uint64
	confirm   func(*updater.Release, func(bool))
	notify    func(string, string)
}

func NewUpdateService(app *application.App, version string) *UpdateService {
	s := &UpdateService{version: version}
	if app == nil || app.Updater == nil {
		return s
	}
	s.client = app.Updater
	s.notify = func(kind, message string) {
		app.Event.Emit("app:update-message", map[string]string{"kind": kind, "message": message})
	}
	s.confirm = func(release *updater.Release, decide func(bool)) {
		responseEvent := fmt.Sprintf("app:update-response:%d", s.requestID.Add(1))
		var once sync.Once
		var off func()
		off = app.Event.On(responseEvent, func(event *application.CustomEvent) {
			data, ok := event.Data.(map[string]any)
			if !ok {
				return
			}
			accepted, ok := data["accepted"].(bool)
			if !ok {
				return
			}
			once.Do(func() {
				off()
				go decide(accepted)
			})
		})
		app.Event.Emit("app:update-available", map[string]string{
			"currentVersion": s.version, "version": release.Version, "responseEvent": responseEvent,
		})
	}
	return s
}

// Version reports the running application version.
func (s *UpdateService) Version() string {
	if s == nil {
		return ""
	}
	return s.version
}

// CheckForUpdates checks for a release and asks before starting any download.
func (s *UpdateService) CheckForUpdates() error {
	return s.checkForUpdates(false)
}

// CheckForUpdatesInBackground leaves startup checks quiet when already up to date.
func CheckForUpdatesInBackground(s *UpdateService) error {
	return s.checkForUpdates(true)
}

func (s *UpdateService) checkForUpdates(background bool) error {
	if s == nil || s.client == nil || s.confirm == nil {
		return errors.New("更新功能未初始化")
	}
	if !s.busy.CompareAndSwap(false, true) {
		return nil
	}
	release, err := s.client.Check(context.Background())
	if err != nil || release == nil {
		s.busy.Store(false)
		if err == nil && !background {
			s.notify("info", "当前已是最新版本")
		}
		return err
	}
	var once sync.Once
	s.confirm(release, func(accepted bool) {
		once.Do(func() {
			defer s.busy.Store(false)
			if !accepted {
				return
			}
			// Keep Wails' progress, verification and restart UI after consent.
			if err := s.client.CheckAndInstall(context.Background()); err != nil {
				s.notify("error", fmt.Sprintf("下载更新失败：%v", err))
			}
		})
	})
	return nil
}

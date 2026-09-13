package services

import (
	"errors"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	// ScriptWindowName identifies the detached script workspace window. The name
	// must be set explicitly: without it Wails names the window "window-N" and
	// lookups by name can never find it.
	ScriptWindowName = "script-workspace"

	scriptWindowTitle = "脚本工作区 — pvfine"

	// ScriptWindowCloseRequestedEvent asks the script window to run its own close
	// confirmation. It is separate from app:close-requested so the main window's
	// CloseGuard does not react to the script window closing.
	ScriptWindowCloseRequestedEvent = "script-window:close-requested"
	// ScriptWindowClosedEvent tells the main window the detached window is gone
	// so it can pull the handed-back session.
	ScriptWindowClosedEvent = "script-window:closed"
	// ScriptWindowDirtyEvent reports whether the detached window holds unsaved
	// script edits. The main window's close guard needs it because each window
	// keeps its own Pinia store.
	ScriptWindowDirtyEvent = "script-window:dirty"
)

// ScriptSession is the script editor state handed between the main window and
// the detached script window. Every webview has its own Pinia store, so the
// content has to travel explicitly.
type ScriptSession struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	SavedSource string `json:"savedSource"`
}

var errScriptWindowUnavailable = errors.New("当前环境不支持多窗口")

// scriptWindowHandle is the subset of application.Window this service drives.
type scriptWindowHandle interface {
	Focus()
	Close()
	RegisterHook(eventType events.WindowEventType, callback func(event *application.WindowEvent)) func()
}

// scriptWindowHost abstracts window lookup and creation so the service can be
// exercised without a running Wails application.
type scriptWindowHost interface {
	findWindow(name string) (scriptWindowHandle, bool)
	createWindow(options application.WebviewWindowOptions) (scriptWindowHandle, error)
}

type wailsScriptWindowHost struct {
	app *application.App
}

func (h wailsScriptWindowHost) findWindow(name string) (scriptWindowHandle, bool) {
	if h.app == nil {
		return nil, false
	}
	window, ok := h.app.Window.GetByName(name)
	if !ok || window == nil {
		return nil, false
	}
	return window, true
}

func (h wailsScriptWindowHost) createWindow(options application.WebviewWindowOptions) (scriptWindowHandle, error) {
	if h.app == nil {
		return nil, errScriptWindowUnavailable
	}
	window := h.app.Window.NewWithOptions(options)
	if window == nil {
		return nil, errScriptWindowUnavailable
	}
	return window, nil
}

// ScriptWindowService owns the detached script workspace window and stages the
// script session that moves between windows.
type ScriptWindowService struct {
	host scriptWindowHost

	mu         sync.Mutex
	session    *ScriptSession
	allowClose bool
}

// NewScriptWindowService creates the service against the running application.
func NewScriptWindowService(app *application.App) *ScriptWindowService {
	return &ScriptWindowService{host: wailsScriptWindowHost{app: app}}
}

func newScriptWindowService(host scriptWindowHost) *ScriptWindowService {
	return &ScriptWindowService{host: host}
}

// OpenScriptWindow opens the detached script window, or focuses the existing one.
// The session is staged first so a freshly created window can pick it up when it
// starts loading its script.
func (s *ScriptWindowService) OpenScriptWindow(session ScriptSession) error {
	if s == nil || s.host == nil {
		return errScriptWindowUnavailable
	}
	s.storeSession(session)
	if window, ok := s.host.findWindow(ScriptWindowName); ok {
		window.Focus()
		return nil
	}

	// A window that reached this branch is new, so any leftover close approval
	// belongs to a previous window and must not leak into this one.
	s.mu.Lock()
	s.allowClose = false
	s.mu.Unlock()

	window, err := s.host.createWindow(scriptWindowOptions())
	if err != nil {
		return err
	}
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if s.consumeAllowClose() {
			emitEvent(ScriptWindowClosedEvent)
			return
		}
		event.Cancel()
		emitEvent(ScriptWindowCloseRequestedEvent)
	})
	return nil
}

// FocusScriptWindow brings the detached window forward, reporting whether it was
// there so the caller can fall back to the embedded workspace.
func (s *ScriptWindowService) FocusScriptWindow() bool {
	window, ok := s.findWindow()
	if !ok {
		return false
	}
	window.Focus()
	return true
}

// IsScriptWindowOpen reports whether the detached window currently exists.
func (s *ScriptWindowService) IsScriptWindowOpen() bool {
	_, ok := s.findWindow()
	return ok
}

// LoadScriptSession returns the staged session, or nil when none was staged.
func (s *ScriptWindowService) LoadScriptSession() *ScriptSession {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return nil
	}
	staged := *s.session
	return &staged
}

// CloseScriptWindow stages the final session and closes the window. The close
// hook is what actually lets the close through, so the frontend runs its own
// confirmation before calling this.
func (s *ScriptWindowService) CloseScriptWindow(session ScriptSession) error {
	if s == nil {
		return nil
	}
	s.storeSession(session)
	window, ok := s.findWindow()
	if !ok {
		return nil
	}
	s.AllowScriptWindowClose()
	window.Close()
	return nil
}

// CloseScriptWindowIfOpen closes the detached window without a frontend
// confirmation. It is for paths where the main surface is already going away and
// there is no window left to run a dialog.
func (s *ScriptWindowService) CloseScriptWindowIfOpen() {
	if s == nil {
		return
	}
	window, ok := s.findWindow()
	if !ok {
		return
	}
	s.AllowScriptWindowClose()
	window.Close()
}

// AllowScriptWindowClose lets the window close without asking the frontend. The
// application quit path calls this so shutdown cannot stall on the script
// window's close confirmation.
func (s *ScriptWindowService) AllowScriptWindowClose() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.allowClose = true
	s.mu.Unlock()
}

func (s *ScriptWindowService) findWindow() (scriptWindowHandle, bool) {
	if s == nil || s.host == nil {
		return nil, false
	}
	return s.host.findWindow(ScriptWindowName)
}

func (s *ScriptWindowService) storeSession(session ScriptSession) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	staged := session
	s.session = &staged
}

func (s *ScriptWindowService) consumeAllowClose() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	allowed := s.allowClose
	s.allowClose = false
	return allowed
}

// scriptWindowOptions mirrors the main window's chrome so the detached workspace
// looks like the window it came from.
func scriptWindowOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Name:   ScriptWindowName,
		Title:  scriptWindowTitle,
		Width:  1200,
		Height: 820,
		// The script window edits scripts, not archives; dropping a .pvf here
		// would have nowhere to go.
		EnableFileDrop:     false,
		UseApplicationMenu: true,
		MinWidth:           860,
		MinHeight:          560,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropLiquidGlass,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(24, 26, 32),
		// The asset server has no SPA fallback, so the view is selected by query
		// rather than by path.
		URL: "/?view=script",
	}
}

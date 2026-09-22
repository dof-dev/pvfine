package main

import (
	"context"
	"embed"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/updater"
	githubupdater "github.com/wailsapp/wails/v3/pkg/updater/providers/github"

	"pvfine/services"
)

// Release builds replace these values with -ldflags. Local builds use a
// synthetic baseline version so manual update checks remain available.
var (
	appVersion       = "0.0.0"
	githubRepository = "dof-dev/pvfine"
)

const (
	quitRequestedEvent  = "app:quit-requested"
	closeRequestedEvent = "app:close-requested"
	quitConfirmedEvent  = "app:quit-confirmed"
	closeConfirmedEvent = "app:close-confirmed"

	// mainWindowName identifies the archive editor window. It must be set
	// explicitly, otherwise Wails names it "window-N" and it cannot be
	// distinguished from the detached script window by name.
	mainWindowName = "main"
)

type closeCoordinator struct {
	app *application.App
	// allowWindowClosing permits the next close of the window that is actually
	// being closed. A single flag is not enough once a second window exists:
	// app.Window.Current() reports the last interacted window, so approving a
	// close would target whichever window the user touched most recently.
	allowWindowClosing atomic.Bool
	// pendingClose remembers which window asked to close so the confirmation
	// closes that window instead of guessing from window focus.
	pendingClose atomic.Uint64
	// scriptWindow lets the quit path release the detached script window without
	// waiting for its own close confirmation.
	scriptWindow *services.ScriptWindowService
}

func newCloseCoordinator(app *application.App, scriptWindow *services.ScriptWindowService) *closeCoordinator {
	coordinator := &closeCoordinator{app: app, scriptWindow: scriptWindow}
	app.Event.On(closeConfirmedEvent, func(*application.CustomEvent) {
		coordinator.allowWindowClosing.Store(true)
		// Close the window that started this handshake; falling back to the
		// focused window would close the wrong one when two windows are open.
		id := uint(coordinator.pendingClose.Swap(0))
		addressable := false
		if id != 0 {
			if window, ok := app.Window.GetByID(id); ok && window != nil {
				// 主窗口关闭时，独立脚本窗口不能留下：它已经没有回到归档编辑的
				// 入口，留下来就是一个无法操作的窗口。
				if coordinator.scriptWindow != nil && window.Name() == mainWindowName {
					coordinator.scriptWindow.AllowScriptWindowClose()
					coordinator.scriptWindow.CloseScriptWindowIfOpen()
				}
				window.Close()
				addressable = true
			}
		}
		if !addressable {
			if window := app.Window.Current(); window != nil {
				window.Close()
			}
		}
	})
	app.Event.On(quitConfirmedEvent, func(*application.CustomEvent) {
		// The detached script window keeps its own close confirmation and its own
		// Pinia store; release it first so quitting cannot stall on it.
		if coordinator.scriptWindow != nil {
			coordinator.scriptWindow.AllowScriptWindowClose()
			coordinator.scriptWindow.CloseScriptWindowIfOpen()
		}
		app.Quit()
	})
	return coordinator
}

func (c *closeCoordinator) requestQuit() {
	_ = c.app.Event.Emit(quitRequestedEvent)
}

// handlerFor returns the close handler for one specific window. Binding the
// window keeps the confirmation handshake pointing at the window the user
// actually tried to close.
func (c *closeCoordinator) handlerFor(window application.Window) func(*application.WindowEvent) {
	return func(event *application.WindowEvent) {
		if c.allowWindowClosing.CompareAndSwap(true, false) {
			return
		}
		if window != nil {
			c.pendingClose.Store(uint64(window.ID()))
		}
		event.Cancel()
		_ = c.app.Event.Emit(closeRequestedEvent)
	}
}

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	core := services.NewCore()
	settingsService := services.NewSettingsService()
	// The backup service attaches itself to the core so saving or closing the
	// workspace can drop a cache that no longer protects anything.
	autosaveService := services.NewAutosaveService(core, settingsService)
	cacheService := services.NewCacheService(core, settingsService)
	// One file set service backs both the sidebar and the script API, so a
	// scripted change and a manual save target the same document.
	fileSetService := services.NewFileSetService()

	app := application.New(application.Options{
		Name:             "pvfine",
		Description:      "PVF 归档编辑器",
		FileAssociations: []string{".pvf"},
		Services: []application.Service{
			application.NewService(services.NewArchiveService(core)),
			application.NewService(services.NewEditorService(core, settingsService)),
			application.NewService(services.NewDropService(core)),
			application.NewService(services.NewBatchService(core)),
			application.NewService(services.NewScriptService(core, fileSetService)),
			application.NewService(services.NewVersionService(core)),
			application.NewService(services.NewAnnotationService(core)),
			application.NewService(services.NewRenderingService(core)),
			application.NewService(services.NewPreviewService(core)),
			application.NewService(services.NewImageService(core, settingsService)),
			application.NewService(services.NewFileGUIService(core)),
			application.NewService(autosaveService),
			application.NewService(cacheService),
			application.NewService(settingsService),
			application.NewService(fileSetService),
			application.NewService(services.NewBookmarkService()),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	app.RegisterService(application.NewService(services.NewUpdateService(app)))
	scriptWindowService := services.NewScriptWindowService(app)
	app.RegisterService(application.NewService(scriptWindowService))
	closeCoordinator := newCloseCoordinator(app, scriptWindowService)

	updaterEnabled := configureUpdater(app)

	menu := app.Menu.New()
	app.Menu.SetApplicationMenu(menu)
	appMenu := menu.AddSubmenu("应用")
	if updaterEnabled {
		appMenu.Add("检查更新").OnClick(func(*application.Context) {
			go func() {
				if err := app.Updater.CheckAndInstall(context.Background()); err != nil {
					log.Printf("检查更新失败: %v", err)
				}
			}()
		})
	}
	appMenu.AddSeparator()
	appMenu.Add("退出").SetAccelerator("CmdOrCtrl+q").OnClick(func(*application.Context) {
		closeCoordinator.requestQuit()
	})
	menu.AddRole(application.EditMenu)

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:          "pvfine — PVF 归档编辑器",
		Name:           mainWindowName,
		Width:          1440,
		Height:         900,
		EnableFileDrop: true,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropLiquidGlass,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(24, 26, 32),
		URL:              "/",
	})
	var openPathMu sync.Mutex
	pendingOpenPath := ""
	openPathReady := false
	emitPVFOpenPath := func(path string) {
		if !strings.HasSuffix(strings.ToLower(path), ".pvf") {
			return
		}
		openPathMu.Lock()
		if !openPathReady {
			pendingOpenPath = path
			openPathMu.Unlock()
			return
		}
		openPathMu.Unlock()
		app.Event.Emit("archive:open-path", path)
	}
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		openPathMu.Lock()
		openPathReady = true
		path := pendingOpenPath
		pendingOpenPath = ""
		openPathMu.Unlock()
		if path != "" {
			app.Event.Emit("archive:open-path", path)
		}
	})
	window.RegisterHook(events.Common.WindowClosing, closeCoordinator.handlerFor(window))
	window.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		ctx := event.Context()
		if ctx == nil {
			return
		}
		for _, file := range ctx.DroppedFiles() {
			if strings.HasSuffix(strings.ToLower(file), ".pvf") {
				emitPVFOpenPath(file)
				break
			}
		}
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationOpenedWithFile, func(event *application.ApplicationEvent) {
		if event == nil || event.Context() == nil {
			return
		}
		path := event.Context().Filename()
		emitPVFOpenPath(path)
	})

	if updaterEnabled {
		startBackgroundUpdateCheck(app)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func configureUpdater(app *application.App) bool {
	version := strings.TrimPrefix(appVersion, "v")
	if githubRepository == "" || version == "" {
		return false
	}

	provider, err := githubupdater.New(githubupdater.Config{
		Repository:    githubRepository,
		ChecksumAsset: "SHA256SUMS",
	})
	if err != nil {
		log.Printf("初始化更新源失败: %v", err)
		return false
	}

	if err := app.Updater.Init(updater.Config{
		CurrentVersion: version,
		Providers:      []updater.Provider{provider},
	}); err != nil {
		log.Printf("初始化更新器失败: %v", err)
		return false
	}

	return true
}

func startBackgroundUpdateCheck(app *application.App) {
	if githubRepository == "" || appVersion == "" || appVersion == "dev" || appVersion == "0.0.0" {
		return
	}

	go func() {
		// Give the first window time to enter the event loop before opening the
		// updater window when a newer release is available.
		time.Sleep(3 * time.Second)

		release, err := app.Updater.Check(context.Background())
		if err != nil || release == nil {
			return
		}

		if err := app.Updater.CheckAndInstall(context.Background()); err != nil {
			log.Printf("自动更新失败: %v", err)
		}
	}()
}

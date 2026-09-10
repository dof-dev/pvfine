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
)

type closeCoordinator struct {
	app                *application.App
	allowWindowClosing atomic.Bool
}

func newCloseCoordinator(app *application.App) *closeCoordinator {
	coordinator := &closeCoordinator{app: app}
	app.Event.On(closeConfirmedEvent, func(*application.CustomEvent) {
		coordinator.allowWindowClosing.Store(true)
		if window := app.Window.Current(); window != nil {
			window.Close()
		}
	})
	app.Event.On(quitConfirmedEvent, func(*application.CustomEvent) {
		app.Quit()
	})
	return coordinator
}

func (c *closeCoordinator) requestQuit() {
	_ = c.app.Event.Emit(quitRequestedEvent)
}

func (c *closeCoordinator) handleWindowClosing(event *application.WindowEvent) {
	if c.allowWindowClosing.CompareAndSwap(true, false) {
		return
	}
	event.Cancel()
	_ = c.app.Event.Emit(closeRequestedEvent)
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

	app := application.New(application.Options{
		Name:             "pvfine",
		Description:      "PVF 归档编辑器",
		FileAssociations: []string{".pvf"},
		Services: []application.Service{
			application.NewService(services.NewArchiveService(core)),
			application.NewService(services.NewEditorService(core, settingsService)),
			application.NewService(services.NewBatchService(core)),
			application.NewService(services.NewScriptService(core)),
			application.NewService(services.NewVersionService(core)),
			application.NewService(services.NewAnnotationService(core)),
			application.NewService(services.NewRenderingService(core)),
			application.NewService(services.NewImageService(core, settingsService)),
			application.NewService(settingsService),
			application.NewService(services.NewFileSetService()),
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
	closeCoordinator := newCloseCoordinator(app)

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
	window.RegisterHook(events.Common.WindowClosing, closeCoordinator.handleWindowClosing)
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

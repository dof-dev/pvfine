package main

import (
	"context"
	"embed"
	"log"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
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

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	core := services.NewCore()

	app := application.New(application.Options{
		Name:        "pvfine",
		Description: "PVF 归档编辑器",
		Services: []application.Service{
			application.NewService(services.NewArchiveService(core)),
			application.NewService(services.NewEditorService(core)),
			application.NewService(services.NewAnnotationService(core)),
			application.NewService(services.NewSettingsService()),
			application.NewService(services.NewFileSetService()),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	app.RegisterService(application.NewService(services.NewUpdateService(app)))

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
	appMenu.Add("退出").OnClick(func(*application.Context) {
		app.Quit()
	})
	menu.AddRole(application.EditMenu)

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "pvfine — PVF 归档编辑器",
		Width:  1440,
		Height: 900,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(24, 26, 32),
		URL:              "/",
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

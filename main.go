package main

import (
	"context"
	"embed"
	"os"

	"github.com/devour-app/devour/app"
	"github.com/devour-app/devour/app/cli"
	"github.com/devour-app/devour/app/tray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// CLI / daemon dispatch. If the user typed `hangar start mysql` or
	// `hangar daemon`, route to the cobra-powered CLI and exit before
	// Wails ever sees the args. No-args (typical double-click or plain
	// `hangar`) falls through to the GUI path below.
	if cli.IsCLIInvocation(os.Args[1:]) {
		os.Exit(cli.Run(os.Args[1:]))
	}

	devourApp := app.NewApp()

	// ctxCh hands the Wails context to the tray goroutine once Startup runs.
	// We can't start the tray before Wails because tray menu items need to
	// call into Wails (WindowShow / WindowHide), which requires the context.
	ctxCh := make(chan context.Context, 1)

	go func() {
		ctx := <-ctxCh
		tray.Start(tray.Callbacks{
			OnShow: func() {
				wailsruntime.WindowShow(ctx)
				wailsruntime.WindowUnminimise(ctx)
			},
			OnQuit: func() {
				// Stop services first so they shut down cleanly. Then ask
				// Wails to quit the app - this calls OnShutdown which also
				// stops services as a safety net (idempotent).
				devourApp.AllowQuit()
				wailsruntime.Quit(ctx)
			},
		})
	}()

	err := wails.Run(&options.App{
		Title:     "Hangar",
		Width:     1280,
		Height:    800,
		MinWidth:  1024,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 16, B: 16, A: 1},
		// Single-instance lock. Hangar holds an exclusive bbolt flock on the
		// config DB plus a TCP listen on :3742 (MCP); a second instance can't
		// acquire either and used to half-start with "service manager not
		// initialized" everywhere. With this lock, the second launch is
		// short-circuited - Wails IPCs the args to the running instance,
		// which un-hides its window (handled below), then exits.
		//
		// UniqueId must be stable across versions but unique to Hangar.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.hangar.app.singleton",
			OnSecondInstanceLaunch: func(secondInstanceData options.SecondInstanceData) {
				if devourApp.Ctx() != nil {
					wailsruntime.WindowShow(devourApp.Ctx())
					wailsruntime.WindowUnminimise(devourApp.Ctx())
				}
			},
		},
		OnStartup: func(ctx context.Context) {
			devourApp.Startup(ctx)
			ctxCh <- ctx
		},
		// OnBeforeClose: when the user clicks the title-bar X, decide whether
		// to actually quit or just hide to tray. If any service is running we
		// hide (so a stray click doesn't kill MySQL/Postgres mid-write); if
		// nothing's running, allow the close. The "Quit Devour" tray menu
		// bypasses this by setting AllowQuit() first.
		OnBeforeClose: func(ctx context.Context) bool {
			if devourApp.AllowingQuit() {
				return false // don't prevent close
			}
			if devourApp.HasRunningServices() {
				wailsruntime.WindowHide(ctx)
				return true // prevent close
			}
			return false
		},
		OnShutdown: func(ctx context.Context) {
			devourApp.Shutdown(ctx)
			tray.Stop()
		},
		Bind: []interface{}{
			devourApp,
		},
		Windows: &windows.Options{
			WebviewIsTransparent:              false,
			WindowIsTranslucent:               false,
			DisableWindowIcon:                 false,
			DisableFramelessWindowDecorations: false,
			WebviewUserDataPath:               "",
			Theme:                             windows.Dark,
		},
		Frameless: true,
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

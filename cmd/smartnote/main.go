// Command smartnote starts the Smartnote desktop application.
package main

import (
	"io/fs"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/nikchis/smartnote/frontend"
)

func main() {
	assets, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		log.Fatalf("frontend assets: %v", err)
	}

	app := application.New(application.Options{
		Name:        "Smartnote",
		Description: "Encrypted notes with remote backup",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Smartnote",
		Width:  1000,
		Height: 640,
		URL:    "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

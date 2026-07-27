package main

import (
	"embed"
	"log"

	appservice "github.com/bioidaika/vmf-preupload/app"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	application := appservice.NewApp()
	err := wails.Run(&options.App{
		Title:       "VMF Preupload",
		Width:       1440,
		Height:      920,
		MinWidth:    1080,
		MinHeight:   700,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   application.Startup,
		Bind:        []interface{}{application},
	})
	if err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"embed"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"log"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	service := NewGitService()
	updates := NewUpdateService(service)
	app := application.New(application.Options{
		Name: "Crab.GitSync", Description: "本地 Git 仓库扫描与远端更新",
		Services: []application.Service{application.NewService(service), application.NewService(updates)},
		Assets:   application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Mac:      application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	service.app = app
	updates.attach(app)
	app.OnShutdown(service.Cancel)
	app.OnShutdown(updates.shutdown)
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) { go updates.automaticChecks() })
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Crab.GitSync", Width: 1280, Height: 850, MinWidth: 900, MinHeight: 650,
		BackgroundColour: application.NewRGB(246, 248, 251), URL: "/",
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

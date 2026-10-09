package main

import (
	"crab.gitsync/internal/networksettings"
	"crab.gitsync/internal/updatefeed"
	"embed"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"log"
	"net/url"
	"os"
	"path/filepath"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	store, loadErr := networksettings.New(filepath.Join(configDir, "Crab.GitSync", "network.json"))
	network := &NetworkService{store: store, loadError: loadErr}
	proxy := func() *url.URL { value, _ := store.Get().URL(); return value }
	service := NewGitService()
	service.proxyURL = func() string {
		if value := proxy(); value != nil {
			return value.String()
		}
		return ""
	}
	updates := NewUpdateService(service)
	updates.provider = updatefeed.NewPublicGitHub(ReleaseRepository, "", updatefeed.NewHTTPClientWithProxy(proxy))
	desktop := &DesktopService{notifier: notifications.New()}
	service.notify = desktop.notify
	updates.notify = desktop.notify
	app := application.New(application.Options{
		Name: "Crab.GitSync", Description: "本地 Git 仓库扫描与远端更新",
		Icon: appIcon,
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "com.crabgo.gitsync", OnSecondInstanceLaunch: func(application.SecondInstanceData) {
			if desktop.window != nil {
				desktop.window.Show()
				desktop.window.UnMinimise()
				desktop.window.Focus()
			}
		}},
		Services: []application.Service{application.NewService(service), application.NewService(updates), application.NewService(desktop), application.NewService(network)},
		Assets:   application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Mac:      application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	service.app = app
	updates.attach(app)
	app.OnShutdown(service.Cancel)
	app.OnShutdown(updates.shutdown)
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) { go updates.automaticChecks() })
	desktop.attach(app, service, updates)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

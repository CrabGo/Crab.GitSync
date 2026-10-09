package main

import (
	"context"
	"fmt"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"log"
	"time"
)

var pageTitles = map[string]string{"workspace": "仓库工作台", "logs": "任务日志", "updates": "应用更新", "help": "使用说明", "settings": "网络设置"}

type DesktopService struct {
	window   *application.WebviewWindow
	notifier *notifications.NotificationService
}

//wails:ignore
func (s *DesktopService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	return s.notifier.ServiceStartup(ctx, options)
}

//wails:ignore
func (s *DesktopService) ServiceShutdown() error {
	return s.notifier.ServiceShutdown()
}

func (s *DesktopService) SetPage(page string) error {
	title, ok := pageTitles[page]
	if !ok {
		return fmt.Errorf("未知页面")
	}
	if s.window != nil {
		s.window.SetTitle(title + " · Crab.GitSync")
	}
	return nil
}

func (s *DesktopService) openPage(page string) {
	_ = s.SetPage(page)
	s.window.Show()
	s.window.UnMinimise()
	s.window.Focus()
	s.window.ExecJS(fmt.Sprintf("window.location.hash = %q", page))
}

func (s *DesktopService) notify(title, body, page string) {
	if s.notifier == nil {
		return
	}
	if err := s.notifier.SendNotification(notifications.NotificationOptions{
		ID: fmt.Sprintf("gitsync-%d", time.Now().UnixNano()), Title: title, Body: body,
		Data: map[string]interface{}{"page": page}, ThreadID: "crab-gitsync",
	}); err != nil {
		log.Printf("通知发送失败: %v", err)
	}
}

func (s *DesktopService) attach(app *application.App, git *GitService, updates *UpdateService) {
	s.window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "仓库工作台 · Crab.GitSync", Width: 1280, Height: 850, MinWidth: 900, MinHeight: 650,
		BackgroundColour: application.NewRGB(246, 248, 251), URL: "/",
	})
	tray := app.SystemTray.New()
	tray.SetIcon(appIcon)
	tray.SetTooltip("Crab.GitSync · 仓库同步工具")
	menu := app.NewMenu()
	menu.Add("打开 Crab.GitSync").OnClick(func(*application.Context) { s.openPage("workspace") })
	menu.AddSeparator()
	for _, page := range []string{"workspace", "logs", "updates", "settings", "help"} {
		menu.Add(pageTitles[page]).OnClick(func(*application.Context) { s.openPage(page) })
	}
	menu.AddSeparator()
	menu.Add("检查更新").OnClick(func(*application.Context) {
		s.openPage("updates")
		if err := updates.StartCheck(); err != nil {
			s.notify("无法检查更新", err.Error(), "updates")
		}
	})
	menu.Add("取消当前任务").OnClick(func(*application.Context) { git.Cancel(); updates.Cancel() })
	menu.Add("退出").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
	tray.OnClick(func() { s.window.Show(); s.window.UnMinimise(); s.window.Focus() })
	tray.OnRightClick(tray.ShowMenu)
	s.window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) { event.Cancel(); s.window.Hide() })
	s.notifier.OnNotificationResponse(func(result notifications.NotificationResult) {
		if result.Error != nil {
			return
		}
		page := "logs"
		if value, ok := result.Response.UserInfo["page"].(string); ok {
			if _, valid := pageTitles[value]; valid {
				page = value
			}
		}
		s.openPage(page)
	})
}

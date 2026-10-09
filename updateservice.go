package main

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"crab.gitsync/internal/updatefeed"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

type UpdateState struct {
	Version       string `json:"version"`
	Repository    string `json:"repository"`
	Platform      string `json:"platform"`
	Phase         string `json:"phase"`
	Busy          bool   `json:"busy"`
	LatestVersion string `json:"latestVersion"`
	Notes         string `json:"notes"`
	Written       int64  `json:"written"`
	Total         int64  `json:"total"`
	AuthSource    string `json:"authSource"`
	CheckedAt     string `json:"checkedAt"`
	Error         string `json:"error"`
}

// UpdateService presents the Wails updater through the application's own UI.
type UpdateService struct {
	app         *application.App
	git         *GitService
	mu          sync.Mutex
	state       UpdateState
	release     *updater.Release
	ctx         context.Context
	stop        context.CancelFunc
	cancel      context.CancelFunc
	notify      func(string, string, string)
	provider    updater.Provider
	initialized bool
}

func NewUpdateService(git *GitService) *UpdateService {
	ctx, stop := context.WithCancel(context.Background())
	return &UpdateService{git: git, ctx: ctx, stop: stop, provider: updatefeed.NewPublicGitHub(ReleaseRepository, "", nil), state: UpdateState{Version: Version, Repository: ReleaseRepository, Platform: runtime.GOOS + "/" + runtime.GOARCH, Phase: "idle"}}
}

// initialiseUpdater configures the application updater once, independently of check outcomes.
// A network error or cancellation must not cause the next check to call Init again.
func (s *UpdateService) initialiseUpdater() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		return nil
	}
	if err := s.app.Updater.Init(updater.Config{CurrentVersion: Version, Providers: []updater.Provider{s.provider}, Window: updater.WindowNone}); err != nil {
		return err
	}
	s.initialized = true
	return nil
}

func (s *UpdateService) attach(app *application.App) {
	s.app = app
	app.Event.On(updater.EventDownloadProgress, func(event *application.CustomEvent) {
		if progress, ok := event.Data.(updater.Progress); ok {
			s.mu.Lock()
			s.state.Written, s.state.Total = progress.Written, progress.Total
			s.mu.Unlock()
		}
	})
	for event, phase := range map[string]string{updater.EventVerifying: "verifying", updater.EventInstalling: "installing"} {
		app.Event.On(event, func(*application.CustomEvent) { s.mu.Lock(); s.state.Phase = phase; s.mu.Unlock() })
	}
}

// GetState contains status only; it never includes the token used for requests.
func (s *UpdateService) GetState() UpdateState { s.mu.Lock(); defer s.mu.Unlock(); return s.state }

func (s *UpdateService) log(level, message string) {
	s.git.mu.Lock()
	defer s.git.mu.Unlock()
	s.git.logLocked(level, "应用更新："+message)
}

// StartCheck checks stable public GitHub releases without a login or API token.
func (s *UpdateService) StartCheck() error {
	s.mu.Lock()
	if s.state.Busy || s.state.Phase == "ready" {
		s.mu.Unlock()
		return fmt.Errorf("更新任务正在运行或已准备好，请先完成当前更新")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 45*time.Second)
	s.cancel = cancel
	s.state.Busy, s.state.Phase, s.state.Error = true, "checking", ""
	s.mu.Unlock()
	go func() {
		defer cancel()
		source := "公开发布源 · 无需登录"
		err := s.initialiseUpdater()
		var release *updater.Release
		if err == nil {
			release, err = s.app.Updater.Check(ctx)
		}
		s.mu.Lock()
		s.state.AuthSource = source
		s.state.CheckedAt = time.Now().Format(time.RFC3339)
		s.release = release
		s.state.LatestVersion, s.state.Notes = "", ""
		s.state.Total = 0
		if release != nil {
			s.state.LatestVersion, s.state.Notes = release.Version, release.Notes
			s.state.Total = release.Artifact.Size
		}
		s.state.Written = 0
		s.mu.Unlock()
		phase := "up-to-date"
		if release != nil {
			phase = "available"
		}
		s.finish(ctx, phase, err)
	}()
	return nil
}

// StartDownload downloads and verifies the pending executable; it does not restart.
func (s *UpdateService) StartDownload() error {
	s.mu.Lock()
	if s.state.Busy || s.release == nil || s.state.Phase == "ready" {
		s.mu.Unlock()
		return fmt.Errorf("请先检查更新，并等待当前任务完成")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
	s.cancel = cancel
	s.state.Busy, s.state.Phase, s.state.Error = true, "downloading", ""
	s.state.Written = 0
	s.mu.Unlock()
	go func() { defer cancel(); err := s.app.Updater.DownloadAndInstall(ctx); s.finish(ctx, "ready", err) }()
	return nil
}

func (s *UpdateService) finish(ctx context.Context, phase string, err error) {
	s.mu.Lock()
	s.state.Busy = false
	if err != nil {
		s.state.Phase = "error"
		s.state.Error = "更新失败，请检查网络、代理以及发布文件后重试"
		if phase == "up-to-date" || phase == "available" {
			s.state.Error = err.Error()
		}
		if ctx.Err() == context.Canceled {
			s.state.Phase = "cancelled"
			s.state.Error = "更新任务已取消"
		}
		if ctx.Err() == context.DeadlineExceeded {
			s.state.Error = "更新任务超时，请检查网络或代理后重试"
		}
	} else {
		s.state.Phase = phase
	}
	message := s.state.Error
	latest := s.state.LatestVersion
	s.mu.Unlock()
	if err != nil {
		s.log("error", message)
		return
	}
	switch phase {
	case "available":
		s.log("info", "发现新版本 v"+latest)
		if s.notify != nil {
			s.notify("发现新版本", "Crab.GitSync v"+latest+" 可下载更新", "updates")
		}
	case "ready":
		s.log("success", "下载与 SHA-256 校验完成，可重启应用更新")
		if s.notify != nil {
			s.notify("更新已准备就绪", "下载和校验完成，请在应用更新页面重启安装", "updates")
		}
	case "up-to-date":
		s.log("info", "没有可用的新版本")
	}
}

// Restart prevents new Git tasks while Wails starts the replacement helper.
func (s *UpdateService) Restart() error {
	s.git.mu.Lock()
	s.mu.Lock()
	if s.git.state.Busy || s.state.Busy || s.state.Phase != "ready" {
		s.mu.Unlock()
		s.git.mu.Unlock()
		return fmt.Errorf("请等待 Git 任务完成，并确保更新已经下载及校验")
	}
	s.git.restarting = true
	s.state.Busy, s.state.Phase = true, "restarting"
	s.mu.Unlock()
	s.git.mu.Unlock()
	if err := s.app.Updater.Restart(s.ctx); err != nil {
		s.git.mu.Lock()
		s.git.restarting = false
		s.git.mu.Unlock()
		s.mu.Lock()
		s.state.Busy = false
		s.state.Phase = "ready"
		s.state.Error = "无法重启更新，请确认程序所在目录可写；可从 Releases 手动下载"
		s.mu.Unlock()
		return fmt.Errorf("无法启动更新替换程序，当前版本仍可继续使用")
	}
	return nil
}

// Cancel leaves the current executable unchanged and stops checking/downloading.
func (s *UpdateService) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Busy && s.state.Phase != "restarting" && s.cancel != nil {
		s.cancel()
	}
}

func (s *UpdateService) automaticChecks() {
	_ = s.StartCheck()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			_ = s.StartCheck()
		}
	}
}

func (s *UpdateService) shutdown() { s.stop(); s.Cancel() }

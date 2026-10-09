package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/wailsapp/wails/v3/pkg/application"
	"strings"
	"testing"
	"time"
)

func TestTaskCompletionNotifications(t *testing.T) {
	for _, phase := range []string{"done", "error", "cancelled"} {
		t.Run(phase, func(t *testing.T) {
			s := NewGitService()
			ctx, cancel := context.WithCancel(context.Background())
			s.cancel = cancel
			s.state.Kind = "fetch"
			s.state.Busy = true
			s.state.Succeeded = 2
			s.state.Failed = 1
			result := make(chan []string, 1)
			s.notify = func(title, body, page string) { result <- []string{title, body, page} }
			var err error
			if phase == "error" {
				err = errors.New("test failure")
			}
			if phase == "cancelled" {
				cancel()
			}
			s.finish(ctx, err)
			select {
			case got := <-result:
				if got[0] != "远端更新任务" || got[2] != "logs" || !strings.Contains(got[1], phasesForNotification(phase)) || !strings.Contains(got[1], "成功 2") {
					t.Fatalf("unexpected notification: %v", got)
				}
			case <-time.After(time.Second):
				t.Fatal("missing notification")
			}
		})
	}
}

type routeWindow struct{ calls []string }

func (w *routeWindow) SetTitle(title string) application.Window {
	w.calls = append(w.calls, "title:"+title)
	return nil
}
func (w *routeWindow) Show() application.Window { w.calls = append(w.calls, "show"); return nil }
func (w *routeWindow) UnMinimise()              { w.calls = append(w.calls, "restore") }
func (w *routeWindow) Focus()                   { w.calls = append(w.calls, "focus") }
func (w *routeWindow) ExecJS(js string)         { w.calls = append(w.calls, js) }

func TestDesktopPageRoutesRestoreWindowAndSetTitle(t *testing.T) {
	for _, page := range []string{"workspace", "logs", "updates", "settings", "help"} {
		w := &routeWindow{}
		s := &DesktopService{window: w}
		s.openPage(page)
		want := []string{"title:" + pageTitles[page] + " · Crab.GitSync", "show", "restore", "focus", fmt.Sprintf("window.location.hash = %q", page)}
		if fmt.Sprint(w.calls) != fmt.Sprint(want) {
			t.Fatalf("route %s: %v", page, w.calls)
		}
	}
	w := &routeWindow{}
	s := &DesktopService{window: w}
	if err := s.SetPage("unknown"); err == nil {
		t.Fatal("unknown page accepted")
	}
	s.openPage("unknown")
	if len(w.calls) != 0 {
		t.Fatal("unknown route changed window")
	}
	(&DesktopService{}).openPage("logs") // A stopped/not-yet-created window is harmless.
}

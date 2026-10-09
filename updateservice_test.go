package main

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

type checkHost struct{}

func (checkHost) Emit(string, ...any) bool         { return false }
func (checkHost) OnEvent(string, func(any)) func() { return func() {} }
func (checkHost) OpenWindow(updater.WindowOptions) updater.WindowHandle {
	panic("unexpected update window")
}
func (checkHost) Quit() { panic("unexpected restart") }

type repeatCheckProvider struct {
	calls     int
	failFirst bool
}

func (*repeatCheckProvider) Name() string { return "repeat-check" }
func (p *repeatCheckProvider) Check(ctx context.Context, _ updater.CheckRequest) (*updater.Release, error) {
	p.calls++
	if p.failFirst && p.calls == 1 {
		return nil, fmt.Errorf("temporary network error")
	}
	return nil, nil
}
func (*repeatCheckProvider) Download(context.Context, *updater.Release, io.Writer, func(int64, int64)) error {
	panic("unexpected download")
}

func TestRepeatedUpdateChecks(t *testing.T) {
	for _, failFirst := range []bool{false, true} {
		t.Run(fmt.Sprint("firstCheckFails=", failFirst), func(t *testing.T) {
			s := NewUpdateService(NewGitService())
			defer s.shutdown()
			p := &repeatCheckProvider{failFirst: failFirst}
			s.provider = p
			s.app = &application.App{Updater: updater.New(checkHost{})}
			for i := 0; i < 3; i++ {
				if err := s.StartCheck(); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(3 * time.Second)
				for s.GetState().Busy && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				state := s.GetState()
				want := "up-to-date"
				if i == 0 && failFirst {
					want = "error"
				}
				if state.Busy || state.Phase != want {
					t.Fatalf("check %d: %+v", i, state)
				}
				if want == "up-to-date" && state.Error != "" {
					t.Fatal(state.Error)
				}
			}
			if p.calls != 3 {
				t.Fatalf("expected 3 provider checks, got %d", p.calls)
			}
		})
	}
}

func TestUpdateTaskGuards(t *testing.T) {
	git := NewGitService()
	service := NewUpdateService(git)
	defer service.shutdown()
	service.state.Busy = true
	if err := service.StartCheck(); err == nil {
		t.Fatal("parallel update check allowed")
	}
	if err := service.StartDownload(); err == nil {
		t.Fatal("parallel download allowed")
	}
	service.state.Busy = false
	service.state.Phase = "available"
	if err := service.StartDownload(); err == nil {
		t.Fatal("download without a checked release allowed")
	}
	service.state.Phase = "ready"
	service.release = &updater.Release{Version: "0.3.0"}
	if err := service.StartCheck(); err == nil {
		t.Fatal("ready update replaced by check")
	}
	git.state.Busy = true
	if err := service.Restart(); err == nil {
		t.Fatal("restart during Git task allowed")
	}
	git.state.Busy = false
	git.restarting = true
	if _, err := git.begin("scan", t.TempDir(), "discovering"); err == nil {
		t.Fatal("new Git task allowed while restarting")
	}
	state := service.GetState()
	state.Version = "mutated"
	if service.GetState().Version == "mutated" {
		t.Fatal("mutable update snapshot")
	}
}

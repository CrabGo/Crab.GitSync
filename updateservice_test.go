package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

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

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"crab.gitsync/internal/gitengine"
)

func testGit(t *testing.T, path string, args ...string) {
	t.Helper()
	if _, err := gitengine.Run(context.Background(), path, 15*time.Second, args...); err != nil {
		t.Fatal(err)
	}
}

func awaitTask(t *testing.T, s *GitService) State {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		state := s.GetState()
		if !state.Busy {
			return state
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Cancel()
	t.Fatal("task timed out")
	return State{}
}

func TestScanFetchFailuresAndSnapshotIsolation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"local", "broken-remote"} {
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		testGit(t, path, "init", "-b", "main")
	}
	testGit(t, filepath.Join(root, "broken-remote"), "remote", "add", "origin", filepath.Join(root, "missing.git"))
	s := NewGitService()
	if err := s.StartScan(root); err != nil {
		t.Fatal(err)
	}
	state := awaitTask(t, s)
	if len(state.Repositories) != 2 || state.Completed != 2 || state.Total != 2 || state.Phase != "done" {
		t.Fatalf("scan: %+v", state)
	}
	state.Repositories[0].Name = "mutated"
	state.Logs[0].Message = "mutated"
	if s.GetState().Repositories[0].Name == "mutated" || s.GetState().Logs[0].Message == "mutated" {
		t.Fatal("mutable snapshot")
	}
	if err := s.StartScan(filepath.Join(root, "missing")); err == nil {
		t.Fatal("invalid path accepted")
	}
	if len(s.GetState().Repositories) != 2 {
		t.Fatal("invalid scan erased results")
	}
	if err := s.StartFetch([]string{root}); err == nil {
		t.Fatal("unscanned repo accepted")
	}
	paths := []string{filepath.Join(root, "local"), filepath.Join(root, "broken-remote"), filepath.Join(root, "local")}
	if err := s.StartFetch(paths); err != nil {
		t.Fatal(err)
	}
	state = awaitTask(t, s)
	if state.Completed != 2 || state.Total != 2 || state.Failed != 1 || state.Skipped != 1 {
		t.Fatalf("fetch: %+v", state)
	}
}

func TestTaskExclusionCancelAndLogBound(t *testing.T) {
	s := NewGitService()
	s.mu.Lock()
	ctx, err := s.begin("scan", t.TempDir(), "discovering")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 600; i++ {
		s.logLocked("info", "test")
	}
	s.mu.Unlock()
	if err := s.StartScan(t.TempDir()); err == nil {
		t.Fatal("parallel task allowed")
	}
	s.Cancel()
	if ctx.Err() != context.Canceled {
		t.Fatal("task was not cancelled")
	}
	s.finish(ctx, nil)
	state := s.GetState()
	if state.Busy || state.Phase != "cancelled" || len(state.Logs) != 500 {
		t.Fatalf("cancel: %+v", state)
	}
}

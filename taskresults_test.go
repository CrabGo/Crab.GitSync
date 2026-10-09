package main

import (
	"context"
	"crab.gitsync/internal/gitengine"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func waitResult(t *testing.T, s *GitService) State {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state := s.GetState()
		if !state.Busy {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("task timeout")
	return State{}
}
func TestFetchStageResultsAndSnapshotIsolation(t *testing.T) {
	s := NewGitService()
	root := t.TempDir()
	for _, name := range []string{"success", "network-failure", "refresh-failure", "no-remote"} {
		r := gitengine.Repository{Path: filepath.Join(root, name), Name: name, Branch: "main", Remotes: []gitengine.Remote{{Name: "origin"}}}
		if name == "no-remote" {
			r.Remotes = nil
		}
		s.state.Repositories = append(s.state.Repositories, r)
	}
	s.fetchRepository = func(_ context.Context, path string) error {
		if filepath.Base(path) == "network-failure" {
			return fmt.Errorf("connection refused")
		}
		return nil
	}
	s.inspectRepository = func(_ context.Context, path string) (gitengine.Repository, error) {
		if filepath.Base(path) == "refresh-failure" {
			return gitengine.Repository{}, fmt.Errorf("index unavailable")
		}
		return gitengine.Repository{Path: path, Name: filepath.Base(path), Branch: "main"}, nil
	}
	paths := []string{}
	for _, r := range s.state.Repositories {
		paths = append(paths, r.Path)
	}
	if err := s.StartFetch(paths); err != nil {
		t.Fatal(err)
	}
	state := waitResult(t, s)
	if state.TaskID == "" || len(state.Results) != 4 || state.Succeeded != 1 || state.Failed != 2 || state.Skipped != 1 {
		t.Fatalf("unexpected counters: %+v", state)
	}
	if state.Results[1].Failure.Category != "connection" || !state.Results[1].Failure.Retryable || state.Results[1].NetworkSucceeded {
		t.Fatal("wrong network failure")
	}
	if !state.Results[2].NetworkSucceeded || state.Results[2].Failure.Category != "refresh" || state.Results[2].Failure.Retryable || state.Repositories[2].FetchStatus != "refresh-error" {
		t.Fatal("refresh failure lost fetch success")
	}
	if state.Results[3].Attempts != 0 || state.Results[3].Status != "skipped" {
		t.Fatal("skipped repo attempted")
	}
	state.Results[1].Failure.Category = "changed"
	state.Results[0].Path = "changed"
	fresh := s.GetState()
	if fresh.Results[1].Failure.Category == "changed" || fresh.Results[0].Path == "changed" {
		t.Fatal("mutable result snapshot")
	}
}
func TestCancelledFetchDoesNotCountQueuedRepositoriesAsFailures(t *testing.T) {
	s := NewGitService()
	setTestConcurrency(t, s, 1)
	root := t.TempDir()
	started := make(chan struct{})
	for _, name := range []string{"first", "queued"} {
		s.state.Repositories = append(s.state.Repositories, gitengine.Repository{Path: filepath.Join(root, name), Remotes: []gitengine.Remote{{Name: "origin"}}})
	}
	s.fetchRepository = func(ctx context.Context, _ string) error { close(started); <-ctx.Done(); return ctx.Err() }
	if err := s.StartFetch([]string{s.state.Repositories[0].Path, s.state.Repositories[1].Path}); err != nil {
		t.Fatal(err)
	}
	<-started
	s.Cancel()
	state := waitResult(t, s)
	if state.Failed != 0 || state.Phase != "cancelled" || state.Results[0].Status != "cancelled" || state.Results[1].Status != "cancelled" || state.Results[1].Attempts != 0 {
		t.Fatalf("incorrect cancellation results: %+v", state)
	}
}

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"crab.gitsync/internal/gitengine"
	"crab.gitsync/internal/tasksettings"
)

func setTestConcurrency(t *testing.T, s *GitService, limit int) {
	t.Helper()
	s.taskSettings, _ = tasksettings.New(filepath.Join(t.TempDir(), "tasks.json"))
	if err := s.SaveTaskConfig(tasksettings.Config{Concurrency: limit}); err != nil {
		t.Fatal(err)
	}
}

func TestParallelFetchLimitSnapshotProgressAndIndependentFailures(t *testing.T) {
	for _, limit := range []int{1, 3, 5} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			s := NewGitService()
			setTestConcurrency(t, s, limit)
			root := t.TempDir()
			paths := []string{}
			for i := range 8 {
				path := filepath.Join(root, fmt.Sprint(i))
				paths = append(paths, path)
				s.state.Repositories = append(s.state.Repositories, gitengine.Repository{Path: path, Remotes: []gitengine.Remote{{Name: "origin"}}})
			}
			var active, peak atomic.Int32
			entered := make(chan struct{}, 8)
			release := make(chan struct{})
			s.fetchRepository = func(ctx context.Context, path string) error {
				n := active.Add(1)
				for {
					old := peak.Load()
					if n <= old || peak.CompareAndSwap(old, n) {
						break
					}
				}
				defer active.Add(-1)
				entered <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				if filepath.Base(path) == "0" {
					return fmt.Errorf("authentication failed")
				}
				return nil
			}
			s.inspectRepository = func(_ context.Context, path string) (gitengine.Repository, error) {
				return gitengine.Repository{Path: path}, nil
			}
			if err := s.StartFetch(paths); err != nil {
				t.Fatal(err)
			}
			for range limit {
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("configured parallelism not reached")
				}
			}
			// Saving the next task's limit does not change the running batch.
			if err := s.SaveTaskConfig(tasksettings.Config{Concurrency: 1}); err != nil {
				t.Fatal(err)
			}
			if peak.Load() != int32(limit) {
				t.Fatal("wrong concurrency", peak.Load())
			}
			close(release)
			previous := 0
			for {
				state := s.GetState()
				if state.Completed < previous || state.Completed > state.Total {
					t.Fatal("non-monotonic progress", state)
				}
				previous = state.Completed
				if !state.Busy {
					if state.Completed != 8 || state.Succeeded != 7 || state.Failed != 1 {
						t.Fatal("independent results lost", state)
					}
					break
				}
				time.Sleep(time.Millisecond)
			}
			if peak.Load() > int32(limit) {
				t.Fatal("limit exceeded")
			}
		})
	}
}

func TestParallelCancellationStopsDispatchAndWaitsForAllWorkers(t *testing.T) {
	s := NewGitService()
	setTestConcurrency(t, s, 3)
	paths := []string{}
	root := t.TempDir()
	for i := range 10 {
		path := filepath.Join(root, fmt.Sprint(i))
		paths = append(paths, path)
		s.state.Repositories = append(s.state.Repositories, gitengine.Repository{Path: path, Remotes: []gitengine.Remote{{Name: "origin"}}})
	}
	entered := make(chan struct{}, 10)
	observed := make(chan struct{}, 3)
	release := make(chan struct{})
	var calls atomic.Int32
	s.fetchRepository = func(ctx context.Context, _ string) error {
		calls.Add(1)
		entered <- struct{}{}
		<-ctx.Done()
		observed <- struct{}{}
		<-release
		return ctx.Err()
	}
	if err := s.StartFetch(paths); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("workers did not start")
		}
	}
	s.Cancel()
	for range 3 {
		select {
		case <-observed:
		case <-time.After(3 * time.Second):
			t.Fatal("worker did not cancel")
		}
	}
	if !s.GetState().Busy {
		t.Fatal("busy cleared before worker cleanup")
	}
	close(release)
	state := awaitTask(t, s)
	if calls.Load() != 3 || state.Failed != 0 || len(state.Results) != 10 {
		t.Fatal("dispatch continued after cancellation", calls.Load(), state)
	}
	for _, r := range state.Results {
		if r.Status != "cancelled" {
			t.Fatal("nonterminal cancellation result", r)
		}
	}
	for _, r := range state.Repositories {
		if r.FetchStatus != "cancelled" {
			t.Fatal("queued repository stayed pending", r)
		}
	}
}

func TestRefreshRetainsConcurrencySlot(t *testing.T) {
	s := NewGitService()
	setTestConcurrency(t, s, 2)
	paths := []string{}
	root := t.TempDir()
	for i := range 4 {
		path := filepath.Join(root, fmt.Sprint(i))
		paths = append(paths, path)
		s.state.Repositories = append(s.state.Repositories, gitengine.Repository{Path: path, Remotes: []gitengine.Remote{{Name: "origin"}}})
	}
	var calls atomic.Int32
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	s.fetchRepository = func(context.Context, string) error { calls.Add(1); return nil }
	s.inspectRepository = func(_ context.Context, path string) (gitengine.Repository, error) {
		entered <- struct{}{}
		<-release
		return gitengine.Repository{Path: path}, nil
	}
	if err := s.StartFetch(paths); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("refresh did not begin")
		}
	}
	state := s.GetState()
	refreshing := 0
	for _, r := range state.Results {
		if r.Status == "running" && r.Stage == "refresh" {
			refreshing++
		}
	}
	if calls.Load() != 2 || refreshing != 2 || state.Completed != 0 {
		t.Fatal("refresh released concurrency slot early", calls.Load(), state)
	}
	close(release)
	state = awaitTask(t, s)
	if state.Completed != 4 || state.Succeeded != 4 {
		t.Fatal(state)
	}
}

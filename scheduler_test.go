package main

import (
	"context"
	"testing"
	"time"

	"crab.gitsync/internal/gitengine"
)

func TestCancellationWaitsForInFlightWorkAndBlocksRestart(t *testing.T) {
	s := NewGitService()
	path := t.TempDir()
	s.state.Repositories = []gitengine.Repository{{Path: path, Remotes: []gitengine.Remote{{Name: "origin"}}}}
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.fetchRepository = func(ctx context.Context, _ string) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	}
	if err := s.StartFetch([]string{path}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("fetch not dispatched")
	}
	s.Cancel()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation not delivered")
	}
	state := s.GetState()
	if !state.Busy || state.Phase != "cancelling" {
		t.Fatal("cancellation cleared busy before cleanup", state)
	}
	if err := s.StartScan(t.TempDir()); err == nil {
		t.Fatal("new scan overlapped cleanup")
	}
	if err := s.StartAction(path, "discard", "", true); err == nil {
		t.Fatal("write overlapped cleanup")
	}
	updates := NewUpdateService(s)
	defer updates.shutdown()
	updates.state.Phase = "ready"
	if err := updates.Restart(); err == nil {
		t.Fatal("restart overlapped cleanup")
	}
	close(release)
	state = awaitTask(t, s)
	if state.Phase != "cancelled" || len(state.Results) != 1 || state.Results[0].Status != "cancelled" {
		t.Fatal("cancel result missing", state)
	}
	if !s.scheduler.Pause() {
		t.Fatal("work not drained after finish")
	}
	s.scheduler.Resume()
}

func TestReadLeaseBlocksTaskAndUpdateRestart(t *testing.T) {
	s := NewGitService()
	path := t.TempDir()
	s.state.Repositories = []gitengine.Repository{{Path: path}}
	_, ctx, release, err := s.readRepository(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StartScan(t.TempDir()); err == nil {
		t.Fatal("task admitted while reading")
	}
	updates := NewUpdateService(s)
	defer updates.shutdown()
	updates.state.Phase = "ready"
	if err := updates.Restart(); err == nil {
		t.Fatal("restart admitted while reading")
	}
	s.Cancel()
	if ctx.Err() != context.Canceled {
		t.Fatal("read was not cancelled")
	}
	release()
	if !s.scheduler.Pause() {
		t.Fatal("read lease did not drain")
	}
	s.scheduler.Resume()
}

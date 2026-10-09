package main

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	"crab.gitsync/internal/gitengine"
)

func TestFetchFreshnessRequiresCompleteNetworkSuccess(t *testing.T) {
	s, path := retryService(t, false)
	failFetch, failRefresh := false, true
	s.fetchRepository = func(context.Context, string) error {
		if failFetch {
			return fmt.Errorf("one remote failed")
		}
		return nil
	}
	s.inspectRepository = func(_ context.Context, path string) (gitengine.Repository, error) {
		if failRefresh {
			return gitengine.Repository{}, fmt.Errorf("index unavailable")
		}
		return gitengine.Repository{Path: path, Remotes: []gitengine.Remote{{Name: "origin"}}}, nil
	}
	if s.GetState().Repositories[0].LastSuccessfulFetch != "" {
		t.Fatal("initial freshness invented")
	}
	if err := s.StartFetch([]string{path}); err != nil {
		t.Fatal(err)
	}
	source := waitResult(t, s)
	stamp := source.Repositories[0].LastSuccessfulFetch
	if stamp == "" || source.Results[0].Stage != "refresh" {
		t.Fatal("completed network fetch not recorded")
	}
	failRefresh = false
	if err := s.RetryFailed(source.TaskID); err != nil {
		t.Fatal(err)
	}
	if state := waitResult(t, s); state.Repositories[0].LastSuccessfulFetch != stamp {
		t.Fatal("local refresh advanced network freshness")
	}
	failFetch = true
	if err := s.StartFetch([]string{path}); err != nil {
		t.Fatal(err)
	}
	if state := waitResult(t, s); state.Repositories[0].LastSuccessfulFetch != stamp || state.Results[0].NetworkSucceeded {
		t.Fatal("failed/partial fetch advanced freshness")
	}
}

func TestScanRetainsSessionFreshnessWithoutInventingIt(t *testing.T) {
	path := t.TempDir()
	if out, err := exec.Command("git", "init", "-b", "main", path).CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	s := NewGitService()
	if err := s.StartScan(path); err != nil {
		t.Fatal(err)
	}
	state := waitResult(t, s)
	if len(state.Repositories) != 1 || state.Repositories[0].LastSuccessfulFetch != "" {
		t.Fatal("scan claimed remote freshness")
	}
	s.fetchTimes[path] = "2026-10-09T00:00:00Z"
	if err := s.StartScan(path); err != nil {
		t.Fatal(err)
	}
	state = waitResult(t, s)
	if state.Repositories[0].LastSuccessfulFetch != "2026-10-09T00:00:00Z" {
		t.Fatal("scan lost known session freshness")
	}
}

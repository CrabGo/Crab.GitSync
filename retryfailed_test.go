package main

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"

	"crab.gitsync/internal/gitengine"
	"crab.gitsync/internal/taskresult"
)

func TestRetryFailedPreservesSourceAndSkipsSuccessfulFetch(t *testing.T) {
	s := NewGitService()
	root := t.TempDir()
	paths := []string{}
	for _, name := range []string{"success", "network", "refresh", "skip"} {
		path := filepath.Join(root, name)
		paths = append(paths, path)
		r := gitengine.Repository{Path: path, Name: name, Branch: "main", Remotes: []gitengine.Remote{{Name: "origin"}}}
		if name == "skip" {
			r.Remotes = nil
		}
		s.state.Repositories = append(s.state.Repositories, r)
	}
	fetches := []string{}
	inspections := []string{}
	retrying := false
	var callsMu sync.Mutex
	s.fetchRepository = func(_ context.Context, path string) error {
		callsMu.Lock()
		defer callsMu.Unlock()
		fetches = append(fetches, filepath.Base(path))
		if !retrying && filepath.Base(path) == "network" {
			return fmt.Errorf("authentication failed")
		}
		return nil
	}
	s.inspectRepository = func(_ context.Context, path string) (gitengine.Repository, error) {
		callsMu.Lock()
		defer callsMu.Unlock()
		inspections = append(inspections, filepath.Base(path))
		if !retrying && filepath.Base(path) == "refresh" {
			return gitengine.Repository{}, fmt.Errorf("index unavailable")
		}
		return gitengine.Repository{Path: path, Name: filepath.Base(path), Branch: "main", Remotes: []gitengine.Remote{{Name: "origin"}}}, nil
	}
	if err := s.StartFetch(paths); err != nil {
		t.Fatal(err)
	}
	source := waitResult(t, s)
	retrying = true
	fetches, inspections = nil, nil
	if err := s.RetryFailed(source.TaskID); err != nil {
		t.Fatal(err)
	}
	retry := waitResult(t, s)
	if retry.TaskID == source.TaskID || retry.SourceTaskID != source.TaskID || retry.Total != 2 || retry.Succeeded != 2 {
		t.Fatalf("bad retry: %+v", retry)
	}
	sort.Strings(fetches)
	sort.Strings(inspections)
	if !reflect.DeepEqual(fetches, []string{"network"}) || !reflect.DeepEqual(inspections, []string{"network", "refresh"}) {
		t.Fatalf("fetches=%v inspections=%v", fetches, inspections)
	}
	if retry.Results[1].Stage != "refresh" || !retry.Results[1].NetworkSucceeded || retry.Repositories[0].FetchStatus != "success" || retry.Repositories[3].FetchStatus != "skipped" {
		t.Fatal("lost refresh provenance or unrelated status")
	}
	stored, err := s.GetFetchResults(source.TaskID)
	if err != nil || !reflect.DeepEqual(stored, source.Results) {
		t.Fatal("source overwritten", err)
	}
	stored[1].Failure.Message = "changed"
	stored, _ = s.GetFetchResults(source.TaskID)
	if stored[1].Failure.Message == "changed" {
		t.Fatal("history snapshot is mutable")
	}
	if err := s.RetryFailed(retry.TaskID); err == nil {
		t.Fatal("successful task retried")
	}
}

func TestRetryFailedRevalidatesScopeAndRejectsBusy(t *testing.T) {
	s := NewGitService()
	root := t.TempDir()
	present, removed := filepath.Join(root, "present"), filepath.Join(root, "removed")
	s.state.Repositories = []gitengine.Repository{{Path: present, Remotes: []gitengine.Remote{{Name: "origin"}}}}
	s.fetchHistory["source"] = []taskresult.Result{{Path: present, Status: "error"}, {Path: removed, Status: "error"}, {Path: "cancelled", Status: "cancelled"}, {Path: "skipped", Status: "skipped"}}
	if err := s.RetryFailed("missing"); err == nil {
		t.Fatal("unknown source accepted")
	}
	if err := s.RetryFailed("source"); err == nil || s.GetState().Busy {
		t.Fatal("stale scan scope accepted")
	}
	s.state.Repositories = append(s.state.Repositories, gitengine.Repository{Path: removed, Remotes: []gitengine.Remote{{Name: "origin"}}})
	s.state.Busy = true
	if err := s.RetryFailed("source"); err == nil {
		t.Fatal("busy retry accepted")
	}
	s.state.Busy = false
	s.fetchHistory["cancel-only"] = []taskresult.Result{{Path: present, Status: "cancelled"}}
	if err := s.RetryFailed("cancel-only"); err == nil {
		t.Fatal("cancelled task retried")
	}
	s.fetchRepository = func(context.Context, string) error { return nil }
	s.inspectRepository = func(_ context.Context, path string) (gitengine.Repository, error) {
		return gitengine.Repository{Path: path}, nil
	}
	if err := s.RetryFailed("source"); err != nil {
		t.Fatal(err)
	}
	if state := waitResult(t, s); state.Total != 2 {
		t.Fatal("non-errors included", state.Total)
	}
}

func TestFetchHistoryIsBounded(t *testing.T) {
	s := NewGitService()
	path := filepath.Join(t.TempDir(), "repo")
	s.state.Repositories = []gitengine.Repository{{Path: path}}
	first := ""
	for i := 0; i < 11; i++ {
		if err := s.StartFetch([]string{path}); err != nil {
			t.Fatal(err)
		}
		state := waitResult(t, s)
		if first == "" {
			first = state.TaskID
		}
	}
	if len(s.fetchHistory) != 10 {
		t.Fatal("unbounded history")
	}
	if _, err := s.GetFetchResults(first); err == nil {
		t.Fatal("expired history retained")
	}
}

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"crab.gitsync/internal/gitengine"
	"crab.gitsync/internal/taskhistory"
	"crab.gitsync/internal/tasksettings"
)

func historyService(path string) *GitService {
	s := NewGitService()
	s.history = taskhistory.New(path)
	_ = s.history.Prune(100, 30)
	s.fetchTimes = s.history.FetchTimes()
	return s
}
func TestHistorySurvivesRestartAndFetchFreshnessRescan(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config", "history.json")
	repo := filepath.Join(root, "client")
	os.Mkdir(repo, 0700)
	testGit(t, repo, "init", "-b", "main")
	remote := filepath.Join(root, "remote.git")
	testGit(t, root, "init", "--bare", remote)
	testGit(t, repo, "remote", "add", "origin", remote)
	s := historyService(path)
	if err := s.StartScan(repo); err != nil {
		t.Fatal(err)
	}
	awaitTask(t, s)
	if err := s.StartFetch([]string{repo}); err != nil {
		t.Fatal(err)
	}
	fetch := awaitTask(t, s)
	stamp := fetch.Repositories[0].LastSuccessfulFetch
	if stamp == "" || fetch.Succeeded != 1 {
		t.Fatal(fetch)
	}
	loaded := historyService(path)
	if len(loaded.ListTaskHistory()) != 2 {
		t.Fatal("tasks not loaded")
	}
	record, err := loaded.GetTaskHistory(fetch.TaskID)
	if err != nil || len(record.Logs) == 0 || record.Succeeded != 1 {
		t.Fatal("archive missing", record, err)
	}
	if _, err := loaded.GetFetchResults(fetch.TaskID); err != nil {
		t.Fatal("fetch provenance lost", err)
	}
	if err := loaded.StartScan(repo); err != nil {
		t.Fatal(err)
	}
	rescanned := awaitTask(t, loaded)
	if rescanned.Repositories[0].LastSuccessfulFetch != stamp {
		t.Fatal("freshness lost on restart/rescan")
	}
	loaded.fetchRepository = func(context.Context, string) error { return fmt.Errorf("authentication failed") }
	if err := loaded.StartFetch([]string{repo}); err != nil {
		t.Fatal(err)
	}
	failed := awaitTask(t, loaded)
	retry := historyService(path)
	if err := retry.RetryFailed(failed.TaskID); err == nil {
		t.Fatal("historical retry bypassed scanned scope")
	}
	if err := retry.StartScan(repo); err != nil {
		t.Fatal(err)
	}
	awaitTask(t, retry)
	if err := retry.RetryFailed(failed.TaskID); err != nil {
		t.Fatal(err)
	}
	if state := awaitTask(t, retry); state.Succeeded != 1 || state.SourceTaskID != failed.TaskID {
		t.Fatal("historical retry failed", state)
	}
}
func TestHistoryWriteFailureDoesNotFailSuccessfulGitTask(t *testing.T) {
	s := NewGitService()
	root := t.TempDir()
	s.history = taskhistory.New(root)
	s.state.Repositories = []gitengine.Repository{{Path: root, Remotes: []gitengine.Remote{{Name: "origin"}}}}
	s.fetchRepository = func(context.Context, string) error { return nil }
	s.inspectRepository = func(_ context.Context, path string) (gitengine.Repository, error) {
		return gitengine.Repository{Path: path}, nil
	}
	if err := s.StartFetch([]string{root}); err != nil {
		t.Fatal(err)
	}
	state := awaitTask(t, s)
	if state.Succeeded != 1 || state.Failed != 0 || state.Results[0].Status != "success" || s.GetHistoryWarning() == "" {
		t.Fatal("storage failure changed Git result", state)
	}
	if _, err := s.GetTaskHistory(state.TaskID); err != nil {
		t.Fatal("session snapshot lost", err)
	}
}

func TestRetentionChangeLeavesActiveTaskUntouched(t *testing.T) {
	root := t.TempDir()
	s := historyService(filepath.Join(root, "history.json"))
	setTestConcurrency(t, s, 1)
	for _, id := range []string{"one", "two"} {
		if err := s.history.Add(taskhistory.Record{Summary: taskhistory.Summary{ID: id, Phase: "done", FinishedAt: time.Now().Format(time.RFC3339)}}, nil, 100, 30); err != nil {
			t.Fatal(err)
		}
	}
	s.state.Repositories = []gitengine.Repository{{Path: root, Remotes: []gitengine.Remote{{Name: "origin"}}}}
	entered := make(chan struct{})
	s.fetchRepository = func(ctx context.Context, _ string) error { close(entered); <-ctx.Done(); return ctx.Err() }
	if err := s.StartFetch([]string{root}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("task not started")
	}
	active := s.GetState()
	if err := s.SaveTaskConfig(tasksettings.Config{Concurrency: 1, HistoryTasks: 1, HistoryDays: 30}); err != nil {
		t.Fatal(err)
	}
	list := s.ListTaskHistory()
	if len(list) != 1 || list[0].ID == active.TaskID || !s.GetState().Busy {
		t.Fatal("active task affected by retention", list)
	}
	s.Cancel()
	finished := awaitTask(t, s)
	list = s.ListTaskHistory()
	if len(list) != 1 || list[0].ID != finished.TaskID {
		t.Fatal("finished task not retained", list)
	}
}

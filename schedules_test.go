package main

import (
	"context"
	"crab.gitsync/internal/gitengine"
	"crab.gitsync/internal/scansettings"
	"fmt"
	"github.com/wailsapp/wails/v3/pkg/application"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func scheduledService(t *testing.T) (*GitService, scansettings.List, time.Time) {
	t.Helper()
	s := NewGitService()
	s.scanLists, _ = scansettings.New(filepath.Join(t.TempDir(), "scans.json"))
	list, err := s.SaveScanList(scansettings.List{Name: "work", Roots: []string{t.TempDir()}, Scheduled: true, IntervalMinutes: 1})
	if err != nil {
		t.Fatal(err)
	}
	due, _ := time.Parse(time.RFC3339Nano, s.GetSchedules()[0].NextRun)
	return s, list, due
}
func TestScheduleDefaultsPersistenceAndValidation(t *testing.T) {
	s, list, _ := scheduledService(t)
	list.Scheduled = false
	list.IntervalMinutes = 0
	saved, err := s.SaveScanList(list)
	if err != nil || saved.Scheduled || saved.IntervalMinutes != 30 {
		t.Fatal(saved, err)
	}
	for _, bad := range []int{-1, 1441} {
		saved.IntervalMinutes = bad
		if _, err := s.SaveScanList(saved); err == nil {
			t.Fatal("invalid interval accepted")
		}
	}
}
func TestScheduleBusySkipResumeAndDisable(t *testing.T) {
	s, list, due := scheduledService(t)
	ctx, err := s.begin("scan", "", "discovering")
	if err != nil {
		t.Fatal(err)
	}
	s.scheduleTick(due.Add(24 * time.Hour))
	snapshot := s.GetSchedules()[0]
	next, _ := time.Parse(time.RFC3339Nano, snapshot.NextRun)
	if snapshot.Status != "busy-skip" || next != due.Add(24*time.Hour+time.Minute) {
		t.Fatal(snapshot)
	}
	s.finish(ctx, nil)
	s.scheduleTick(due.Add(24*time.Hour + time.Second))
	if s.GetState().Busy {
		t.Fatal("overdue schedule queued backlog")
	}
	list.Scheduled = false
	if _, err = s.SaveScanList(list); err != nil {
		t.Fatal(err)
	}
	s.scheduleTick(next.Add(24 * time.Hour))
	if s.GetState().Busy || s.GetSchedules()[0].Status != "disabled" {
		t.Fatal("disabled schedule still dispatched")
	}
	list.Scheduled = true
	s.SaveScanList(list)
	s.shutdownSchedules()
	s.scheduleTick(time.Now().Add(48 * time.Hour))
	if s.GetState().Busy {
		t.Fatal("shutdown allowed new dispatch")
	}
}

func TestSchedulesWithSameDeadlineDoNotStarve(t *testing.T) {
	s, first, due := scheduledService(t)
	second, err := s.SaveScanList(scansettings.List{Name: "second", Roots: []string{t.TempDir()}, Scheduled: true, IntervalMinutes: 1})
	if err != nil {
		t.Fatal(err)
	}
	s.scheduleTick(due.Add(time.Second))
	state := awaitTask(t, s)
	if state.ScanListID != first.ID {
		t.Fatal("first eligible list was not dispatched", state)
	}
	s.scheduleTick(due.Add(time.Minute + time.Second))
	state = awaitTask(t, s)
	if state.ScanListID != second.ID {
		t.Fatal("never-run list starved", state)
	}
}

func TestScheduleServiceLifecycle(t *testing.T) {
	s, _, due := scheduledService(t)
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	s.scheduleTick(due.Add(24 * time.Hour))
	if s.GetState().Busy {
		t.Fatal("service shutdown allowed schedule admission")
	}
}
func TestScheduledFetchSnapshotCancellationAndDisableDuringRun(t *testing.T) {
	s, list, due := scheduledService(t)
	repo := filepath.Join(list.Roots[0], "repo")
	testGit(t, list.Roots[0], "init", "-b", "main", repo)
	entered := make(chan struct{})
	release := make(chan struct{})
	s.inspectRepository = func(_ context.Context, path string) (gitengine.Repository, error) {
		return gitengine.Repository{Path: path, Name: "repo", Remotes: []gitengine.Remote{{Name: "origin"}}}, nil
	}
	s.fetchRepository = func(ctx context.Context, _ string) error { close(entered); <-release; return ctx.Err() }
	s.scheduleTick(due)
	<-entered
	if !s.GetState().Automatic || s.GetState().ScanListID != list.ID {
		t.Fatal("automatic scope not recorded")
	}
	list.Scheduled = false
	list.Roots = []string{t.TempDir()}
	if _, err := s.SaveScanList(list); err != nil {
		t.Fatal(err)
	}
	s.Cancel()
	s.scheduleTick(due.Add(48 * time.Hour))
	if !s.GetState().Busy {
		t.Fatal("cancel did not wait in-flight work")
	}
	close(release)
	state := awaitTask(t, s)
	if state.Phase != "cancelled" || len(state.Results) != 1 || scansettings.PathKey(state.Results[0].Path) != scansettings.PathKey(repo) || s.GetSchedules()[0].Status != "disabled" {
		t.Fatal(state, s.GetSchedules())
	}
}
func TestScheduledFetchNewCommitsAndQuietSuccess(t *testing.T) {
	s, list, due := scheduledService(t)
	root := list.Roots[0]
	source := filepath.Join(root, "source")
	repo := filepath.Join(root, "client")
	remote := filepath.Join(t.TempDir(), "remote.git")
	testGit(t, root, "init", "-b", "main", source)
	testGit(t, source, "config", "user.name", "test")
	testGit(t, source, "config", "user.email", "test@example.invalid")
	os.WriteFile(filepath.Join(source, "file"), []byte("one"), 0600)
	testGit(t, source, "add", "file")
	testGit(t, source, "commit", "-m", "first")
	testGit(t, root, "clone", "--bare", source, remote)
	testGit(t, root, "clone", remote, repo)
	list.Roots = []string{repo}
	list, _ = s.SaveScanList(list)
	due, _ = time.Parse(time.RFC3339Nano, s.GetSchedules()[0].NextRun)
	notices := make(chan string, 4)
	s.notify = func(title, _, _ string) { notices <- title }
	testGit(t, source, "remote", "add", "origin", remote)
	os.WriteFile(filepath.Join(source, "file"), []byte("two"), 0600)
	testGit(t, source, "commit", "-am", "second")
	testGit(t, source, "push", "origin", "main")
	head, _ := gitengine.Run(context.Background(), repo, time.Second*15, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(repo, "file"), []byte("local edit"), 0600)
	s.scheduleTick(due)
	state := awaitTask(t, s)
	if state.Succeeded != 1 || state.Kind != "fetch" {
		t.Fatal(state)
	}
	select {
	case title := <-notices:
		if title != "发现远端新提交" {
			t.Fatal(title)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("missing new commit notification")
	}
	after, _ := gitengine.Run(context.Background(), repo, time.Second*15, "rev-parse", "HEAD")
	content, _ := os.ReadFile(filepath.Join(repo, "file"))
	if head != after || string(content) != "local edit" {
		t.Fatal("scheduled fetch mutated worktree")
	}
	s.scheduleTick(due.Add(time.Minute))
	awaitTask(t, s)
	select {
	case title := <-notices:
		t.Fatal("unchanged success should be quiet", title)
	case <-time.After(100 * time.Millisecond):
	}
	s.fetchRepository = func(context.Context, string) error { return fmt.Errorf("authentication failed") }
	s.scheduleTick(due.Add(2 * time.Minute))
	state = awaitTask(t, s)
	if state.Failed != 1 {
		t.Fatal(state)
	}
	select {
	case title := <-notices:
		if title != "定时获取出现错误" {
			t.Fatal(title)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("failure notification missing")
	}
}

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"crab.gitsync/internal/gitengine"
)

func gitText(t *testing.T, path string, args ...string) string {
	t.Helper()
	out, err := gitengine.Run(context.Background(), path, 20*time.Second, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestM1RealBatchRetryAndContextFetchPreserveWorktree(t *testing.T) {
	root := t.TempDir()
	producer := filepath.Join(root, "producer")
	os.Mkdir(producer, 0700)
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}} {
		testGit(t, producer, args...)
	}
	os.WriteFile(filepath.Join(producer, "tracked.txt"), []byte("initial"), 0600)
	testGit(t, producer, "add", "tracked.txt")
	testGit(t, producer, "-c", "commit.gpgsign=false", "commit", "-m", "initial")
	remote := filepath.Join(root, "remote.git")
	testGit(t, root, "clone", "--bare", producer, remote)
	scanRoot := filepath.Join(root, "workspace")
	os.Mkdir(scanRoot, 0700)
	paths := []string{}
	for _, name := range []string{"success", "retry"} {
		path := filepath.Join(scanRoot, name)
		testGit(t, root, "clone", remote, path)
		paths = append(paths, path)
	}
	local := filepath.Join(scanRoot, "no-remote")
	os.Mkdir(local, 0700)
	testGit(t, local, "init", "-b", "main")
	paths = append(paths, local)
	testGit(t, paths[1], "remote", "set-url", "origin", filepath.Join(root, "missing.git"))
	os.WriteFile(filepath.Join(producer, "tracked.txt"), []byte("remote update"), 0600)
	testGit(t, producer, "add", "tracked.txt")
	testGit(t, producer, "-c", "commit.gpgsign=false", "commit", "-m", "update")
	testGit(t, producer, "push", remote, "main")
	heads := map[string]string{}
	for _, path := range paths[:2] {
		heads[path] = gitText(t, path, "rev-parse", "HEAD")
		os.WriteFile(filepath.Join(path, "tracked.txt"), []byte("local dirty"), 0600)
		os.WriteFile(filepath.Join(path, "untracked.txt"), []byte("keep new file"), 0600)
	}
	s := NewGitService()
	if err := s.StartScan(scanRoot); err != nil {
		t.Fatal(err)
	}
	awaitTask(t, s)
	if err := s.StartFetch(paths); err != nil {
		t.Fatal(err)
	}
	source := awaitTask(t, s)
	if source.Succeeded != 1 || source.Failed != 1 || source.Skipped != 1 {
		t.Fatalf("unexpected batch %+v", source.Results)
	}
	testGit(t, paths[1], "remote", "set-url", "origin", remote)
	if err := s.RetryFailed(source.TaskID); err != nil {
		t.Fatal(err)
	}
	retried := awaitTask(t, s)
	if retried.Total != 1 || retried.Succeeded != 1 || retried.SourceTaskID != source.TaskID {
		t.Fatal("unexpected retry", retried.Results)
	}
	if err := s.StartAction(paths[0], "fetch", "", false); err != nil {
		t.Fatal(err)
	}
	if state := awaitTask(t, s); state.Succeeded != 1 {
		t.Fatal("context fetch failed", state.Results)
	}
	for _, path := range paths[:2] {
		if gitText(t, path, "rev-parse", "HEAD") != heads[path] {
			t.Fatal("fetch changed head")
		}
		data, _ := os.ReadFile(filepath.Join(path, "tracked.txt"))
		if string(data) != "local dirty" {
			t.Fatal("fetch changed tracked files")
		}
		data, _ = os.ReadFile(filepath.Join(path, "untracked.txt"))
		if string(data) != "keep new file" {
			t.Fatal("fetch removed new files")
		}
		r, err := gitengine.Inspect(context.Background(), path)
		if err != nil || r.SyncStatus != "behind" || r.Behind != 1 || r.Changed != 2 {
			t.Fatal("wrong state", r, err)
		}
	}
	stored, err := s.GetFetchResults(source.TaskID)
	if err != nil || stored[1].Status != "error" {
		t.Fatal("source overwritten")
	}
}

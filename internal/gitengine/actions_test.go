package gitengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscardKeepsCommitsAndNewFiles(t *testing.T) {
	path := t.TempDir()
	initRepo(t, path)
	commit(t, path, "tracked.txt", "original")
	head := git(t, path, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(path, "tracked.txt"), []byte("changed"), 0644)
	os.WriteFile(filepath.Join(path, "new.txt"), []byte("new staged file"), 0644)
	os.WriteFile(filepath.Join(path, "untracked.txt"), []byte("untracked"), 0644)
	git(t, path, "add", "tracked.txt", "new.txt")
	if _, err := Action(context.Background(), path, "discard", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(path, "tracked.txt"))
	if string(data) != "original" {
		t.Fatal("tracked content not restored")
	}
	for _, name := range []string{"new.txt", "untracked.txt"} {
		if _, err := os.Stat(filepath.Join(path, name)); err != nil {
			t.Fatal("new file lost", err)
		}
	}
	if git(t, path, "rev-parse", "HEAD") != head || git(t, path, "diff", "--cached") != "" {
		t.Fatal("commit or index changed unexpectedly")
	}
}

func TestDiscardOnlyNewFilesAndChangedBranch(t *testing.T) {
	path := t.TempDir()
	initRepo(t, path)
	git(t, path, "commit", "--allow-empty", "-m", "empty")
	os.WriteFile(filepath.Join(path, "new.txt"), []byte("new"), 0644)
	git(t, path, "add", "new.txt")
	if _, err := Action(context.Background(), path, "discard", "", "other"); err == nil {
		t.Fatal("changed branch accepted")
	}
	if git(t, path, "diff", "--cached", "--name-only") == "" {
		t.Fatal("index changed despite branch mismatch")
	}
	if _, err := Action(context.Background(), path, "discard", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "new.txt")); err != nil {
		t.Fatal("new file lost")
	}
}

func TestMergeValidationAndConflictRecovery(t *testing.T) {
	path := t.TempDir()
	initRepo(t, path)
	commit(t, path, "file.txt", "base")
	git(t, path, "checkout", "-b", "feature")
	commit(t, path, "file.txt", "feature")
	git(t, path, "checkout", "main")
	commit(t, path, "file.txt", "main")
	head := git(t, path, "rev-parse", "HEAD")
	if _, err := Action(context.Background(), path, "merge", "--abort"); err == nil {
		t.Fatal("option accepted as target")
	}
	os.WriteFile(filepath.Join(path, "untracked.txt"), []byte("keep"), 0644)
	if _, err := Action(context.Background(), path, "merge", "refs/heads/feature"); err == nil {
		t.Fatal("dirty worktree accepted")
	}
	os.Remove(filepath.Join(path, "untracked.txt"))
	preview, err := PreviewMerge(context.Background(), path, "merge", "refs/heads/feature")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ExecuteMerge(context.Background(), preview, "merge"); err == nil {
		t.Fatal("conflicting merge succeeded")
	}
	if git(t, path, "rev-parse", "--verify", "MERGE_HEAD") == "" {
		t.Fatal("conflict state lost")
	}
	if repo, err := Inspect(context.Background(), path); err != nil || !repo.MergeInProgress {
		t.Fatal("merge state missing from snapshot", err)
	}
	if _, err := Action(context.Background(), path, "discard", ""); err == nil {
		t.Fatal("discard accepted during merge")
	}
	if _, err := Action(context.Background(), path, "abort-merge", ""); err != nil {
		t.Fatal(err)
	}
	if git(t, path, "rev-parse", "HEAD") != head || git(t, path, "status", "--porcelain") != "" {
		t.Fatal("abort failed to restore pre-merge state")
	}
	if repo, err := Inspect(context.Background(), path); err != nil || repo.MergeInProgress {
		t.Fatal("merge state not cleared", err)
	}
}

func TestPullMergeAndFetchSemantics(t *testing.T) {
	root := t.TempDir()
	producer := filepath.Join(root, "producer")
	initRepo(t, producer)
	commit(t, producer, "file.txt", "first")
	remote := filepath.Join(root, "remote.git")
	git(t, root, "clone", "--bare", producer, remote)
	client := filepath.Join(root, "client")
	git(t, root, "clone", remote, client)
	git(t, client, "config", "user.name", "Test")
	git(t, client, "config", "user.email", "test@example.invalid")
	commit(t, producer, "file.txt", "second")
	git(t, producer, "push", remote, "main")
	head := git(t, client, "rev-parse", "HEAD")
	if _, err := Action(context.Background(), client, "fetch", ""); err != nil {
		t.Fatal(err)
	}
	if git(t, client, "rev-parse", "HEAD") != head {
		t.Fatal("fetch changed HEAD")
	}
	if _, err := Action(context.Background(), client, "pull-merge", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(client, "file.txt"))
	if string(data) != "second" {
		t.Fatal("upstream not merged")
	}
	git(t, client, "branch", "--unset-upstream")
	if _, err := Action(context.Background(), client, "pull-merge", ""); err == nil {
		t.Fatal("missing upstream accepted")
	}
}

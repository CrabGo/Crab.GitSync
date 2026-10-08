package gitengine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func git(t *testing.T, path string, args ...string) string {
	t.Helper()
	out, err := Run(context.Background(), path, 20*time.Second, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func initRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	git(t, path, "init", "-b", "main")
	git(t, path, "config", "user.name", "GitSync Test")
	git(t, path, "config", "user.email", "test@example.invalid")
}

func commit(t *testing.T, path, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(path, name), []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	git(t, path, "add", "--", name)
	git(t, path, "-c", "commit.gpgsign=false", "commit", "-m", text)
}

func TestDiscoverNestedWorktreeBareAndInvalid(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "父仓库 with spaces")
	initRepo(t, parent)
	commit(t, parent, "file.txt", "initial")
	nested := filepath.Join(parent, "nested")
	initRepo(t, nested)
	worktree := filepath.Join(root, "worktree")
	git(t, parent, "worktree", "add", "-b", "feature", worktree)
	bare := filepath.Join(root, "bare.git")
	git(t, root, "init", "--bare", "-b", "main", bare)
	ignored := filepath.Join(root, "node_modules", "ignored")
	initRepo(t, ignored)
	invalid := filepath.Join(root, "invalid")
	if err := os.MkdirAll(filepath.Join(invalid, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	paths, err := Discover(context.Background(), root, func(int, string) {}, func(message string) { t.Error(message) })
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 5 {
		t.Fatalf("got candidates %v, want 5", paths)
	}
	valid := 0
	for _, path := range paths {
		r, err := Inspect(context.Background(), path)
		if path == invalid {
			if err == nil {
				t.Fatal("accepted invalid .git")
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		valid++
		if path == bare && !r.Bare {
			t.Fatal("bare repo not recognised")
		}
		if path == worktree && r.Branch != "feature" {
			t.Fatalf("worktree branch = %s", r.Branch)
		}
	}
	if valid != 4 {
		t.Fatalf("valid = %d", valid)
	}
}

func TestFetchPreservesHeadAndDirtyWorkspace(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	git(t, root, "init", "--bare", "-b", "main", remote)
	producer := filepath.Join(root, "producer")
	initRepo(t, producer)
	commit(t, producer, "tracked.txt", "first")
	git(t, producer, "remote", "add", "origin", remote)
	git(t, producer, "push", "-u", "origin", "main")
	consumer := filepath.Join(root, "consumer")
	git(t, root, "clone", remote, consumer)
	commit(t, producer, "tracked.txt", "second")
	git(t, producer, "push", "origin", "main")
	head := git(t, consumer, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(consumer, "tracked.txt"), []byte("local dirty"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Fetch(context.Background(), consumer); err != nil {
		t.Fatal(err)
	}
	if got := git(t, consumer, "rev-parse", "HEAD"); got != head {
		t.Fatal("fetch changed HEAD")
	}
	contents, err := os.ReadFile(filepath.Join(consumer, "tracked.txt"))
	if err != nil || string(contents) != "local dirty" {
		t.Fatal("fetch changed working files")
	}
	r, err := Inspect(context.Background(), consumer)
	if err != nil {
		t.Fatal(err)
	}
	if r.Behind != 1 || r.Ahead != 0 || r.Changed != 1 || r.Upstream != "origin/main" {
		t.Fatalf("wrong metadata: %+v", r)
	}
	git(t, consumer, "checkout", "--detach")
	r, err = Inspect(context.Background(), consumer)
	if err != nil || !r.Detached {
		t.Fatalf("detached HEAD: %+v %v", r, err)
	}
}

func TestCancellationAndRemoteCredentials(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Discover(ctx, t.TempDir(), func(int, string) {}, func(string) {}); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := Run(ctx, t.TempDir(), time.Second, "status"); err != context.Canceled {
		t.Fatalf("cancel git: %v", err)
	}
	root := t.TempDir()
	initRepo(t, root)
	git(t, root, "remote", "add", "origin", "https://alice:secret@github.com/example/repo.git?token=secret")
	r, err := Inspect(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Remotes) != 1 || !r.Remotes[0].GitHub || strings.Contains(r.Remotes[0].URL, "secret") {
		t.Fatalf("unsafe remote: %+v", r.Remotes)
	}
	if strings.Contains(Redact("fatal https://alice:secret@github.com/foo?token=secret"), "secret") {
		t.Fatal("leaked credentials in error")
	}
}

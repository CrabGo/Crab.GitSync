package gitengine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultMergeFastForwardsAndRejectsDivergence(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir()
	initRepo(t, path)
	commit(t, path, "file.txt", "initial")
	git(t, path, "checkout", "-b", "feature")
	commit(t, path, "feature.txt", "feature")
	git(t, path, "checkout", "main")
	p, err := PreviewMerge(ctx, path, "merge", "refs/heads/feature")
	if err != nil {
		t.Fatal(err)
	}
	if p.Ahead != 0 || p.Behind != 1 || p.Diverged {
		t.Fatal("bad preview", p)
	}
	if _, err = Action(ctx, path, "merge", "refs/heads/feature"); err != nil {
		t.Fatal(err)
	}
	if git(t, path, "rev-parse", "HEAD") != p.TargetHead {
		t.Fatal("not fast forwarded")
	}
	git(t, path, "checkout", "feature")
	commit(t, path, "feature.txt", "next feature")
	git(t, path, "checkout", "main")
	commit(t, path, "local.txt", "local")
	head := git(t, path, "rev-parse", "HEAD")
	status := git(t, path, "status", "--porcelain")
	if _, err = Action(ctx, path, "merge", "refs/heads/feature"); err == nil {
		t.Fatal("default silently merged diverged branches")
	}
	if git(t, path, "rev-parse", "HEAD") != head || git(t, path, "status", "--porcelain") != status {
		t.Fatal("rejection changed repository")
	}
	p, err = PreviewMerge(ctx, path, "merge", "refs/heads/feature")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Diverged || p.Ahead != 1 || p.Behind != 1 {
		t.Fatal("bad diverged preview", p)
	}
	if _, _, err = ExecuteMerge(ctx, p, "merge"); err != nil {
		t.Fatal("explicit merge failed", err)
	}
	if len(strings.Fields(git(t, path, "rev-list", "--parents", "-1", "HEAD"))) != 3 {
		t.Fatal("explicit merge commit missing")
	}
}

func TestMergeRejectsStaleConfirmationAndDirtyWorktree(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir()
	initRepo(t, path)
	commit(t, path, "file.txt", "initial")
	git(t, path, "checkout", "-b", "feature")
	commit(t, path, "feature.txt", "feature")
	git(t, path, "checkout", "main")
	p, err := PreviewMerge(ctx, path, "merge", "refs/heads/feature")
	if err != nil {
		t.Fatal(err)
	}
	head := git(t, path, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(path, "new.txt"), []byte("keep"), 0600)
	if _, _, err = ExecuteMerge(ctx, p, "ff-only"); err == nil {
		t.Fatal("dirty worktree accepted")
	}
	if git(t, path, "rev-parse", "HEAD") != head {
		t.Fatal("dirty rejection moved head")
	}
	os.Remove(filepath.Join(path, "new.txt"))
	git(t, path, "checkout", "feature")
	commit(t, path, "feature.txt", "changed target")
	git(t, path, "checkout", "main")
	if _, _, err = ExecuteMerge(ctx, p, "merge"); err == nil {
		t.Fatal("stale target accepted")
	}
	git(t, path, "checkout", "feature")
	if _, _, err = ExecuteMerge(ctx, p, "ff-only"); err == nil {
		t.Fatal("changed current branch accepted")
	}
}

func TestPullMergeRevalidatesAfterFetch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	producer := filepath.Join(root, "producer")
	initRepo(t, producer)
	commit(t, producer, "file.txt", "first")
	remote := filepath.Join(root, "remote.git")
	git(t, root, "clone", "--bare", producer, remote)
	client := filepath.Join(root, "client")
	git(t, root, "clone", remote, client)
	p, err := PreviewMerge(ctx, client, "pull-merge", "")
	if err != nil {
		t.Fatal(err)
	}
	commit(t, producer, "file.txt", "new remote")
	git(t, producer, "push", remote, "main")
	head := git(t, client, "rev-parse", "HEAD")
	if _, fetched, err := ExecuteMerge(ctx, p, "merge"); err == nil || !fetched {
		t.Fatal("ordinary merge used stale fetched confirmation", fetched, err)
	}
	if git(t, client, "rev-parse", "HEAD") != head {
		t.Fatal("stale confirmation moved head")
	}
	p, err = PreviewMerge(ctx, client, "pull-merge", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = ExecuteMerge(ctx, p, "ff-only"); err != nil {
		t.Fatal(err)
	}
	// Reproduce an external branch change during fetch deterministically.
	p, err = PreviewMerge(ctx, client, "pull-merge", "")
	if err != nil {
		t.Fatal(err)
	}
	_, fetched, err := executeMerge(ctx, p, "ff-only", func(context.Context, string) error { git(t, client, "checkout", "-b", "external"); return nil })
	if err == nil || !fetched {
		t.Fatal("post-fetch external branch change accepted")
	}
	if git(t, client, "rev-parse", "HEAD") != p.Head {
		t.Fatal("changed head after external branch change")
	}
}

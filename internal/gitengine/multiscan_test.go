package gitengine

import (
	"context"
	"crab.gitsync/internal/scansettings"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverManyDeduplicatesAndContinuesAfterBadRoot(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "nested", "b")
	excludedRepo := filepath.Join(root, "archive", "repo")
	for _, path := range []string{a, b, excludedRepo} {
		initRepo(t, path)
	}
	file := filepath.Join(root, "file.txt")
	os.WriteFile(file, []byte("not a directory"), 0600)
	visited := 0
	warnings := 0
	paths, err := DiscoverMany(context.Background(), []string{filepath.Join(root, "missing"), a, root, root, file}, []string{"archive"}, func(n int, _ string) {
		if n <= visited {
			t.Fatal("progress moved backwards")
		}
		visited = n
	}, func(string) { warnings++ })
	if err != nil || len(paths) != 2 || warnings != 2 {
		t.Fatal("unexpected discovery", paths, warnings, err)
	}
	for _, path := range paths {
		if path == excludedRepo {
			t.Fatal("excluded repo included")
		}
	}
	paths, err = DiscoverMany(context.Background(), []string{root}, []string{filepath.Join(root, "nested"), "archive"}, func(int, string) {}, func(string) {})
	if err != nil || len(paths) != 1 || scansettings.PathKey(paths[0]) != scansettings.PathKey(a) {
		t.Fatal("absolute exclusion failed", paths, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = DiscoverMany(ctx, []string{root}, nil, func(int, string) {}, func(string) {}); err != context.Canceled {
		t.Fatal("cancelled scan continued", err)
	}
}

func TestDiscoverManyDoesNotFollowChildSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	initRepo(t, outside)
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skip("symlink creation unavailable:", err)
	}
	paths, err := DiscoverMany(context.Background(), []string{root}, nil, func(int, string) {}, func(string) {})
	if err != nil || len(paths) != 0 {
		t.Fatal("followed child symlink", paths, err)
	}
}

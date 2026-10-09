package gitengine

import (
	"path/filepath"
	"testing"

	"crab.gitsync/internal/scansettings"
)

func TestCommonDirectoryForLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	initRepo(t, main)
	commit(t, main, "tracked", "base")
	linked := filepath.Join(root, "linked")
	git(t, main, "worktree", "add", "-b", "linked", linked)
	if scansettings.PathKey(CommonDirectory(main)) != scansettings.PathKey(CommonDirectory(linked)) {
		t.Fatal("linked worktrees do not share a repository lease")
	}
	bare := filepath.Join(root, "bare.git")
	git(t, root, "init", "--bare", bare)
	if scansettings.PathKey(CommonDirectory(bare)) != scansettings.PathKey(bare) {
		t.Fatal("bare repository key incorrect")
	}
}

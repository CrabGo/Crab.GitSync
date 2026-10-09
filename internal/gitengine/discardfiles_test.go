package gitengine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func discardTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	initRepo(t, root)
	git(t, root, "config", "core.autocrlf", "false")
	for _, name := range []string{"chosen.txt", "other.txt", "binary.dat", "nested/gone.txt", "literal[1].txt"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, []byte("base\n"), 0600)
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "base")
	return root
}
func readDiscardTest(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestDiscardFilesBackupRestoresWorktreeAndIndexExactly(t *testing.T) {
	root := discardTestRepo(t)
	ctx := context.Background()
	backup := t.TempDir()
	staged := []byte("staged  \r\n")
	working := []byte("working  \r\nlast space ")
	binary := []byte{0, 255, 1, 2, 10, 32, 32}
	os.WriteFile(filepath.Join(root, "chosen.txt"), staged, 0600)
	git(t, root, "add", "chosen.txt")
	os.WriteFile(filepath.Join(root, "chosen.txt"), working, 0600)
	os.WriteFile(filepath.Join(root, "binary.dat"), binary, 0600)
	os.WriteFile(filepath.Join(root, "other.txt"), []byte("keep other"), 0600)
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("new staged"), 0600)
	git(t, root, "add", "new.txt")
	os.WriteFile(filepath.Join(root, "loose.txt"), []byte("untracked"), 0600)
	p, err := PreviewDiscard(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range p.Files {
		if f.Path == "new.txt" || f.Path == "loose.txt" {
			t.Fatal("new files offered for discard")
		}
	}
	info, err := DiscardFiles(ctx, p, []string{"chosen.txt", "binary.dat"}, backup)
	if err != nil || !info.Ready {
		t.Fatal(info, err)
	}
	if !bytes.Equal(readDiscardTest(t, filepath.Join(root, "chosen.txt")), []byte("base\n")) {
		t.Fatal("selected file not discarded")
	}
	if string(readDiscardTest(t, filepath.Join(root, "other.txt"))) != "keep other" || string(readDiscardTest(t, filepath.Join(root, "new.txt"))) != "new staged" {
		t.Fatal("unselected files changed")
	}
	if err = RestoreDiscardBackup(ctx, root, backup, info.ID); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readDiscardTest(t, filepath.Join(root, "chosen.txt")), working) || !bytes.Equal(readDiscardTest(t, filepath.Join(root, "binary.dat")), binary) {
		t.Fatal("raw bytes or trailing spaces lost")
	}
	index, err := gitData(ctx, root, nil, "show", ":chosen.txt")
	if err != nil || !bytes.Equal(index, staged) {
		t.Fatal("staged state not restored", err, index)
	}
	if git(t, root, "diff", "--cached", "--name-only") == "" {
		t.Fatal("staged additions lost")
	}
	if string(readDiscardTest(t, filepath.Join(root, "loose.txt"))) != "untracked" {
		t.Fatal("untracked file changed")
	}
	if err = RestoreDiscardBackup(ctx, root, backup, info.ID); err != nil {
		t.Fatal("same baseline should remain recoverable", err)
	}
}
func TestDiscardRejectsStaleFilesHeadAndInvalidSelections(t *testing.T) {
	root := discardTestRepo(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(root, "chosen.txt"), []byte("edit"), 0600)
	p, err := PreviewDiscard(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, files := range [][]string{{}, {"../outside"}, {"other.txt"}, {".git/config"}, {"new.txt"}} {
		if _, err = DiscardFiles(ctx, p, files, ""); err == nil {
			t.Fatal("invalid selection admitted", files)
		}
	}
	os.WriteFile(filepath.Join(root, "chosen.txt"), []byte("later edit"), 0600)
	if _, err = DiscardFiles(ctx, p, []string{"chosen.txt"}, ""); err == nil {
		t.Fatal("stale preview overwrote later edit")
	}
	p, _ = PreviewDiscard(ctx, root)
	git(t, root, "commit", "--allow-empty", "-m", "new head")
	if _, err = DiscardFiles(ctx, p, []string{"chosen.txt"}, ""); err == nil {
		t.Fatal("changed HEAD was ignored")
	}
	if string(readDiscardTest(t, filepath.Join(root, "chosen.txt"))) != "later edit" {
		t.Fatal("rejected operation mutated file")
	}
}
func TestBackupFailureStopsDiscardAndRestoreProtectsNewEdits(t *testing.T) {
	root := discardTestRepo(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(root, "chosen.txt"), []byte("edit"), 0600)
	p, _ := PreviewDiscard(ctx, root)
	bad := filepath.Join(t.TempDir(), "not-directory")
	os.WriteFile(bad, []byte("x"), 0600)
	if _, err := DiscardFiles(ctx, p, []string{"chosen.txt"}, bad); err == nil {
		t.Fatal("backup write failure did not stop mutation")
	}
	if string(readDiscardTest(t, filepath.Join(root, "chosen.txt"))) != "edit" {
		t.Fatal("backup failure lost edit")
	}
	dir := t.TempDir()
	info, err := DiscardFiles(ctx, p, []string{"chosen.txt"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "chosen.txt"), []byte("new edit after discard"), 0600)
	if err = RestoreDiscardBackup(ctx, root, dir, info.ID); err == nil {
		t.Fatal("restore overwrote a newer edit")
	}
	if err = RestoreDiscardBackup(ctx, t.TempDir(), dir, info.ID); err == nil {
		t.Fatal("foreign repository restored")
	}
	if err = RestoreDiscardBackup(ctx, root, dir, "../escape"); err == nil {
		t.Fatal("unsafe backup ID accepted")
	}
	m, err := readManifest(dir, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	m.Original[0].Data = []byte("corrupt")
	writeManifest(dir, m)
	if err = RestoreDiscardBackup(ctx, root, dir, info.ID); err == nil {
		t.Fatal("corrupt original contents accepted")
	}
}
func TestDiscardDeletedDirectoryAndStagedDeletion(t *testing.T) {
	root := discardTestRepo(t)
	ctx := context.Background()
	os.RemoveAll(filepath.Join(root, "nested"))
	git(t, root, "rm", "chosen.txt")
	p, err := PreviewDiscard(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	info, err := DiscardFiles(ctx, p, []string{"nested/gone.txt", "chosen.txt"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = RestoreDiscardBackup(ctx, root, dir, info.ID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nested/gone.txt", "chosen.txt"} {
		if _, err = os.Stat(filepath.Join(root, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatal("deleted working file not restored as deleted", name)
		}
	}
	if git(t, root, "diff", "--cached", "--name-only") != "chosen.txt" {
		t.Fatal("staged deletion not restored")
	}
}

func TestDiscardIncludesCancelledOutStagedEditsAndLiteralNames(t *testing.T) {
	root := discardTestRepo(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(root, "chosen.txt"), []byte("staged change"), 0600)
	git(t, root, "add", "chosen.txt")
	os.WriteFile(filepath.Join(root, "chosen.txt"), []byte("base\n"), 0600)
	os.WriteFile(filepath.Join(root, "literal[1].txt"), []byte("literal edit"), 0600)
	p, err := PreviewDiscard(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 2 {
		t.Fatal("staged changes hidden by working tree", p)
	}
	dir := t.TempDir()
	info, err := DiscardFiles(ctx, p, []string{"chosen.txt", "literal[1].txt"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(readDiscardTest(t, filepath.Join(root, "literal[1].txt"))) != "base\n" {
		t.Fatal("literal path was interpreted as pattern")
	}
	if err = RestoreDiscardBackup(ctx, root, dir, info.ID); err != nil {
		t.Fatal(err)
	}
	if git(t, root, "show", ":chosen.txt") != "staged change" {
		t.Fatal("staged state lost")
	}
}

func TestDiscardDoesNotFollowParentSymlinksOrCancelledContext(t *testing.T) {
	root := discardTestRepo(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "gone.txt"), []byte("outside"), 0600)
	os.RemoveAll(filepath.Join(root, "nested"))
	if err := os.Symlink(outside, filepath.Join(root, "nested")); err != nil {
		t.Skip("symlink creation unavailable", err)
	}
	if _, err := PreviewDiscard(context.Background(), root); err == nil {
		t.Fatal("linked parent was followed")
	}
	if string(readDiscardTest(t, filepath.Join(outside, "gone.txt"))) != "outside" {
		t.Fatal("outside file mutated")
	}
}

func TestDiscardCancelledContextDoesNotMutate(t *testing.T) {
	root := discardTestRepo(t)
	os.WriteFile(filepath.Join(root, "chosen.txt"), []byte("edit"), 0600)
	p, _ := PreviewDiscard(context.Background(), root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DiscardFiles(ctx, p, []string{"chosen.txt"}, t.TempDir()); err == nil {
		t.Fatal("cancelled mutation admitted")
	}
	if string(readDiscardTest(t, filepath.Join(root, "chosen.txt"))) != "edit" {
		t.Fatal("cancelled discard mutated file")
	}
}

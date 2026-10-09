package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func discardIndex(t *testing.T, root, name string) string {
	t.Helper()
	data, err := exec.Command("git", "-C", root, "show", ":"+name).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

func discardServiceFixture(t *testing.T) (*GitService, string) {
	t.Helper()
	root := t.TempDir()
	testGit(t, root, "init", "-b", "main")
	testGit(t, root, "config", "user.name", "Test")
	testGit(t, root, "config", "user.email", "test@example.invalid")
	testGit(t, root, "config", "core.autocrlf", "false")
	for _, name := range []string{"chosen.txt", "other.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("original\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "-m", "base")
	s := NewGitService()
	s.backupDir = filepath.Join(t.TempDir(), "backups")
	if err := s.StartScan(root); err != nil {
		t.Fatal(err)
	}
	awaitTask(t, s)
	return s, root
}

func assertDiscardBytes(t *testing.T, root, name, want string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil || string(b) != want {
		t.Fatalf("%s: %q %v", name, b, err)
	}
}

func TestDiscardServiceSelectionBackupAndRestartRestore(t *testing.T) {
	s, root := discardServiceFixture(t)
	os.WriteFile(filepath.Join(root, "chosen.txt"), []byte("staged\n"), 0600)
	testGit(t, root, "add", "chosen.txt")
	os.WriteFile(filepath.Join(root, "chosen.txt"), []byte("working  \r\n"), 0600)
	os.WriteFile(filepath.Join(root, "other.txt"), []byte("other edit"), 0600)
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("new"), 0600)
	testGit(t, root, "add", "new.txt")
	p, err := s.GetDiscardPreview(root)
	if err != nil || len(p.Files) != 2 {
		t.Fatal(p, err)
	}
	if err = s.StartDiscard(p, []string{"chosen.txt"}, true, false); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	if err = s.StartDiscard(p, nil, true, true); err == nil {
		t.Fatal("empty selection accepted")
	}
	if err = s.StartDiscard(p, []string{"chosen.txt"}, true, true); err != nil {
		t.Fatal(err)
	}
	state := awaitTask(t, s)
	if state.Succeeded != 1 || state.Kind != "discard" {
		t.Fatal(state)
	}
	assertDiscardBytes(t, root, "chosen.txt", "original\n")
	assertDiscardBytes(t, root, "other.txt", "other edit")
	assertDiscardBytes(t, root, "new.txt", "new")
	backups, err := s.GetDiscardBackups(root)
	if err != nil || len(backups) != 1 || !backups[0].Ready || len(backups[0].Files) != 1 {
		t.Fatal(backups, err)
	}
	found := false
	for _, l := range state.Logs {
		found = found || strings.Contains(l.Message, backups[0].ID)
	}
	if !found {
		t.Fatal("backup ID absent from task log")
	}
	// A fresh service uses the saved on-disk backup; no session-only recovery cache.
	restarted := NewGitService()
	restarted.backupDir = s.backupDir
	if err = restarted.StartScan(root); err != nil {
		t.Fatal(err)
	}
	awaitTask(t, restarted)
	if err = restarted.StartRestoreDiscard(root, backups[0].ID, false); err == nil {
		t.Fatal("restore without confirmation")
	}
	if err = restarted.StartRestoreDiscard(root, backups[0].ID, true); err != nil {
		t.Fatal(err)
	}
	state = awaitTask(t, restarted)
	if state.Succeeded != 1 || state.Kind != "restore-discard" {
		t.Fatal(state)
	}
	assertDiscardBytes(t, root, "chosen.txt", "working  \r\n")
	if discardIndex(t, root, "chosen.txt") != "staged" {
		t.Fatal("index content lost")
	}
	assertDiscardBytes(t, root, "other.txt", "other edit")
	if discardIndex(t, root, "new.txt") != "new" {
		t.Fatal("staged new file lost")
	}
}

func TestDiscardServiceFailureAndReadLeaseGuards(t *testing.T) {
	s, root := discardServiceFixture(t)
	os.WriteFile(filepath.Join(root, "chosen.txt"), []byte("edit"), 0600)
	p, err := s.GetDiscardPreview(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetDiscardBackups(t.TempDir()); err == nil {
		t.Fatal("unscanned backup scope accepted")
	}
	_, _, release, err := s.readRepository(root, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.StartDiscard(p, []string{"chosen.txt"}, false, true); err == nil {
		t.Fatal("read lease did not block mutation")
	}
	release()
	os.WriteFile(filepath.Join(root, "chosen.txt"), []byte("changed after preview"), 0600)
	if err = s.StartDiscard(p, []string{"chosen.txt"}, false, true); err != nil {
		t.Fatal(err)
	}
	if state := awaitTask(t, s); state.Failed != 1 {
		t.Fatal("stale preview not rejected", state)
	}
	assertDiscardBytes(t, root, "chosen.txt", "changed after preview")
	p, err = s.GetDiscardPreview(root)
	if err != nil {
		t.Fatal(err)
	}
	s.backupDir = filepath.Join(t.TempDir(), "blocked")
	os.WriteFile(s.backupDir, []byte("not a directory"), 0600)
	if err = s.StartDiscard(p, []string{"chosen.txt"}, true, true); err != nil {
		t.Fatal(err)
	}
	if state := awaitTask(t, s); state.Failed != 1 {
		t.Fatal("backup failure not reported", state)
	}
	assertDiscardBytes(t, root, "chosen.txt", "changed after preview")
}

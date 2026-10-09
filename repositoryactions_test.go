package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestActionScopeConfirmationAndBusyGuard(t *testing.T) {
	root := t.TempDir()
	testGit(t, root, "init", "-b", "main")
	testGit(t, root, "config", "user.name", "Test")
	testGit(t, root, "config", "user.email", "test@example.invalid")
	file := filepath.Join(root, "file.txt")
	os.WriteFile(file, []byte("original"), 0644)
	testGit(t, root, "add", "file.txt")
	testGit(t, root, "commit", "-m", "first")
	s := NewGitService()
	if err := s.StartScan(root); err != nil {
		t.Fatal(err)
	}
	awaitTask(t, s)
	os.WriteFile(file, []byte("changed"), 0644)
	if err := s.StartAction(root, "discard", "", false); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	if err := s.StartAction(t.TempDir(), "discard", "", true); err == nil {
		t.Fatal("unscanned path accepted")
	}
	s.mu.Lock()
	s.state.Busy = true
	s.mu.Unlock()
	if err := s.StartAction(root, "discard", "", true); err == nil {
		t.Fatal("concurrent operation accepted")
	}
	s.mu.Lock()
	s.state.Busy = false
	s.mu.Unlock()
	data, _ := os.ReadFile(file)
	if string(data) != "changed" {
		t.Fatal("rejected operations changed file")
	}
	if err := s.StartAction(root, "discard", "", true); err != nil {
		t.Fatal(err)
	}
	state := awaitTask(t, s)
	if state.Phase != "done" || state.Succeeded != 1 {
		t.Fatalf("operation failed: %+v", state)
	}
	data, _ = os.ReadFile(file)
	if string(data) != "original" {
		t.Fatal("confirmed discard failed")
	}
}

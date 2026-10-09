package main

import (
	"path/filepath"
	"testing"

	"crab.gitsync/internal/scansettings"
)

func TestSavedScanListUsesSnapshotAndDeduplicatesRepositories(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	testGit(t, root, "init", "-b", "main", repo)
	s := NewGitService()
	var err error
	s.scanLists, err = scansettings.New(filepath.Join(t.TempDir(), "scans.json"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.SaveScanList(scansettings.List{Name: "work", Roots: []string{root, repo, filepath.Join(root, "missing")}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.StartScanList(list.ID); err != nil {
		t.Fatal(err)
	}
	changed := list
	changed.Roots = []string{t.TempDir()}
	if _, err = s.SaveScanList(changed); err != nil {
		t.Fatal(err)
	}
	state := awaitTask(t, s)
	if len(state.Repositories) != 1 || state.Total != 1 || state.Succeeded != 1 || state.Failed != 1 || state.ScanListID != list.ID {
		t.Fatal("scan list snapshot failed", state)
	}
	if err = s.StartScanList(list.ID); err != nil {
		t.Fatal(err)
	}
	if state = awaitTask(t, s); len(state.Repositories) != 0 {
		t.Fatal("saved change not applied to next scan")
	}
	if err = s.RemoveScanList(list.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.StartScanList(list.ID); err == nil {
		t.Fatal("deleted list still usable")
	}
}

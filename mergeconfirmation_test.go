package main

import (
	"path/filepath"
	"testing"

	"crab.gitsync/internal/gitengine"
)

func TestMergeAPIRequiresPreviewConfirmationAndScannedScope(t *testing.T) {
	s := NewGitService()
	path := filepath.Join(t.TempDir(), "repository")
	p := gitengine.MergePreview{Path: path, Action: "merge", Branch: "main", Target: "refs/heads/feature"}
	if err := s.StartMerge(p, "ff-only", false); err == nil {
		t.Fatal("unconfirmed merge accepted")
	}
	if err := s.StartMerge(p, "merge", true); err == nil {
		t.Fatal("out-of-scan merge accepted")
	}
	p.Action = "discard"
	if err := s.StartMerge(p, "merge", true); err == nil {
		t.Fatal("merge API accepted discard")
	}
	if err := s.StartAction(path, "merge", p.Target, true); err == nil {
		t.Fatal("legacy API bypassed preview")
	}
	if s.GetState().Busy {
		t.Fatal("rejected operation started task")
	}
}

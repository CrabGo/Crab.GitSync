package main

import (
	"context"
	"crab.gitsync/internal/gitengine"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubShortcutURLs(t *testing.T) {
	for address, want := range map[string]string{
		"https://user:secret@github.com/owner/repo.git?token=hidden": "https://github.com/owner/repo",
		"git@github.com:owner/repo.git":                              "https://github.com/owner/repo",
		"ssh://git@github.com/owner/repo.git":                        "https://github.com/owner/repo",
		"https://github.com.evil.test/owner/repo":                    "",
		"javascript:alert(1)":                                        "",
		"https://github.com/owner/../repo":                           "",
		"https://github.com/owner/repo/issues":                       "",
	} {
		if got := githubPage(address); got != want {
			t.Fatalf("%s => %s", address, got)
		}
	}
}
func TestRepositoryLinksScopeFreshnessAndRedaction(t *testing.T) {
	repo := t.TempDir()
	testGit(t, repo, "init", "-b", "main")
	testGit(t, repo, "remote", "add", "origin", "https://user:secret@github.com/o/r.git?token=hidden")
	s := NewGitService()
	s.StartScan(repo)
	awaitTask(t, s)
	links, err := s.GetRepositoryLinks(repo)
	if err != nil || len(links) != 1 || strings.Contains(links[0].Address, "secret") || strings.Contains(links[0].Address, "hidden") || links[0].GitHubURL != "https://github.com/o/r" {
		t.Fatal(links, err)
	}
	testGit(t, repo, "remote", "set-url", "origin", "git@github.com:new/project.git")
	links, err = s.GetRepositoryLinks(repo)
	if err != nil || links[0].GitHubURL != "https://github.com/new/project" {
		t.Fatal("stale shortcut data", links, err)
	}
	if _, err = s.GetRepositoryLinks(t.TempDir()); err == nil {
		t.Fatal("unscanned repo accepted")
	}
	if err = s.OpenRepository(t.TempDir(), "folder"); err == nil {
		t.Fatal("unscanned folder accepted")
	}
	if err = s.OpenRepository(repo, "unknown"); err == nil {
		t.Fatal("unknown action accepted")
	}
	s.state.Busy = true
	if _, err = s.GetRepositoryLinks(repo); err == nil {
		t.Fatal("busy reader admitted")
	}
	s.state.Busy = false
	os.RemoveAll(filepath.Join(repo, ".git"))
	if _, err = s.GetRepositoryLinks(repo); err == nil {
		t.Fatal("deleted repository accepted")
	}
}
func TestSelectedRemoteFetchAndRetryProvenance(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "client")
	remote := filepath.Join(root, "origin.git")
	testGit(t, root, "init", "-b", "main", repo)
	testGit(t, root, "init", "--bare", remote)
	testGit(t, repo, "remote", "add", "origin", remote)
	testGit(t, repo, "remote", "add", "other", filepath.Join(root, "missing.git"))
	s := NewGitService()
	s.StartScan(repo)
	awaitTask(t, s)
	if err := s.StartFetchRemote(repo, "origin"); err != nil {
		t.Fatal(err)
	}
	state := awaitTask(t, s)
	if state.Succeeded != 1 || state.Results[0].Remote != "origin" || state.Repositories[0].LastSuccessfulFetch != "" {
		t.Fatal("partial fetch should not claim all remotes fresh", state)
	}
	if err := s.StartFetchRemote(repo, "missing"); err == nil {
		t.Fatal("unknown remote accepted")
	}
	s.StartFetch([]string{repo})
	state = awaitTask(t, s)
	if state.Failed != 1 {
		t.Fatal("default fetch stopped fetching all remotes", state)
	}
	s.fetchRemoteRepository = func(context.Context, string, string) error { return fmt.Errorf("connection refused") }
	s.StartFetchRemote(repo, "origin")
	state = awaitTask(t, s)
	called := ""
	s.fetchRemoteRepository = func(_ context.Context, _ string, remote string) error { called = remote; return nil }
	if err := s.RetryFailed(state.TaskID); err != nil {
		t.Fatal(err)
	}
	state = awaitTask(t, s)
	if called != "origin" || state.Results[0].Remote != "origin" {
		t.Fatal("retry widened remote scope", state, called)
	}
	s.fetchRemoteRepository = gitengine.FetchRemote
	testGit(t, repo, "remote", "remove", "origin")
	if err := s.StartFetchRemote(repo, "origin"); err != nil {
		t.Fatal(err)
	}
	state = awaitTask(t, s)
	if state.Failed != 1 {
		t.Fatal("removed remote was not revalidated", state)
	}
}

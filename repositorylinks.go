package main

import (
	"context"
	"crab.gitsync/internal/gitengine"
	"crab.gitsync/internal/scansettings"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type RepositoryLink struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	GitHubURL string `json:"githubURL"`
}

func githubPage(address string) string {
	path := ""
	if strings.HasPrefix(address, "git@github.com:") {
		path = strings.TrimPrefix(address, "git@github.com:")
	} else {
		u, err := url.Parse(address)
		if err != nil || u.Hostname() != "github.com" || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "ssh") {
			return ""
		}
		path = strings.TrimPrefix(u.Path, "/")
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		return ""
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return ""
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
				return ""
			}
		}
	}
	return "https://github.com/" + path
}

func (s *GitService) GetRepositoryLinks(path string) ([]RepositoryLink, error) {
	repo, ctx, release, err := s.readRepository(path, 20*time.Second)
	if err != nil {
		return nil, err
	}
	defer release()
	links := []RepositoryLink{}
	err = s.scheduler.Do(ctx, repositoryKeys(repo.Path), func(ctx context.Context) error {
		fresh, err := s.inspectRepository(ctx, repo.Path)
		if err != nil {
			return err
		}
		for _, remote := range fresh.Remotes {
			links = append(links, RepositoryLink{Name: remote.Name, Address: gitengine.Redact(remote.URL), GitHubURL: githubPage(remote.URL)})
		}
		return nil
	})
	return links, err
}
func (s *GitService) OpenRepository(path, kind string) error {
	repo, ctx, release, err := s.readRepository(path, 10*time.Second)
	if err != nil {
		return err
	}
	defer release()
	return s.scheduler.Do(ctx, repositoryKeys(repo.Path), func(context.Context) error {
		info, err := os.Stat(repo.Path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("仓库目录不可访问，请重新扫描")
		}
		return openRepositoryDirectory(repo.Path, kind)
	})
}
func (s *GitService) StartFetchRemote(path, remote string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	repo, err := s.scannedRepository(path)
	if err != nil {
		return err
	}
	if remote != "" {
		found := false
		for _, r := range repo.Remotes {
			if r.Name == remote {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("远端不在扫描结果中，请重新扫描")
		}
	}
	return s.startFetchSelectionsLocked([]string{repo.Path}, nil, "", map[string]string{scansettings.PathKey(repo.Path): remote})
}

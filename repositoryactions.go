package main

import (
	"context"
	"crab.gitsync/internal/gitengine"
	"fmt"
	"time"
)

var actionLabels = map[string]string{"fetch": "拉取", "merge": "合并", "discard": "撤销本地修改", "pull-merge": "拉取并合并", "abort-merge": "中止合并"}

func (s *GitService) scannedRepository(path string) (gitengine.Repository, error) {
	for _, repo := range s.state.Repositories {
		if repo.Path == path {
			return repo, nil
		}
	}
	return gitengine.Repository{}, fmt.Errorf("仓库不在当前扫描结果中")
}

func (s *GitService) GetBranches(path string) ([]string, error) {
	s.mu.Lock()
	_, err := s.scannedRepository(path)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return gitengine.Branches(ctx, path)
}

// StartAction executes only an explicit single-repository action selected in the UI.
func (s *GitService) StartAction(path, action, target string, confirmed bool) error {
	if action == "fetch" {
		return s.StartFetch([]string{path})
	}
	label, ok := actionLabels[action]
	if !ok {
		return fmt.Errorf("未知仓库操作")
	}
	if !confirmed {
		return fmt.Errorf("请先确认操作目标与影响")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	repo, err := s.scannedRepository(path)
	if err != nil {
		return err
	}
	ctx, err := s.begin(action, s.state.Root, "operating")
	if err != nil {
		return err
	}
	s.state.Total = 1
	s.state.Current = path
	s.updateRepoLocked(path, "operating", "", nil)
	s.logLocked("info", fmt.Sprintf("%s：%s · 当前分支 %s · 目标 %s", label, repo.Path, repo.Branch, target))
	go func() {
		output, actionErr := gitengine.Action(ctx, path, action, target, repo.Branch)
		// Refresh even after failure/cancellation: conflicts and partial changes must be visible.
		refreshCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		refreshed, refreshErr := gitengine.Inspect(refreshCtx, path)
		cancel()
		s.mu.Lock()
		var snapshot *gitengine.Repository
		if refreshErr == nil {
			snapshot = &refreshed
		} else {
			s.logLocked("warn", "刷新仓库状态失败："+refreshErr.Error())
		}
		s.state.Completed = 1
		if actionErr != nil {
			s.state.Failed = 1
			s.updateRepoLocked(path, "error", actionErr.Error(), snapshot)
			s.logLocked("error", label+"失败："+actionErr.Error())
		} else {
			s.state.Succeeded = 1
			s.updateRepoLocked(path, "action-success", "", snapshot)
			s.logLocked("success", label+"完成："+output)
		}
		s.mu.Unlock()
		s.finish(ctx, actionErr)
	}()
	return nil
}

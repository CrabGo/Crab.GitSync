package main

import (
	"context"
	"crab.gitsync/internal/gitengine"
	"crab.gitsync/internal/taskresult"
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

// PreviewMerge is read-only and validates the current scanned scope.
func (s *GitService) PreviewMerge(path, action, target string) (gitengine.MergePreview, error) {
	s.mu.Lock()
	repo, err := s.scannedRepository(path)
	if err == nil && (s.state.Busy || s.restarting) {
		err = fmt.Errorf("已有任务正在运行，请稍后预览")
	}
	s.mu.Unlock()
	if err != nil {
		return gitengine.MergePreview{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	preview, err := gitengine.PreviewMerge(ctx, path, action, target)
	if err == nil && preview.Branch != repo.Branch {
		err = fmt.Errorf("当前分支已变化，请重新扫描")
	}
	return preview, err
}

func (s *GitService) StartMerge(preview gitengine.MergePreview, strategy string, confirmed bool) error {
	if !confirmed {
		return fmt.Errorf("请先确认合并目标与影响")
	}
	if preview.Action != "merge" && preview.Action != "pull-merge" {
		return fmt.Errorf("未知合并操作")
	}
	if strategy != "ff-only" && strategy != "merge" {
		return fmt.Errorf("未知合并策略")
	}
	return s.startAction(preview.Path, preview.Action, preview.Target, true, &preview, strategy)
}

// StartAction executes only an explicit single-repository action selected in the UI.
func (s *GitService) StartAction(path, action, target string, confirmed bool) error {
	if action == "merge" || action == "pull-merge" {
		return fmt.Errorf("请先预览合并关系并确认策略")
	}
	return s.startAction(path, action, target, confirmed, nil, "")
}

func (s *GitService) startAction(path, action, target string, confirmed bool, preview *gitengine.MergePreview, strategy string) error {
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
		started := time.Now()
		var output string
		var actionErr error
		if preview != nil {
			if preview.Branch != repo.Branch {
				actionErr = fmt.Errorf("当前分支已变化，请重新预览")
			} else {
				var fetched bool
				output, fetched, actionErr = gitengine.ExecuteMerge(ctx, *preview, strategy)
				if fetched {
					s.mu.Lock()
					s.fetchTimes[path] = time.Now().Format(time.RFC3339Nano)
					s.mu.Unlock()
				}
			}
		} else {
			output, actionErr = gitengine.Action(ctx, path, action, target, repo.Branch)
		}
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
		result := taskresult.Result{TaskID: s.state.TaskID, Kind: action, Path: path, Branch: repo.Branch, Stage: "action", Status: "success", Attempts: 1, StartedAt: started.Format(time.RFC3339Nano), FinishedAt: time.Now().Format(time.RFC3339Nano), DurationMS: time.Since(started).Milliseconds(), Failure: taskresult.Wrap(actionErr, "action")}
		if actionErr == nil && refreshErr != nil {
			result.Stage = "refresh"
			result.Status = "error"
			result.Failure = taskresult.Wrap(refreshErr, "refresh")
		}
		if actionErr != nil {
			result.Status = "error"
		}
		if ctx.Err() != nil {
			result.Status = "cancelled"
			result.Failure = taskresult.Wrap(ctx.Err(), "action")
		}
		s.recordResultLocked(result)
		if ctx.Err() != nil {
			s.updateRepoLocked(path, "cancelled", "任务已取消", snapshot)
			s.logLocked("warn", label+"已取消，操作现场已保留")
		} else if actionErr != nil {
			s.state.Failed = 1
			s.updateRepoLocked(path, "error", actionErr.Error(), snapshot)
			s.logLocked("error", label+"失败："+actionErr.Error())
		} else {
			if refreshErr != nil {
				s.state.Failed = 1
				s.updateRepoLocked(path, "refresh-error", taskresult.Wrap(refreshErr, "refresh").Error(), nil)
			} else {
				s.state.Succeeded = 1
				s.updateRepoLocked(path, "action-success", "", snapshot)
			}
			s.logLocked("success", label+"完成："+output)
		}
		s.mu.Unlock()
		s.finish(ctx, actionErr)
	}()
	return nil
}

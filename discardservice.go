package main

import (
	"context"
	"crab.gitsync/internal/gitengine"
	"fmt"
	"time"
)

func (s *GitService) GetDiscardPreview(path string) (gitengine.DiscardPreview, error) {
	repo, ctx, release, err := s.readRepository(path, 60*time.Second)
	if err != nil {
		return gitengine.DiscardPreview{}, err
	}
	defer release()
	var p gitengine.DiscardPreview
	err = s.scheduler.Do(ctx, repositoryKeys(repo.Path), func(ctx context.Context) error {
		var e error
		p, e = gitengine.PreviewDiscard(ctx, repo.Path)
		return e
	})
	if err == nil && p.Branch != repo.Branch {
		err = fmt.Errorf("当前分支已变化，请重新扫描")
	}
	return p, err
}

func (s *GitService) StartDiscard(preview gitengine.DiscardPreview, names []string, backup, confirmed bool) error {
	if !confirmed {
		return fmt.Errorf("请先确认所选文件的撤销影响")
	}
	if len(names) == 0 {
		return fmt.Errorf("请至少选择一个已跟踪文件")
	}
	s.mu.Lock()
	repo, err := s.scannedRepository(preview.Path)
	dir := s.backupDir
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if repo.Branch != preview.Branch {
		return fmt.Errorf("当前分支已变化，请重新预览")
	}
	if backup && dir == "" {
		return fmt.Errorf("未配置本地备份目录，未执行撤销")
	}
	if !backup {
		dir = ""
	}
	preview.Path = repo.Path
	preview.Files = append([]gitengine.DiscardFile{}, preview.Files...)
	names = append([]string{}, names...)
	return s.startRepositoryOperation(repo.Path, "discard", "所选文件", true, nil, "", func(ctx context.Context) (string, error) {
		info, e := gitengine.DiscardFiles(ctx, preview, names, dir)
		if info.ID != "" {
			s.mu.Lock()
			if info.Ready {
				s.logLocked("info", "本地撤销备份已就绪，可在仓库菜单恢复："+info.ID)
			} else {
				s.logLocked("warn", "原始备份已保存，但自动恢复尚未就绪："+info.ID)
			}
			s.mu.Unlock()
		}
		return fmt.Sprintf("已选择 %d 个文件", len(names)), e
	})
}

func (s *GitService) GetDiscardBackups(path string) ([]gitengine.DiscardBackup, error) {
	repo, ctx, release, err := s.readRepository(path, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer release()
	var items []gitengine.DiscardBackup
	err = s.scheduler.Do(ctx, repositoryKeys(repo.Path), func(context.Context) error {
		var e error
		items, e = gitengine.ListDiscardBackups(s.backupDir, repo.Path)
		return e
	})
	return items, err
}

func (s *GitService) StartRestoreDiscard(path, id string, confirmed bool) error {
	if !confirmed {
		return fmt.Errorf("请先确认备份及恢复文件范围")
	}
	s.mu.Lock()
	repo, err := s.scannedRepository(path)
	dir := s.backupDir
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("未配置本地备份目录")
	}
	return s.startRepositoryOperation(repo.Path, "restore-discard", id, true, nil, "", func(ctx context.Context) (string, error) {
		// A branch change since scanning also requires a fresh scan before restoration.
		fresh, e := gitengine.Inspect(ctx, repo.Path)
		if e != nil {
			return "", e
		}
		if fresh.Branch != repo.Branch {
			return "", fmt.Errorf("当前分支已变化，请重新扫描")
		}
		e = gitengine.RestoreDiscardBackup(ctx, repo.Path, dir, id)
		return "本地备份 " + id, e
	})
}

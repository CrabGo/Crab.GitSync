package main

import (
	"crab.gitsync/internal/tasksettings"
	"fmt"
)

func (s *GitService) GetTaskConfig() (tasksettings.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.taskSettings == nil {
		return tasksettings.Default(), nil
	}
	return s.taskSettings.Get(), s.taskSettingsError
}
func (s *GitService) SaveTaskConfig(config tasksettings.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.taskSettings == nil {
		return fmt.Errorf("任务设置存储尚未初始化")
	}
	if err := s.taskSettings.Save(config); err != nil {
		return err
	}
	s.taskSettingsError = nil
	if s.history != nil {
		saved := s.taskSettings.Get()
		if err := s.history.Prune(saved.HistoryTasks, saved.HistoryDays); err != nil {
			s.logLocked("warn", "历史清理未写入磁盘："+err.Error())
		}
	}
	return nil
}

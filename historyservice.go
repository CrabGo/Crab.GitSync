package main

import (
	"crab.gitsync/internal/taskhistory"
	"crab.gitsync/internal/taskresult"
	"crab.gitsync/internal/tasksettings"
	"fmt"
)

func (s *GitService) ListTaskHistory() []taskhistory.Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.history == nil {
		return []taskhistory.Summary{}
	}
	return s.history.List()
}
func (s *GitService) GetTaskHistory(id string) (taskhistory.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.history == nil {
		return taskhistory.Record{}, fmt.Errorf("任务历史存储尚未初始化")
	}
	return s.history.Get(id)
}
func (s *GitService) GetHistoryWarning() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.history == nil {
		return ""
	}
	return s.history.Warning()
}
func (s *GitService) fetchResultsLocked(id string) ([]taskresult.Result, bool) {
	if s.history != nil {
		if record, err := s.history.Get(id); err == nil && record.Kind == "fetch" {
			return record.Results, true
		}
		return nil, false
	}
	if results, ok := s.fetchHistory[id]; ok {
		return results, true
	}
	return nil, false
}
func (s *GitService) persistCompletedLocked() {
	if s.history == nil {
		return
	}
	state := s.state
	record := taskhistory.Record{Summary: taskhistory.Summary{ID: state.TaskID, SourceTaskID: state.SourceTaskID, Kind: state.Kind, Root: state.Root, ScanListID: state.ScanListID, Phase: state.Phase, StartedAt: state.StartedAt, FinishedAt: state.FinishedAt, Total: state.Total, Completed: state.Completed, Succeeded: state.Succeeded, Failed: state.Failed, Skipped: state.Skipped}, Results: cloneResults(state.Results), Logs: []taskhistory.Log{}}
	for _, entry := range state.Logs {
		if entry.ID >= s.firstTaskLogID {
			record.Logs = append(record.Logs, taskhistory.Log{Time: entry.Time, Level: entry.Level, Message: entry.Message})
		}
	}
	config := tasksettings.Default()
	if s.taskSettings != nil {
		config = s.taskSettings.Get()
	}
	if err := s.history.Add(record, s.fetchTimes, config.HistoryTasks, config.HistoryDays); err != nil {
		s.logLocked("warn", "历史保存失败，Git 任务结果不受影响："+err.Error())
	}
}

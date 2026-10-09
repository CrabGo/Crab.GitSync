package main

import (
	"context"
	"crab.gitsync/internal/gitengine"
	"crab.gitsync/internal/scansettings"
	"crab.gitsync/internal/taskqueue"
	"crab.gitsync/internal/taskresult"
	"crab.gitsync/internal/tasksettings"
	"github.com/wailsapp/wails/v3/pkg/application"
	"sort"
	"strings"
	"time"
)

type ScheduleState struct {
	ListID  string `json:"listID"`
	NextRun string `json:"nextRun"`
	LastRun string `json:"lastRun"`
	Status  string `json:"status"`
}

func (s *GitService) GetSchedules() []ScheduleState {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []ScheduleState{}
	if s.scanLists != nil {
		for _, list := range s.scanLists.Get() {
			if state, ok := s.schedules[list.ID]; ok {
				result = append(result, state)
			}
		}
	}
	return result
}
func (s *GitService) resetScheduleLocked(list scansettings.List) {
	if s.schedules == nil {
		s.schedules = map[string]ScheduleState{}
	}
	state := ScheduleState{ListID: list.ID, Status: "disabled"}
	if list.Scheduled {
		state.NextRun = time.Now().Add(time.Duration(list.IntervalMinutes) * time.Minute).Format(time.RFC3339Nano)
		state.Status = "waiting"
	}
	s.schedules[list.ID] = state
}

//wails:ignore
func (s *GitService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.startSchedules(ctx)
	return nil
}

//wails:ignore
func (s *GitService) ServiceShutdown() error {
	s.shutdownSchedules()
	s.Cancel()
	return nil
}

func (s *GitService) startSchedules(parent context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopSchedules != nil || s.schedulesStopped {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.stopSchedules = cancel
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		if ctx.Err() != nil {
			return
		}
		s.scheduleTick(time.Now())
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				s.scheduleTick(time.Now())
			}
		}
	}()
}
func (s *GitService) shutdownSchedules() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopSchedules != nil {
		s.stopSchedules()
	}
	s.schedulesStopped = true
	s.scheduler.Cancel()
}

// Wall-clock deadlines advance from now, never from an overdue deadline. No backlog is queued.
func (s *GitService) scheduleTick(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scanLists == nil || s.schedulesStopped {
		return
	}
	if s.schedules == nil {
		s.schedules = map[string]ScheduleState{}
	}
	lists := s.scanLists.Get()
	// Prefer never-run/oldest-run lists when deadlines coincide.
	sort.SliceStable(lists, func(i, j int) bool { return s.schedules[lists[i].ID].LastRun < s.schedules[lists[j].ID].LastRun })
	for _, list := range lists {
		state, ok := s.schedules[list.ID]
		if !list.Scheduled {
			s.schedules[list.ID] = ScheduleState{ListID: list.ID, Status: "disabled"}
			continue
		}
		if !ok || state.NextRun == "" {
			state = ScheduleState{ListID: list.ID, Status: "waiting", NextRun: now.Add(time.Duration(list.IntervalMinutes) * time.Minute).Format(time.RFC3339Nano)}
			s.schedules[list.ID] = state
			continue
		}
		due, _ := time.Parse(time.RFC3339Nano, state.NextRun)
		if now.Before(due) {
			continue
		}
		state.NextRun = now.Add(time.Duration(list.IntervalMinutes) * time.Minute).Format(time.RFC3339Nano)
		state.Status = "busy-skip"
		ctx, err := s.begin("fetch", strings.Join(list.Roots, "；"), "discovering")
		if err != nil {
			if strings.Contains(err.Error(), "未找到 Git") {
				state.Status = "error"
				s.logLocked("error", "定时获取未启动："+err.Error())
				if s.notify != nil {
					go s.notify("定时获取无法启动", err.Error(), "logs")
				}
			}
			s.schedules[list.ID] = state
			continue
		}
		state.Status = "running"
		state.LastRun = now.Format(time.RFC3339Nano)
		s.schedules[list.ID] = state
		s.state.Automatic = true
		s.state.ScanListID = list.ID
		s.state.Repositories = nil
		s.scheduledNew = 0
		s.logLocked("info", "定时获取列表「"+list.Name+"」，仅 fetch，不修改工作区")
		s.launch(ctx, nil, func(ctx context.Context) error { return s.scheduledFetch(ctx, list) })
	}
}

func (s *GitService) scheduledFetch(ctx context.Context, list scansettings.List) error {
	paths, err := gitengine.DiscoverMany(ctx, list.Roots, list.Excludes, func(count int, path string) {
		s.mu.Lock()
		s.state.Visited = count
		s.state.Current = path
		s.mu.Unlock()
	}, func(message string) { s.mu.Lock(); s.state.Failed++; s.logLocked("warn", message); s.mu.Unlock() })
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.state.Total = len(paths)
	s.state.Phase = "fetching"
	for _, path := range paths {
		s.state.Results = append(s.state.Results, taskresult.Result{TaskID: s.state.TaskID, Kind: "fetch", Path: path, Stage: "queued", Status: "queued"})
	}
	s.mu.Unlock()
	limit, _ := ctx.Value(concurrencyKey{}).(int)
	if limit == 0 {
		limit = tasksettings.Default().Concurrency
	}
	jobs := []taskqueue.Job{}
	for _, path := range paths {
		jobs = append(jobs, taskqueue.Job{Keys: repositoryKeys(path), Run: func(ctx context.Context) error {
			started := time.Now()
			repo, err := s.inspectRepository(ctx, path)
			if err != nil {
				s.mu.Lock()
				defer s.mu.Unlock()
				status := "error"
				if ctx.Err() != nil {
					status = "cancelled"
				} else {
					s.state.Failed++
					s.state.Completed++
				}
				s.recordResultLocked(taskresult.Result{TaskID: s.state.TaskID, Kind: "fetch", Path: path, Stage: "inspect", Status: status, Attempts: 1, StartedAt: started.Format(time.RFC3339Nano), FinishedAt: time.Now().Format(time.RFC3339Nano), Failure: taskresult.Wrap(err, "inspect")})
				s.logLocked("error", err.Error())
				return nil
			}
			s.mu.Lock()
			repo.LastSuccessfulFetch = s.fetchTimes[scansettings.PathKey(path)]
			s.state.Repositories = append(s.state.Repositories, repo)
			s.mu.Unlock()
			before, tipsErr := gitengine.RemoteTips(ctx, path)
			if err := s.fetchOne(ctx, repo, false); err != nil {
				return err
			}
			s.mu.Lock()
			success := false
			for _, r := range s.state.Results {
				if r.Path == path && r.Status == "success" {
					success = true
				}
			}
			s.mu.Unlock()
			if success && tipsErr == nil {
				if changed, _ := gitengine.HasNewRemoteCommits(ctx, path, before); changed {
					s.mu.Lock()
					s.scheduledNew++
					s.mu.Unlock()
				}
			}
			return nil
		}})
	}
	_, err = s.scheduler.Run(ctx, jobs, limit)
	return err
}

func scheduleFinished(phase string, failed int) string {
	if phase == "cancelled" {
		return "cancelled"
	}
	if phase == "error" || failed > 0 {
		return "error"
	}
	return "done"
}

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"crab.gitsync/internal/gitengine"
	"crab.gitsync/internal/taskresult"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type LogEntry struct {
	ID      int    `json:"id"`
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type State struct {
	TaskID       string                 `json:"taskID"`
	SourceTaskID string                 `json:"sourceTaskID"`
	Results      []taskresult.Result    `json:"results"`
	Busy         bool                   `json:"busy"`
	Kind         string                 `json:"kind"`
	Phase        string                 `json:"phase"`
	Root         string                 `json:"root"`
	Current      string                 `json:"current"`
	Visited      int                    `json:"visited"`
	Completed    int                    `json:"completed"`
	Total        int                    `json:"total"`
	Succeeded    int                    `json:"succeeded"`
	Failed       int                    `json:"failed"`
	Skipped      int                    `json:"skipped"`
	StartedAt    string                 `json:"startedAt"`
	FinishedAt   string                 `json:"finishedAt"`
	Repositories []gitengine.Repository `json:"repositories"`
	Logs         []LogEntry             `json:"logs"`
}

// GitService owns one cancellable task and exposes immutable snapshots to the UI.
type GitService struct {
	app               *application.App
	mu                sync.Mutex
	state             State
	cancel            context.CancelFunc
	logID             int
	restarting        bool
	notify            func(string, string, string)
	proxyURL          func() string
	fetchRepository   func(context.Context, string) error
	inspectRepository func(context.Context, string) (gitengine.Repository, error)
	fetchHistory      map[string][]taskresult.Result
	fetchHistoryOrder []string
}

func NewGitService() *GitService {
	return &GitService{fetchHistory: map[string][]taskresult.Result{}, fetchRepository: gitengine.Fetch, inspectRepository: gitengine.Inspect, state: State{Phase: "idle", Repositories: []gitengine.Repository{}, Logs: []LogEntry{}}}
}

// ChooseDirectory opens the native directory picker. An empty result means cancelled.
func (s *GitService) ChooseDirectory() (string, error) {
	return s.app.Dialog.OpenFile().SetTitle("选择 Git 仓库扫描路径").CanChooseDirectories(true).CanChooseFiles(false).PromptForSingleSelection()
}

// GetState returns copies so a concurrent task cannot mutate a frontend response.
func (s *GitService) GetState() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := s.state
	copy.Repositories = append([]gitengine.Repository{}, s.state.Repositories...)
	for i := range copy.Repositories {
		copy.Repositories[i].Remotes = append([]gitengine.Remote{}, s.state.Repositories[i].Remotes...)
	}
	copy.Logs = append([]LogEntry{}, s.state.Logs...)
	copy.Results = cloneResults(s.state.Results)
	return copy
}

func cloneResults(results []taskresult.Result) []taskresult.Result {
	copy := append([]taskresult.Result{}, results...)
	for i := range copy {
		if copy[i].Failure != nil {
			f := *copy[i].Failure
			copy[i].Failure = &f
		}
	}
	return copy
}

// GetFetchResults retains the last ten completed fetch tasks for retry provenance.
func (s *GitService) GetFetchResults(taskID string) ([]taskresult.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	results, ok := s.fetchHistory[taskID]
	if !ok {
		return nil, fmt.Errorf("未找到已完成的获取任务，任务记录可能已过期")
	}
	return cloneResults(results), nil
}

// RetryFailed retries errors only. A successful fetch with a failed refresh never fetches again.
func (s *GitService) RetryFailed(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	results, ok := s.fetchHistory[taskID]
	if !ok {
		return fmt.Errorf("未找到已完成的获取任务，任务记录可能已过期")
	}
	paths := []string{}
	refreshOnly := map[string]bool{}
	for _, result := range results {
		if result.Status == "error" {
			paths = append(paths, result.Path)
			refreshOnly[result.Path] = result.NetworkSucceeded && result.Stage == "refresh"
		}
	}
	if len(paths) == 0 {
		return fmt.Errorf("该任务没有需要重试的失败仓库")
	}
	return s.startFetchLocked(paths, refreshOnly, taskID)
}

func (s *GitService) logLocked(level, message string) {
	s.logID++
	s.state.Logs = append(s.state.Logs, LogEntry{s.logID, time.Now().Format("15:04:05"), level, gitengine.Redact(message)})
	if len(s.state.Logs) > 500 {
		s.state.Logs = append([]LogEntry{}, s.state.Logs[len(s.state.Logs)-500:]...)
	}
}

func (s *GitService) begin(kind, root, phase string) (context.Context, error) {
	if s.restarting {
		return nil, fmt.Errorf("应用正在重启更新，请稍后操作")
	}
	if s.state.Busy {
		return nil, fmt.Errorf("已有任务正在运行，请等待完成或取消")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("未找到 Git，请安装 Git 并添加到 PATH")
	}
	ctx, cancel := context.WithCancel(context.Background())
	if s.proxyURL != nil {
		ctx = gitengine.WithProxy(ctx, s.proxyURL())
	}
	s.cancel = cancel
	repos, logs := s.state.Repositories, s.state.Logs
	s.state = State{Busy: true, Kind: kind, Phase: phase, Root: root, StartedAt: time.Now().Format(time.RFC3339), Repositories: repos, Logs: logs}
	s.state.TaskID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	s.state.Results = []taskresult.Result{}
	return ctx, nil
}

// StartScan validates the root and starts discovery without blocking the UI.
func (s *GitService) StartScan(root string) error {
	abs, err := filepath.Abs(root)
	if err != nil || root == "" {
		return fmt.Errorf("请选择扫描路径")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("无法访问扫描路径：%v", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("扫描路径必须是目录")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, err := s.begin("scan", abs, "discovering")
	if err != nil {
		return err
	}
	s.state.Repositories = []gitengine.Repository{}
	s.logLocked("info", "开始扫描："+abs)
	go s.scan(ctx, abs)
	return nil
}

func (s *GitService) scan(ctx context.Context, root string) {
	paths, err := gitengine.Discover(ctx, root, func(count int, path string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state.Visited, s.state.Current = count, path
	}, func(message string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state.Failed++
		s.logLocked("warn", message)
	})
	if err != nil {
		s.finish(ctx, err)
		return
	}
	s.mu.Lock()
	s.state.Phase, s.state.Total = "inspecting", len(paths)
	s.logLocked("info", fmt.Sprintf("发现 %d 个候选仓库，开始读取 Git 信息", len(paths)))
	s.mu.Unlock()
	for _, path := range paths {
		if ctx.Err() != nil {
			break
		}
		s.mu.Lock()
		s.state.Current = path
		s.mu.Unlock()
		started := time.Now()
		repo, err := gitengine.Inspect(ctx, path)
		result := taskresult.Result{TaskID: s.state.TaskID, Kind: "scan", Path: path, Branch: repo.Branch, Stage: "inspect", Status: "success", Attempts: 1, StartedAt: started.Format(time.RFC3339Nano), FinishedAt: time.Now().Format(time.RFC3339Nano), DurationMS: time.Since(started).Milliseconds(), Failure: taskresult.Wrap(err, "inspect")}
		if ctx.Err() != nil {
			result.Status = "cancelled"
			result.Failure = taskresult.Wrap(ctx.Err(), "inspect")
			s.mu.Lock()
			s.recordResultLocked(result)
			s.mu.Unlock()
			break
		}
		s.mu.Lock()
		s.state.Completed++
		if err != nil {
			s.state.Failed++
			result.Status = "error"
			s.logLocked("error", path+"："+err.Error())
		} else {
			s.state.Repositories = append(s.state.Repositories, repo)
			s.state.Succeeded++
			s.logLocked("success", fmt.Sprintf("已识别 %s · %s", repo.Name, repo.Branch))
			if repo.Error != "" {
				s.logLocked("warn", repo.Name+"："+repo.Error)
			}
		}
		s.recordResultLocked(result)
		s.mu.Unlock()
	}
	s.finish(ctx, nil)
}

// StartFetch accepts only repositories from the current scan, deduplicating selections.
func (s *GitService) StartFetch(paths []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startFetchLocked(paths, nil, "")
}

func (s *GitService) startFetchLocked(paths []string, refreshOnly map[string]bool, sourceTaskID string) error {
	selected := []gitengine.Repository{}
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		found := false
		for _, repo := range s.state.Repositories {
			if repo.Path == path {
				selected = append(selected, repo)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("仓库不在当前扫描结果中：%s", path)
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("请至少选择一个仓库")
	}
	ctx, err := s.begin("fetch", s.state.Root, "fetching")
	if err != nil {
		return err
	}
	s.state.Total = len(selected)
	s.state.SourceTaskID = sourceTaskID
	for _, repo := range selected {
		s.state.Results = append(s.state.Results, taskresult.Result{TaskID: s.state.TaskID, Kind: "fetch", Path: repo.Path, Branch: repo.Branch, Stage: "queued", Status: "queued"})
	}
	for i := range s.state.Repositories {
		if seen[s.state.Repositories[i].Path] {
			s.state.Repositories[i].FetchStatus = "idle"
		}
	}
	if sourceTaskID != "" {
		s.logLocked("info", "重试失败仓库，来源任务："+sourceTaskID)
	}
	s.logLocked("info", fmt.Sprintf("开始获取 %d 个仓库的远端更新，工作区保持不变", len(selected)))
	go s.fetch(ctx, selected, refreshOnly)
	return nil
}

func (s *GitService) updateRepoLocked(path string, status, message string, refreshed *gitengine.Repository) {
	for i := range s.state.Repositories {
		if s.state.Repositories[i].Path != path {
			continue
		}
		if refreshed != nil {
			s.state.Repositories[i] = *refreshed
		}
		s.state.Repositories[i].FetchStatus = status
		s.state.Repositories[i].Error = message
		break
	}
}

func (s *GitService) fetch(ctx context.Context, repos []gitengine.Repository, refreshOnly map[string]bool) {
	for _, repo := range repos {
		if ctx.Err() != nil {
			break
		}
		s.mu.Lock()
		started := time.Now()
		stage := "fetch"
		if refreshOnly[repo.Path] {
			stage = "refresh"
		}
		s.state.Current = repo.Path
		s.recordResultLocked(taskresult.Result{TaskID: s.state.TaskID, Kind: "fetch", Path: repo.Path, Branch: repo.Branch, Stage: stage, Status: "running", Attempts: 1, StartedAt: started.Format(time.RFC3339Nano)})
		s.updateRepoLocked(repo.Path, "fetching", "", nil)
		if refreshOnly[repo.Path] {
			s.logLocked("info", "仅刷新本地状态："+repo.Name)
		} else {
			s.logLocked("info", "获取远端更新："+repo.Name)
		}
		s.mu.Unlock()
		var err error
		networkSucceeded := false
		var refreshed *gitengine.Repository
		if refreshOnly[repo.Path] || len(repo.Remotes) > 0 {
			if !refreshOnly[repo.Path] {
				err = s.fetchRepository(ctx, repo.Path)
			}
			if err == nil {
				networkSucceeded = true
				stage = "refresh"
				r, inspectErr := s.inspectRepository(ctx, repo.Path)
				if inspectErr != nil {
					err = inspectErr
				} else {
					refreshed = &r
				}
			}
		}
		s.mu.Lock()
		result := taskresult.Result{TaskID: s.state.TaskID, Kind: "fetch", Path: repo.Path, Branch: repo.Branch, Stage: stage, Status: "success", Attempts: 1, StartedAt: started.Format(time.RFC3339Nano), FinishedAt: time.Now().Format(time.RFC3339Nano), DurationMS: time.Since(started).Milliseconds(), NetworkSucceeded: networkSucceeded, Failure: taskresult.Wrap(err, stage)}
		if ctx.Err() != nil {
			result.Status = "cancelled"
			result.Failure = taskresult.Wrap(ctx.Err(), stage)
			s.recordResultLocked(result)
			s.updateRepoLocked(repo.Path, "cancelled", "任务已取消", nil)
			s.mu.Unlock()
			break
		}
		s.state.Completed++
		switch {
		case len(repo.Remotes) == 0 && !refreshOnly[repo.Path]:
			result.Status = "skipped"
			result.Attempts = 0
			result.Stage = "fetch"
			s.state.Skipped++
			s.updateRepoLocked(repo.Path, "skipped", "未配置远端", nil)
			s.logLocked("warn", repo.Name+"：未配置远端，已跳过")
		case err != nil:
			result.Status = "error"
			s.state.Failed++
			status := "error"
			if networkSucceeded {
				status = "refresh-error"
			}
			s.updateRepoLocked(repo.Path, status, result.Failure.Error(), nil)
			s.logLocked("error", repo.Name+"："+result.Failure.Error())
		default:
			s.state.Succeeded++
			s.updateRepoLocked(repo.Path, "success", refreshed.Error, refreshed)
			if refreshOnly[repo.Path] {
				s.logLocked("success", repo.Name+"：本地状态已刷新（沿用来源任务的远端获取结果）")
			} else {
				s.logLocked("success", repo.Name+"：远端更新已获取")
			}
		}
		s.recordResultLocked(result)
		s.mu.Unlock()
	}
	s.finish(ctx, nil)
}

func (s *GitService) recordResultLocked(result taskresult.Result) {
	for i := range s.state.Results {
		if s.state.Results[i].Path == result.Path {
			s.state.Results[i] = result
			return
		}
	}
	s.state.Results = append(s.state.Results, result)
}

func (s *GitService) finish(ctx context.Context, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.cancel()
	defer func() {
		if s.notify != nil {
			title := "扫描任务"
			if s.state.Kind == "fetch" {
				title = "远端更新任务"
			}
			if label, ok := actionLabels[s.state.Kind]; ok && s.state.Kind != "fetch" {
				title = label + "任务"
			}
			message := fmt.Sprintf("%s：成功 %d，错误/警告 %d，跳过 %d", phasesForNotification(s.state.Phase), s.state.Succeeded, s.state.Failed, s.state.Skipped)
			go s.notify(title, message, "logs")
		}
	}()
	s.state.Busy = false
	s.state.FinishedAt = time.Now().Format(time.RFC3339)
	switch {
	case ctx.Err() != nil:
		for i := range s.state.Results {
			if s.state.Results[i].Status == "queued" {
				s.state.Results[i].Status = "cancelled"
				s.state.Results[i].FinishedAt = time.Now().Format(time.RFC3339Nano)
				s.state.Results[i].Failure = taskresult.Wrap(ctx.Err(), "queued")
			}
		}
		s.state.Phase = "cancelled"
		s.logLocked("warn", "任务已取消，已完成的结果仍然保留")
	case err != nil:
		s.state.Phase = "error"
		s.logLocked("error", err.Error())
	default:
		s.state.Phase = "done"
		s.logLocked("info", fmt.Sprintf("任务完成：成功 %d，错误/警告 %d，跳过 %d", s.state.Succeeded, s.state.Failed, s.state.Skipped))
	}
	if s.state.Kind == "fetch" {
		s.fetchHistory[s.state.TaskID] = cloneResults(s.state.Results)
		s.fetchHistoryOrder = append(s.fetchHistoryOrder, s.state.TaskID)
		if len(s.fetchHistoryOrder) > 10 {
			delete(s.fetchHistory, s.fetchHistoryOrder[0])
			s.fetchHistoryOrder = s.fetchHistoryOrder[1:]
		}
	}
}

func phasesForNotification(phase string) string {
	return map[string]string{"done": "已完成", "error": "失败", "cancelled": "已取消"}[phase]
}

// Cancel stops the running task. Fetch operations already completed are retained.
func (s *GitService) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Busy && s.cancel != nil {
		s.cancel()
	}
}

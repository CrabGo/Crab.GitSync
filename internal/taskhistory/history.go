// Package taskhistory stores completed task snapshots independently of live Git state.
package taskhistory

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"crab.gitsync/internal/taskresult"
)

const maxBytes = 64 << 20

type Log struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}
type Summary struct {
	ID           string `json:"id"`
	SourceTaskID string `json:"sourceTaskID"`
	Kind         string `json:"kind"`
	Root         string `json:"root"`
	ScanListID   string `json:"scanListID"`
	Phase        string `json:"phase"`
	StartedAt    string `json:"startedAt"`
	FinishedAt   string `json:"finishedAt"`
	Total        int    `json:"total"`
	Completed    int    `json:"completed"`
	Succeeded    int    `json:"succeeded"`
	Failed       int    `json:"failed"`
	Skipped      int    `json:"skipped"`
}
type Record struct {
	Summary
	Results []taskresult.Result `json:"results"`
	Logs    []Log               `json:"logs"`
}
type data struct {
	Version    int               `json:"version"`
	Tasks      []Record          `json:"tasks"`
	FetchTimes map[string]string `json:"fetchTimes"`
}
type Store struct {
	mu          sync.Mutex
	path        string
	data        data
	warning     string
	loadWarning string
	recover     bool
}

func New(path string) *Store {
	s := &Store{path: path, data: data{Version: 1, Tasks: []Record{}, FetchTimes: map[string]string{}}}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return s
	}
	if err != nil {
		s.loadWarning = "无法读取任务历史：" + err.Error()
		return s
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Mode().IsRegular() {
		s.recover = true
	}
	bytes, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || len(bytes) > maxBytes {
		s.loadWarning = "任务历史读取失败或超过 64 MiB；本次会话继续使用内存记录"
		return s
	}
	var loaded data
	if err = json.Unmarshal(bytes, &loaded); err != nil || loaded.Version != 1 {
		s.loadWarning = "任务历史文件损坏或格式不兼容；本次会话继续使用内存记录"
		return s
	}
	ids := map[string]bool{}
	for _, record := range loaded.Tasks {
		_, err := time.Parse(time.RFC3339, record.FinishedAt)
		if record.ID == "" || ids[record.ID] || err != nil || (record.Phase != "done" && record.Phase != "error" && record.Phase != "cancelled") {
			s.loadWarning = "任务历史记录无效；本次会话继续使用内存记录"
			return s
		}
		ids[record.ID] = true
	}
	if loaded.FetchTimes == nil {
		loaded.FetchTimes = map[string]string{}
	}
	for key, stamp := range loaded.FetchTimes {
		if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
			delete(loaded.FetchTimes, key)
		}
	}
	s.recover = false
	s.data = loaded
	for i := range s.data.Tasks {
		s.data.Tasks[i] = sanitize(s.data.Tasks[i])
	}
	return s
}
func clone(record Record) Record {
	record.Logs = append([]Log{}, record.Logs...)
	record.Results = append([]taskresult.Result{}, record.Results...)
	for i := range record.Results {
		if record.Results[i].Failure != nil {
			copy := *record.Results[i].Failure
			record.Results[i].Failure = &copy
		}
	}
	return record
}
func sanitize(record Record) Record {
	record = clone(record)
	record.Root = taskresult.Redact(record.Root)
	if len(record.Logs) > 500 {
		record.Logs = record.Logs[len(record.Logs)-500:]
	}
	for i := range record.Logs {
		record.Logs[i].Message = taskresult.Redact(record.Logs[i].Message)
	}
	for i := range record.Results {
		r := &record.Results[i]
		r.Path = taskresult.Redact(r.Path)
		if r.Failure != nil {
			r.Failure.Message = taskresult.Redact(r.Failure.Message)
			r.Failure.Detail = taskresult.Redact(r.Failure.Detail)
		}
	}
	return record
}
func (s *Store) Warning() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadWarning != "" && s.warning != "" {
		return s.loadWarning + "；" + s.warning
	}
	return s.loadWarning + s.warning
}
func (s *Store) List() []Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Summary{}
	for i := len(s.data.Tasks) - 1; i >= 0; i-- {
		out = append(out, s.data.Tasks[i].Summary)
	}
	return out
}
func (s *Store) Get(id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.data.Tasks {
		if r.ID == id {
			return clone(r), nil
		}
	}
	return Record{}, fmt.Errorf("任务历史不存在或已按保留策略清理")
}
func (s *Store) FetchTimes() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for key, value := range s.data.FetchTimes {
		out[key] = value
	}
	return out
}

// Add retains the current session snapshot even when disk writing fails.
// Such errors belong to history storage, never to the recorded Git result.
func (s *Store) Add(record Record, stamps map[string]string, maxTasks, maxDays int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.ID == "" || record.FinishedAt == "" {
		return fmt.Errorf("只保存已完成任务")
	}
	if _, err := time.Parse(time.RFC3339, record.FinishedAt); err != nil {
		return fmt.Errorf("任务完成时间无效")
	}
	if record.Phase != "done" && record.Phase != "error" && record.Phase != "cancelled" {
		return fmt.Errorf("进行中任务不能归档")
	}
	for i, r := range s.data.Tasks {
		if r.ID == record.ID {
			s.data.Tasks = append(s.data.Tasks[:i], s.data.Tasks[i+1:]...)
			break
		}
	}
	s.data.Tasks = append(s.data.Tasks, sanitize(record))
	for key, value := range stamps {
		s.data.FetchTimes[key] = value
	}
	return s.pruneAndWrite(maxTasks, maxDays, time.Now())
}
func (s *Store) Prune(maxTasks, maxDays int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pruneAndWrite(maxTasks, maxDays, time.Now())
}
func (s *Store) pruneAndWrite(maxTasks, maxDays int, now time.Time) error {
	if maxTasks < 1 || maxTasks > 1000 || maxDays < 1 || maxDays > 365 {
		return fmt.Errorf("历史保留需为 1–1000 个任务及 1–365 天")
	}
	cutoff := now.Add(-time.Duration(maxDays) * 24 * time.Hour)
	kept := []Record{}
	for _, r := range s.data.Tasks {
		stamp, err := time.Parse(time.RFC3339, r.FinishedAt)
		if err == nil && !stamp.Before(cutoff) {
			kept = append(kept, r)
		}
	}
	if len(kept) > maxTasks {
		kept = kept[len(kept)-maxTasks:]
	}
	s.data.Tasks = kept
	for key, value := range s.data.FetchTimes {
		stamp, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || stamp.Before(cutoff) {
			delete(s.data.FetchTimes, key)
		}
	}
	if err := s.write(); err != nil {
		s.warning = "任务历史未写入磁盘，当前会话记录仍可查看：" + taskresult.Redact(err.Error())
		return err
	}
	s.warning = ""
	return nil
}
func (s *Store) write() error {
	bytes, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if len(bytes) > maxBytes {
		return fmt.Errorf("任务历史超过 64 MiB，请减少保留数量")
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), "history-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(bytes); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if s.recover {
		backup := fmt.Sprintf("%s.broken-%d", s.path, time.Now().UnixNano())
		if err = os.Rename(s.path, backup); err != nil {
			return fmt.Errorf("保留损坏历史文件失败：%w", err)
		}
		s.recover = false
		s.loadWarning = "损坏任务历史已保留备份：" + backup
	}
	return os.Rename(f.Name(), s.path)
}

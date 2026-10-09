package tasksettings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	Concurrency  int `json:"concurrency"`
	HistoryTasks int `json:"historyTasks"`
	HistoryDays  int `json:"historyDays"`
}

func Default() Config { return Config{Concurrency: 3, HistoryTasks: 100, HistoryDays: 30} }

type Store struct {
	mu     sync.Mutex
	path   string
	config Config
}

func New(path string) (*Store, error) {
	s := &Store{path: path, config: Default()}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	c := Default()
	if err = json.Unmarshal(data, &c); err != nil {
		return s, fmt.Errorf("任务设置文件损坏：%w", err)
	}
	if err = validate(c); err != nil {
		return s, err
	}
	s.config = c
	return s, nil
}
func validate(c Config) error {
	if c.Concurrency < 1 || c.Concurrency > 5 {
		return fmt.Errorf("并发仓库数必须为 1–5")
	}
	if c.HistoryTasks < 1 || c.HistoryTasks > 1000 || c.HistoryDays < 1 || c.HistoryDays > 365 {
		return fmt.Errorf("历史保留需为 1–1000 个任务及 1–365 天")
	}
	return nil
}
func (s *Store) Get() Config { s.mu.Lock(); defer s.mu.Unlock(); return s.config }
func (s *Store) Save(c Config) error {
	// Older clients omit retention fields; keep the existing policy in that case.
	current := s.Get()
	if c.HistoryTasks == 0 {
		c.HistoryTasks = current.HistoryTasks
	}
	if c.HistoryDays == 0 {
		c.HistoryDays = current.HistoryDays
	}
	if err := validate(c); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), "tasks-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	s.config = c
	return nil
}

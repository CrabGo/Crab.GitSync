// Package scansettings persists named root/exclusion lists independently of task state.
package scansettings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type List struct {
	Scheduled       bool     `json:"scheduled"`
	IntervalMinutes int      `json:"intervalMinutes"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Roots           []string `json:"roots"`
	Excludes        []string `json:"excludes"`
}
type Store struct {
	mu    sync.Mutex
	path  string
	lists []List
}

func PathKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if actual, err := filepath.EvalSymlinks(path); err == nil {
		path = actual
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
func Normalize(list List) (List, error) {
	if list.IntervalMinutes == 0 {
		list.IntervalMinutes = 30
	}
	if list.IntervalMinutes < 1 || list.IntervalMinutes > 1440 {
		return list, fmt.Errorf("定时获取间隔需为 1–1440 分钟")
	}
	list.Name = strings.TrimSpace(list.Name)
	if list.Name == "" || len([]rune(list.Name)) > 80 {
		return list, fmt.Errorf("扫描列表名称需为 1–80 个字符")
	}
	if len(list.Roots) == 0 || len(list.Roots) > 32 {
		return list, fmt.Errorf("请设置 1–32 个扫描根目录")
	}
	roots := []string{}
	seen := map[string]bool{}
	for _, root := range list.Roots {
		root = strings.TrimSpace(root)
		if root == "" {
			return list, fmt.Errorf("扫描根目录不能为空")
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			return list, err
		}
		abs = filepath.Clean(abs)
		key := PathKey(abs)
		if !seen[key] {
			roots = append(roots, abs)
			seen[key] = true
		}
	}
	if len(list.Excludes) > 64 {
		return list, fmt.Errorf("排除目录最多 64 个")
	}
	excludes := []string{}
	seen = map[string]bool{}
	for _, exclude := range list.Excludes {
		exclude = strings.TrimSpace(exclude)
		if exclude == "" {
			continue
		}
		exclude = filepath.Clean(exclude)
		if !filepath.IsAbs(exclude) && (exclude == ".." || strings.HasPrefix(exclude, ".."+string(filepath.Separator)) || filepath.VolumeName(exclude) != "") {
			return list, fmt.Errorf("相对排除目录不能超出扫描根目录")
		}
		key := PathKey(exclude)
		if !seen[key] {
			excludes = append(excludes, exclude)
			seen[key] = true
		}
	}
	list.Roots, list.Excludes = roots, excludes
	return list, nil
}
func clone(lists []List) []List {
	copy := append([]List{}, lists...)
	for i := range copy {
		copy[i].Roots = append([]string{}, copy[i].Roots...)
		copy[i].Excludes = append([]string{}, copy[i].Excludes...)
	}
	return copy
}
func New(path string) (*Store, error) {
	s := &Store{path: path, lists: []List{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var lists []List
	if err = json.Unmarshal(data, &lists); err != nil {
		return s, fmt.Errorf("扫描列表文件损坏：%w", err)
	}
	if len(lists) > 20 {
		return s, fmt.Errorf("扫描列表数量超出限制")
	}
	ids := map[string]bool{}
	for i, list := range lists {
		if list.ID == "" || ids[list.ID] {
			return s, fmt.Errorf("扫描列表 ID 无效或重复")
		}
		ids[list.ID] = true
		lists[i], err = Normalize(list)
		if err != nil {
			return s, err
		}
	}
	s.lists = lists
	return s, nil
}
func (s *Store) Get() []List { s.mu.Lock(); defer s.mu.Unlock(); return clone(s.lists) }
func (s *Store) Save(list List) (List, error) {
	var err error
	list, err = Normalize(list)
	if err != nil {
		return list, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.lists)
	index := -1
	for i, value := range next {
		if value.ID == list.ID {
			index = i
		}
		if value.ID != list.ID && strings.EqualFold(value.Name, list.Name) {
			return list, fmt.Errorf("扫描列表名称已存在")
		}
	}
	if list.ID == "" {
		if len(next) >= 20 {
			return list, fmt.Errorf("扫描列表最多 20 个")
		}
		list.ID = fmt.Sprintf("scan-%d", time.Now().UnixNano())
		next = append(next, list)
	} else {
		if index < 0 {
			return list, fmt.Errorf("扫描列表已不存在，请重新加载")
		}
		next[index] = list
	}
	if err = s.write(next); err != nil {
		return list, err
	}
	s.lists = clone(next)
	return clone([]List{list})[0], nil
}
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := []List{}
	found := false
	for _, list := range s.lists {
		if list.ID == id {
			found = true
		} else {
			next = append(next, list)
		}
	}
	if !found {
		return fmt.Errorf("扫描列表已不存在")
	}
	if err := s.write(next); err != nil {
		return err
	}
	s.lists = next
	return nil
}
func (s *Store) write(lists []List) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lists, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), "scans-*.tmp")
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
	return os.Rename(f.Name(), s.path)
}

package main

import (
	"context"
	"fmt"
	"strings"

	"crab.gitsync/internal/scansettings"
)

func (s *GitService) GetScanLists() ([]scansettings.List, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scanLists == nil {
		return []scansettings.List{}, fmt.Errorf("扫描列表存储尚未初始化")
	}
	return s.scanLists.Get(), s.scanLoadError
}
func (s *GitService) SaveScanList(list scansettings.List) (scansettings.List, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scanLists == nil {
		return list, fmt.Errorf("扫描列表存储尚未初始化")
	}
	saved, err := s.scanLists.Save(list)
	if err == nil {
		s.scanLoadError = nil
		s.resetScheduleLocked(saved)
	}
	return saved, err
}
func (s *GitService) RemoveScanList(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scanLists == nil {
		return fmt.Errorf("扫描列表存储尚未初始化")
	}
	if err := s.scanLists.Remove(id); err != nil {
		return err
	}
	s.scanLoadError = nil
	delete(s.schedules, id)
	return nil
}
func (s *GitService) StartScanList(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scanLists == nil {
		return fmt.Errorf("扫描列表存储尚未初始化")
	}
	var selected *scansettings.List
	for _, list := range s.scanLists.Get() {
		if list.ID == id {
			copy := list
			selected = &copy
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("扫描列表已不存在，请重新加载")
	}
	ctx, err := s.begin("scan", strings.Join(selected.Roots, "；"), "discovering")
	if err != nil {
		return err
	}
	s.state.ScanListID = id
	s.state.Repositories = nil
	s.logLocked("info", fmt.Sprintf("开始扫描列表「%s」：%d 个根目录，%d 条排除目录", selected.Name, len(selected.Roots), len(selected.Excludes)))
	s.launch(ctx, nil, func(ctx context.Context) error { return s.scanPaths(ctx, selected.Roots, selected.Excludes) })
	return nil
}

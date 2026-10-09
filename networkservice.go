package main

import (
	"crab.gitsync/internal/networksettings"
	"sync"
)

// NetworkService persists application-specific proxy settings without changing global Git or OS settings.
type NetworkService struct {
	store     *networksettings.Store
	loadError error
	mu        sync.Mutex
}

func (s *NetworkService) GetConfig() (networksettings.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.Get(), s.loadError
}
func (s *NetworkService) SaveConfig(config networksettings.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.Save(config); err != nil {
		return err
	}
	s.loadError = nil
	return nil
}

package main

import (
	"crab.gitsync/internal/diagnostics"
	"crab.gitsync/internal/networksettings"
	"sync"
)

// NetworkService persists application-specific proxy settings without changing global Git or OS settings.
type NetworkService struct {
	store       *networksettings.Store
	loadError   error
	mu          sync.Mutex
	diagnostics *diagnostics.Runner
}

func (s *NetworkService) StartDiagnosis(remote string) error {
	return s.diagnostics.Start(s.store.Get(), remote)
}
func (s *NetworkService) GetDiagnosis() diagnostics.State { return s.diagnostics.State() }
func (s *NetworkService) CancelDiagnosis()                { s.diagnostics.Cancel() }

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

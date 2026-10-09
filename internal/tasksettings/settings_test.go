package tasksettings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPersistenceValidationAndWriteRollback(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tasks.json")
	s, err := New(path)
	if err != nil || s.Get().Concurrency != 3 {
		t.Fatal("default", err)
	}
	for _, limit := range []int{1, 5} {
		if err := s.Save(Config{Concurrency: limit}); err != nil {
			t.Fatal(err)
		}
		loaded, err := New(path)
		if err != nil || loaded.Get().Concurrency != limit {
			t.Fatal("reload", err)
		}
	}
	for _, limit := range []int{0, 6, -1} {
		if s.Save(Config{Concurrency: limit}) == nil {
			t.Fatal("invalid accepted")
		}
	}
	s.path = root
	if s.Save(Config{Concurrency: 2}) == nil || s.Get().Concurrency != 5 {
		t.Fatal("write rollback failed")
	}
	os.WriteFile(path, []byte("invalid"), 0600)
	loaded, err := New(path)
	if err == nil || loaded.Get().Concurrency != 3 {
		t.Fatal("corruption hidden")
	}
	if err = loaded.Save(Config{Concurrency: 2}); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyConfigGetsRetentionDefaultsAndKeepsPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	os.WriteFile(path, []byte(`{"concurrency":2}`), 0600)
	s, err := New(path)
	if err != nil || s.Get().HistoryTasks != 100 || s.Get().HistoryDays != 30 {
		t.Fatal("legacy defaults", err)
	}
	if err = s.Save(Config{Concurrency: 2, HistoryTasks: 5, HistoryDays: 7}); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(Config{Concurrency: 3}); err != nil || s.Get().HistoryTasks != 5 || s.Get().HistoryDays != 7 {
		t.Fatal("old client lost retention", err)
	}
	for _, c := range []Config{{Concurrency: 3, HistoryTasks: -1, HistoryDays: 7}, {Concurrency: 3, HistoryTasks: 1001, HistoryDays: 7}, {Concurrency: 3, HistoryTasks: 5, HistoryDays: 366}} {
		if s.Save(c) == nil {
			t.Fatal("invalid retention accepted")
		}
	}
}

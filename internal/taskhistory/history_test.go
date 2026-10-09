package taskhistory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crab.gitsync/internal/taskresult"
)

func record(id string, days int) Record {
	return Record{Summary: Summary{ID: id, Kind: "fetch", Phase: "done", FinishedAt: time.Now().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)}, Results: []taskresult.Result{{Status: "error", Failure: &taskresult.Failure{Message: "https://user:secret@example.test/repo?token=hidden", Detail: "https://user:secret@example.test/repo?token=hidden"}}}, Logs: []Log{{Message: "fetch https://user:secret@example.test/repo?token=hidden"}}}
}
func TestRetentionPersistenceRedactionAndSnapshotIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	s := New(path)
	stamps := map[string]string{"old": time.Now().Add(-40 * 24 * time.Hour).Format(time.RFC3339Nano), "recent": time.Now().Format(time.RFC3339Nano)}
	for _, r := range []Record{record("old", 40), record("a", 2), record("b", 1), record("c", 0)} {
		if err := s.Add(r, stamps, 2, 30); err != nil {
			t.Fatal(err)
		}
	}
	list := s.List()
	if len(list) != 2 || list[0].ID != "c" || list[1].ID != "b" {
		t.Fatal("retention", list)
	}
	loaded := New(path)
	if loaded.Warning() != "" || len(loaded.List()) != 2 || len(loaded.FetchTimes()) != 1 {
		t.Fatal("reload", loaded.Warning())
	}
	bytes, _ := os.ReadFile(path)
	if strings.Contains(string(bytes), "secret") || strings.Contains(string(bytes), "hidden") {
		t.Fatal("credentials persisted")
	}
	r, _ := loaded.Get("c")
	r.Logs[0].Message = "changed"
	r.Results[0].Path = "changed"
	again, _ := loaded.Get("c")
	if again.Logs[0].Message == "changed" || again.Results[0].Path == "changed" {
		t.Fatal("mutable snapshot")
	}
	copy := loaded.FetchTimes()
	copy["recent"] = "changed"
	if loaded.FetchTimes()["recent"] == "changed" {
		t.Fatal("mutable freshness")
	}
	if err := loaded.Prune(1, 1); err != nil || len(loaded.List()) != 1 {
		t.Fatal("policy change", err)
	}
}
func TestCorruptionBackupRecoveryAndWriteFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "history.json")
	os.WriteFile(path, []byte("broken"), 0600)
	s := New(path)
	if s.Warning() == "" {
		t.Fatal("corruption hidden")
	}
	if err := s.Add(record("recovered", 0), nil, 100, 30); err != nil {
		t.Fatal(err)
	}
	backups, _ := filepath.Glob(path + ".broken-*")
	if len(backups) != 1 {
		t.Fatal("corrupt file not preserved")
	}
	bytes, _ := os.ReadFile(backups[0])
	if string(bytes) != "broken" {
		t.Fatal("backup changed")
	}
	if s.Warning() == "" || len(New(path).List()) != 1 {
		t.Fatal("recovery warning or data missing")
	}
	s.path = root
	if err := s.Add(record("memory", 0), nil, 100, 30); err == nil {
		t.Fatal("expected write failure")
	}
	r, err := s.Get("memory")
	if err != nil || r.Phase != "done" || s.Warning() == "" {
		t.Fatal("write failure lost session result", r, err)
	}
	bad := record("active", 0)
	bad.Phase = "fetching"
	if s.Add(bad, nil, 100, 30) == nil {
		t.Fatal("active task archived")
	}
	bad = record("bad time", 0)
	bad.FinishedAt = "invalid"
	if s.Add(bad, nil, 100, 30) == nil {
		t.Fatal("invalid time archived")
	}
}

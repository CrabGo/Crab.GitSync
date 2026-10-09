package scansettings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistenceNormalizationAndIsolation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config", "scans.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.Save(List{Name: " work ", Roots: []string{root, filepath.Join(root, ".")}, Excludes: []string{"build", "build", "nested/../archive"}})
	if err != nil || list.ID == "" || list.Name != "work" || len(list.Roots) != 1 || len(list.Excludes) != 2 {
		t.Fatal("normalization failed", list, err)
	}
	list.Roots[0] = "changed"
	snapshot := s.Get()
	if snapshot[0].Roots[0] != root {
		t.Fatal("save response changed store")
	}
	snapshot[0].Excludes[0] = "changed"
	if s.Get()[0].Excludes[0] != "build" {
		t.Fatal("get response changed store")
	}
	loaded, err := New(path)
	if err != nil || loaded.Get()[0].ID != list.ID {
		t.Fatal("reload failed", err)
	}
	value := loaded.Get()[0]
	value.Name = "renamed"
	if _, err = loaded.Save(value); err != nil {
		t.Fatal(err)
	}
	if err = loaded.Remove(list.ID); err != nil {
		t.Fatal(err)
	}
	reloaded, err := New(path)
	if err != nil || len(reloaded.Get()) != 0 {
		t.Fatal("delete not persisted", err)
	}
}

func TestInvalidSettingsAndWriteFailurePreserveStore(t *testing.T) {
	root := t.TempDir()
	s, _ := New(filepath.Join(root, "scans.json"))
	good, err := s.Save(List{Name: "work", Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []List{{Name: "", Roots: []string{root}}, {Name: "no roots"}, {Name: "escape", Roots: []string{root}, Excludes: []string{"../outside"}}, {Name: "WORK", Roots: []string{root}}, {ID: "missing", Name: "missing", Roots: []string{root}}} {
		if _, err = s.Save(bad); err == nil {
			t.Fatal("invalid list accepted", bad)
		}
	}
	if len(s.Get()) != 1 {
		t.Fatal("invalid save changed store")
	}
	s.path = root // Renaming a file over a directory fails even with elevated permissions.
	good.Name = "failed rename"
	if _, err = s.Save(good); err == nil {
		t.Fatal("expected write failure")
	}
	if s.Get()[0].Name != "work" {
		t.Fatal("write failure changed in-memory state")
	}
	path := filepath.Join(root, "broken.json")
	os.WriteFile(path, []byte("invalid JSON"), 0600)
	broken, err := New(path)
	if err == nil || len(broken.Get()) != 0 {
		t.Fatal("corrupt file silently loaded")
	}
	if _, err = broken.Save(List{Name: "recovered", Roots: []string{root}}); err != nil {
		t.Fatal("explicit save cannot recover", err)
	}
}

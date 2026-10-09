package networksettings

import (
	"path/filepath"
	"testing"
)

func TestPersistAndValidateProxy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "network.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Get() != Default() {
		t.Fatal("unexpected defaults")
	}
	u, err := s.Get().URL()
	if err != nil || u.String() != "http://127.0.0.1:33210" {
		t.Fatalf("default URL: %v %v", u, err)
	}
	c := Config{Enabled: true, Protocol: "socks5h", Host: "::1", Port: 1080}
	if err = s.Save(c); err != nil {
		t.Fatal(err)
	}
	loaded, err := New(path)
	if err != nil || loaded.Get() != c {
		t.Fatalf("reload: %v %v", loaded, err)
	}
	for _, bad := range []Config{{true, "ftp", "localhost", 80}, {true, "http", "http://localhost", 80}, {true, "http", "localhost", 0}, {true, "http", "localhost", 65536}, {true, "http", "host/path", 80}} {
		if err = s.Save(bad); err == nil {
			t.Fatalf("invalid proxy accepted: %+v", bad)
		}
		if s.Get() != c {
			t.Fatal("invalid proxy changed configuration")
		}
	}
	c.Enabled = false
	if err = s.Save(c); err != nil {
		t.Fatal(err)
	}
	if u, err = s.Get().URL(); err != nil || u != nil {
		t.Fatal("disabled proxy returned URL")
	}
	if err = s.Save(Default()); err != nil {
		t.Fatal("cannot replace existing settings:", err)
	}
}

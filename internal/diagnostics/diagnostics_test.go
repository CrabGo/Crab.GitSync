package diagnostics

import (
	"context"
	"crab.gitsync/internal/networksettings"
	"errors"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func done(t *testing.T, r *Runner) State {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := r.State()
		if !s.Busy {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("diagnostic did not finish")
	return State{}
}
func TestProbeSnapshotsAndIndependentFailures(t *testing.T) {
	var httpCalls, gitCalls atomic.Int32
	r := New(Probes{Dial: func(context.Context, string) error { return errors.New("connection refused") }, HTTP: func(_ context.Context, p *url.URL) error {
		httpCalls.Add(1)
		if p.String() != "http://127.0.0.1:33210" {
			t.Error("wrong proxy snapshot")
		}
		return errors.New("SSL certificate problem")
	}, Git: func(_ context.Context, remote string, _ *url.URL) error { gitCalls.Add(1); return nil }})
	if err := r.Start(networksettings.Default(), "https://github.com/o/r.git"); err != nil {
		t.Fatal(err)
	}
	s := done(t, r)
	if s.Status != "error" || s.Steps[0].Failure.Category != "proxy" || s.Steps[1].Failure.Category != "tls" || s.Steps[2].Status != "success" || httpCalls.Load() != 1 || gitCalls.Load() != 1 {
		t.Fatalf("unexpected steps: %+v", s)
	}
	s.Steps[0].Failure.Category = "mutated"
	if r.State().Steps[0].Failure.Category == "mutated" {
		t.Fatal("mutable diagnostic snapshot")
	}
}
func TestDisableCancelAndInvalidRemote(t *testing.T) {
	started := make(chan struct{})
	var dialCalls atomic.Int32
	r := New(Probes{Dial: func(context.Context, string) error { dialCalls.Add(1); return nil }, HTTP: func(ctx context.Context, _ *url.URL) error { close(started); <-ctx.Done(); return ctx.Err() }, Git: func(context.Context, string, *url.URL) error { t.Error("Git probe ran after cancellation"); return nil }})
	for _, remote := range []string{"--upload-pack=evil", "file:///tmp/test", "https://user:pass@github.com/o/r.git", "https://github.com/o/r.git?token=private"} {
		if r.Start(networksettings.Default(), remote) == nil {
			t.Fatal("invalid remote accepted")
		}
	}
	c := networksettings.Default()
	c.Enabled = false
	if err := r.Start(c, "git@github.com:o/r.git"); err != nil {
		t.Fatal(err)
	}
	<-started
	if r.Start(c, "https://github.com/o/r.git") == nil {
		t.Fatal("parallel diagnosis allowed")
	}
	r.Cancel()
	s := done(t, r)
	if s.Status != "cancelled" || dialCalls.Load() != 0 || s.Steps[0].Status != "skipped" || s.Steps[1].Failure.Category != "cancelled" || s.Steps[2].Status != "cancelled" {
		t.Fatalf("unexpected cancellation: %+v", s)
	}
}
func TestDefaultPortProbeWithLocalListener(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	r := New(Probes{HTTP: func(context.Context, *url.URL) error { return nil }, Git: func(context.Context, string, *url.URL) error { return nil }})
	c := networksettings.Default()
	c.Port = l.Addr().(*net.TCPAddr).Port
	if err = r.Start(c, "https://github.com/o/r.git"); err != nil {
		t.Fatal(err)
	}
	s := done(t, r)
	if s.Status != "success" {
		t.Fatalf("local listener unreachable: %+v", s)
	}
}

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"crab.gitsync/internal/gitengine"
)

func retryService(t *testing.T, enabled bool) (*GitService, string) {
	t.Helper()
	s := NewGitService()
	path := filepath.Join(t.TempDir(), "repo")
	s.state.Repositories = []gitengine.Repository{{Path: path, Remotes: []gitengine.Remote{{Name: "origin"}}}}
	s.retryEnabled = func() bool { return enabled }
	s.waitRetry = func(context.Context, time.Duration) error { return nil }
	s.inspectRepository = func(_ context.Context, path string) (gitengine.Repository, error) {
		return gitengine.Repository{Path: path}, nil
	}
	return s, path
}

func TestAutoRetryBoundedAndDefaultOff(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s, path := retryService(t, enabled)
		calls := 0
		delays := []time.Duration{}
		s.fetchRepository = func(context.Context, string) error { calls++; return fmt.Errorf("connection reset") }
		s.waitRetry = func(_ context.Context, delay time.Duration) error { delays = append(delays, delay); return nil }
		if err := s.StartFetch([]string{path}); err != nil {
			t.Fatal(err)
		}
		state := waitResult(t, s)
		want := 1
		if enabled {
			want = 3
		}
		if calls != want || state.Results[0].Attempts != want || state.Completed != 1 || state.Failed != 1 {
			t.Fatalf("bad attempts %+v calls=%d", state.Results, calls)
		}
		if enabled && (len(delays) != 2 || delays[0] != 2*time.Second || delays[1] != 5*time.Second) {
			t.Fatal("wrong backoff", delays)
		}
	}
}

func TestAutoRetryTransientSuccessAndNeverRetriesOtherFailures(t *testing.T) {
	for _, message := range []string{"connection refused", "authentication failed", "SSL certificate problem", "repository not found", "工作区有修改", "unexpected error"} {
		t.Run(message, func(t *testing.T) {
			s, path := retryService(t, true)
			calls := 0
			s.fetchRepository = func(context.Context, string) error {
				calls++
				if calls == 1 {
					return fmt.Errorf("%s", message)
				}
				return nil
			}
			if err := s.StartFetch([]string{path}); err != nil {
				t.Fatal(err)
			}
			state := waitResult(t, s)
			want := 1
			if message == "connection refused" {
				want = 2
			}
			if calls != want || state.Results[0].Attempts != want {
				t.Fatal("unsafe retry", calls, state.Results)
			}
			if want == 2 && (state.Succeeded != 1 || !state.Results[0].NetworkSucceeded) {
				t.Fatal("recovered success lost")
			}
		})
	}
	// Inspection remains one attempt even for a network-looking error.
	s, path := retryService(t, true)
	calls := 0
	s.fetchRepository = func(context.Context, string) error { calls++; return nil }
	s.inspectRepository = func(context.Context, string) (gitengine.Repository, error) {
		return gitengine.Repository{}, fmt.Errorf("connection refused")
	}
	if err := s.StartFetch([]string{path}); err != nil {
		t.Fatal(err)
	}
	state := waitResult(t, s)
	if calls != 1 || state.Results[0].Failure.Category != "refresh" {
		t.Fatal("refresh triggered remote retry")
	}
}

func TestAutoRetryCancellationAndTaskSnapshot(t *testing.T) {
	s, path := retryService(t, true)
	waiting := make(chan struct{})
	calls := 0
	s.fetchRepository = func(context.Context, string) error { calls++; return fmt.Errorf("connection refused") }
	s.waitRetry = func(ctx context.Context, _ time.Duration) error { close(waiting); <-ctx.Done(); return ctx.Err() }
	if err := s.StartFetch([]string{path}); err != nil {
		t.Fatal(err)
	}
	<-waiting
	if state := s.GetState(); state.Results[0].Stage != "retry-wait" {
		t.Fatal("wait not visible")
	}
	s.Cancel()
	state := waitResult(t, s)
	if calls != 1 || state.Results[0].Attempts != 1 || state.Results[0].Status != "cancelled" || state.Failed != 0 {
		t.Fatal("cancel did not stop retry", state.Results, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitRetry(ctx, time.Hour); err != context.Canceled {
		t.Fatal("real backoff not cancelled", err)
	}

	// A setting changed during fetch cannot alter the current task's retry policy.
	s, path = retryService(t, true)
	calls = 0
	s.fetchRepository = func(context.Context, string) error {
		calls++
		s.retryEnabled = func() bool { return false }
		if calls == 1 {
			return fmt.Errorf("connection refused")
		}
		return nil
	}
	if err := s.StartFetch([]string{path}); err != nil {
		t.Fatal(err)
	}
	state = waitResult(t, s)
	if calls != 2 || state.Succeeded != 1 {
		t.Fatal("task setting snapshot changed")
	}
}

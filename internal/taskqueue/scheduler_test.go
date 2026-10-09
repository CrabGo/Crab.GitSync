package taskqueue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("work did not drain")
		var zero T
		return zero
	}
}

func TestRepositoryQueueCancellationAndIndependentKeys(t *testing.T) {
	s := New()
	entered, release, first := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		first <- s.Do(context.Background(), []string{"repo", "repo"}, func(context.Context) error { close(entered); <-release; return nil })
	}()
	receive(t, entered)
	if s.Pause() {
		t.Fatal("restart allowed with work in flight")
	}
	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() {
		second <- s.Do(ctx, []string{"repo"}, func(context.Context) error { t.Error("cancelled queued operation ran"); return nil })
	}()
	if err := s.Do(context.Background(), []string{"other"}, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := receive(t, second); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := receive(t, first); err != nil {
		t.Fatal(err)
	}
	if !s.Pause() {
		t.Fatal("drained scheduler could not pause")
	}
	if _, _, err := s.Begin(context.Background()); err == nil {
		t.Fatal("task admitted during restart")
	}
	s.Resume()
	if len(s.gates) != 0 {
		t.Fatal("repository leases leaked")
	}
}

func TestAdmissionReadLeasesAndCancellation(t *testing.T) {
	s := New()
	read1, done1, err := s.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	read2, done2, err := s.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Begin(context.Background()); err == nil {
		t.Fatal("task overlapped a preview")
	}
	if s.Pause() {
		t.Fatal("restart overlapped reads")
	}
	s.Cancel()
	if read1.Err() != context.Canceled || read2.Err() != context.Canceled {
		t.Fatal("reads not cancelled")
	}
	done1()
	done1()
	if s.Pause() {
		t.Fatal("second read lease lost")
	}
	done2()
	ctx, _, err := s.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Begin(context.Background()); err == nil {
		t.Fatal("parallel task admitted")
	}
	if _, _, err := s.Read(context.Background()); err == nil {
		t.Fatal("preview overlapped task")
	}
	s.Cancel()
	if ctx.Err() != context.Canceled || s.Pause() {
		t.Fatal("cancel released task before cleanup")
	}
	s.End()
	if !s.Pause() {
		t.Fatal("finished task prevents restart")
	}
}

func TestOppositeKeyOrderCannotDeadlock(t *testing.T) {
	s := New()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	results := make(chan error, 2)
	for _, keys := range [][]string{{"a", "b"}, {"b", "a"}} {
		go func(keys []string) { results <- s.Do(ctx, keys, func(context.Context) error { return nil }) }(keys)
	}
	for range 2 {
		if err := receive(t, results); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSameRepositoryWorkNeverOverlaps(t *testing.T) {
	s := New()
	var running atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	results := make(chan error, 2)
	go func() {
		results <- s.Do(context.Background(), []string{"shared.git"}, func(context.Context) error {
			if running.Add(1) != 1 {
				t.Error("repository work overlapped")
			}
			close(entered)
			<-release
			running.Add(-1)
			return nil
		})
	}()
	receive(t, entered)
	go func() {
		results <- s.Do(context.Background(), []string{"shared.git"}, func(context.Context) error {
			if running.Add(1) != 1 {
				t.Error("repository work overlapped")
			}
			running.Add(-1)
			return nil
		})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.mu.Lock()
		queued := s.inFlight == 2
		s.mu.Unlock()
		if queued {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("second operation was not queued")
		}
		time.Sleep(time.Millisecond)
	}
	if running.Load() != 1 {
		t.Error("queued operation entered occupied repository")
	}
	close(release)
	for range 2 {
		if err := receive(t, results); err != nil {
			t.Fatal(err)
		}
	}
}

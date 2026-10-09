// Package taskqueue owns task admission, cancellation and repository leases.
package taskqueue

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type gate struct {
	token chan struct{}
	refs  int
}
type Scheduler struct {
	mu             sync.Mutex
	active, paused bool
	inFlight       int
	cancel         context.CancelFunc
	readers        map[*readLease]context.CancelFunc
	gates          map[string]*gate
}
type readLease struct{ reserved byte }

func New() *Scheduler {
	return &Scheduler{readers: map[*readLease]context.CancelFunc{}, gates: map[string]*gate{}}
}

func (s *Scheduler) Begin(parent context.Context) (context.Context, context.CancelFunc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused {
		return nil, nil, fmt.Errorf("应用正在重启更新，请稍后操作")
	}
	if s.active || s.inFlight != 0 || len(s.readers) != 0 {
		return nil, nil, fmt.Errorf("已有 Git 工作正在运行，请等待完成或取消")
	}
	ctx, cancel := context.WithCancel(parent)
	s.active, s.cancel = true, cancel
	return ctx, cancel, nil
}

// End is called only after all dispatched work has returned, including cancellation cleanup.
func (s *Scheduler) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFlight != 0 {
		panic("taskqueue: ending a task with work still running")
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.active, s.cancel = false, nil
}

// Read reserves read-only Git work before releasing the service state lock.
func (s *Scheduler) Read(parent context.Context) (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active || s.paused {
		return nil, nil, fmt.Errorf("已有任务正在运行或应用正在更新，请稍后读取")
	}
	ctx, cancel := context.WithCancel(parent)
	lease := &readLease{}
	s.readers[lease] = cancel
	var once sync.Once
	return ctx, func() { once.Do(func() { cancel(); s.mu.Lock(); delete(s.readers, lease); s.mu.Unlock() }) }, nil
}

func (s *Scheduler) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	for _, cancel := range s.readers {
		cancel()
	}
}

// Pause atomically blocks new admission only when every task and read has drained.
func (s *Scheduler) Pause() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active || s.inFlight != 0 || len(s.readers) != 0 {
		return false
	}
	s.paused = true
	return true
}
func (s *Scheduler) Resume() { s.mu.Lock(); s.paused = false; s.mu.Unlock() }

// Do queues cancellable work behind canonical repository keys. Sorted keys prevent deadlock.
// Locks cover the whole operation, including refresh and cancellation cleanup.
func (s *Scheduler) Do(ctx context.Context, keys []string, work func(context.Context) error) error {
	unique := map[string]bool{}
	for _, key := range keys {
		if key != "" {
			unique[key] = true
		}
	}
	ordered := make([]string, 0, len(unique))
	for key := range unique {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	s.mu.Lock()
	if s.paused {
		s.mu.Unlock()
		return fmt.Errorf("应用正在重启更新")
	}
	s.inFlight++
	gates := make([]*gate, len(ordered))
	for i, key := range ordered {
		g := s.gates[key]
		if g == nil {
			g = &gate{token: make(chan struct{}, 1)}
			g.token <- struct{}{}
			s.gates[key] = g
		}
		g.refs++
		gates[i] = g
	}
	s.mu.Unlock()
	acquired := 0
	defer func() {
		for i := acquired - 1; i >= 0; i-- {
			gates[i].token <- struct{}{}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, key := range ordered {
			gates[i].refs--
			if gates[i].refs == 0 {
				delete(s.gates, key)
			}
		}
		s.inFlight--
	}()
	for _, g := range gates {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-g.token:
			acquired++
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return work(ctx)
}

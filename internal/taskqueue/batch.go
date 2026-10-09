package taskqueue

import (
	"context"
	"fmt"
	"sync"
)

type Job struct {
	Keys []string
	Run  func(context.Context) error
}

// Run dispatches a bounded batch and waits for every in-flight job to release its leases.
// Per-job failures are returned in input order and do not stop independent jobs.
func (s *Scheduler) Run(ctx context.Context, jobs []Job, limit int) ([]error, error) {
	if limit < 1 || limit > 5 {
		return nil, fmt.Errorf("并发仓库数必须为 1–5")
	}
	results := make([]error, len(jobs))
	queue := make(chan int)
	var workers sync.WaitGroup
	for range min(limit, len(jobs)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range queue {
				results[index] = s.Do(ctx, jobs[index].Keys, jobs[index].Run)
			}
		}()
	}
	dispatched := 0
dispatch:
	for index := range jobs {
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			break dispatch
		case queue <- index:
			dispatched++
		}
	}
	close(queue)
	workers.Wait()
	for index := dispatched; index < len(jobs); index++ {
		results[index] = ctx.Err()
	}
	return results, ctx.Err()
}

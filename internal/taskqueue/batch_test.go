package taskqueue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestBatchFailureDoesNotStopOtherJobs(t *testing.T) {
	s := New()
	failure := errors.New("one repository failed")
	var calls atomic.Int32
	jobs := []Job{}
	for i := range 5 {
		jobs = append(jobs, Job{Keys: []string{"shared-metadata"}, Run: func(context.Context) error {
			calls.Add(1)
			if i == 1 {
				return failure
			}
			return nil
		}})
	}
	results, err := s.Run(context.Background(), jobs, 3)
	if err != nil || calls.Load() != 5 || len(results) != 5 || !errors.Is(results[1], failure) {
		t.Fatal("independent failure lost", results, err)
	}
	for i, value := range results {
		if i != 1 && value != nil {
			t.Fatal("independent job failed", i, value)
		}
	}
	if _, err = s.Run(context.Background(), jobs, 6); err == nil {
		t.Fatal("invalid limit accepted")
	}
}

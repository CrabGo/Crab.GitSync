package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTaskCompletionNotifications(t *testing.T) {
	for _, phase := range []string{"done", "error", "cancelled"} {
		t.Run(phase, func(t *testing.T) {
			s := NewGitService()
			ctx, cancel := context.WithCancel(context.Background())
			s.cancel = cancel
			s.state.Kind = "fetch"
			s.state.Busy = true
			s.state.Succeeded = 2
			s.state.Failed = 1
			result := make(chan []string, 1)
			s.notify = func(title, body, page string) { result <- []string{title, body, page} }
			var err error
			if phase == "error" {
				err = errors.New("test failure")
			}
			if phase == "cancelled" {
				cancel()
			}
			s.finish(ctx, err)
			select {
			case got := <-result:
				if got[0] != "远端更新任务" || got[2] != "logs" || !strings.Contains(got[1], phasesForNotification(phase)) || !strings.Contains(got[1], "成功 2") {
					t.Fatalf("unexpected notification: %v", got)
				}
			case <-time.After(time.Second):
				t.Fatal("missing notification")
			}
		})
	}
}

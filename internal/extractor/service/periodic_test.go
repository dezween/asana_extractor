package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunPeriodic_RunsImmediatelyThenOnTicks(t *testing.T) {
	var calls int32
	ctx, cancel := context.WithCancel(context.Background())

	fn := func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	}

	done := make(chan struct{})
	go func() {
		RunPeriodic(ctx, 20*time.Millisecond, fn, nil)
		close(done)
	}()

	time.Sleep(70 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunPeriodic did not stop after ctx cancellation")
	}

	if got := atomic.LoadInt32(&calls); got < 2 {
		t.Fatalf("expected at least 2 calls (immediate + at least one tick), got %d", got)
	}
}

func TestRunPeriodic_OnResultCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var gotErr error
	var calls int32

	fn := func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	}

	done := make(chan struct{})
	go func() {
		RunPeriodic(ctx, time.Hour, fn, func(err error) {
			gotErr = err
			cancel()
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunPeriodic did not stop after cancel from callback")
	}

	if gotErr != nil {
		t.Fatalf("expected nil error from callback, got %v", gotErr)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly 1 call before cancel, got %d", calls)
	}
}

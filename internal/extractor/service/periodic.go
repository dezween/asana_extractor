package service

import (
	"context"
	"time"
)

// RunPeriodic invokes fn immediately, then again on every tick of interval,
// until ctx is cancelled. The caller is responsible for deciding what to do
// with each invocation's error (e.g. logging); RunPeriodic itself never
// aborts the loop on an fn error so a transient failure doesn't kill
// subsequent cycles.
func RunPeriodic(ctx context.Context, interval time.Duration, fn func(context.Context) error, onResult func(error)) {
	runOnce := func() {
		err := fn(ctx)
		if onResult != nil {
			onResult(err)
		}
	}

	runOnce()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			runOnce()
		case <-ctx.Done():
			return
		}
	}
}

package asana

import (
	"context"
	"time"
)

// rateLimiter is a minimal client-side token bucket built on time.Ticker:
// one token is added per tick, capped at the bucket size, so bursts up to
// the bucket size are allowed while the long-run rate stays bounded.
type rateLimiter struct {
	tokens chan struct{}
	ticker *time.Ticker
	done   chan struct{}
}

func newRateLimiter(ratePerMinute int) *rateLimiter {
	if ratePerMinute <= 0 {
		ratePerMinute = DefaultRatePerMinute
	}

	interval := time.Minute / time.Duration(ratePerMinute)
	rl := &rateLimiter{
		tokens: make(chan struct{}, ratePerMinute),
		ticker: time.NewTicker(interval),
		done:   make(chan struct{}),
	}

	for i := 0; i < ratePerMinute; i++ {
		rl.tokens <- struct{}{}
	}

	go rl.refill()
	return rl
}

func (rl *rateLimiter) refill() {
	for {
		select {
		case <-rl.ticker.C:
			select {
			case rl.tokens <- struct{}{}:
			default:
			}
		case <-rl.done:
			return
		}
	}
}

// wait blocks until a token is available or ctx is cancelled.
func (rl *rateLimiter) wait(ctx context.Context) error {
	select {
	case <-rl.tokens:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (rl *rateLimiter) stop() {
	rl.ticker.Stop()
	close(rl.done)
}

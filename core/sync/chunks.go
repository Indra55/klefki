package sync

import (
	"context"
	"sync"
	"time"
)

const chunkWorkers = 4

// Each window retains at most four results. Consume in index order only after
// the whole window succeeds; wait for cancelled siblings before returning.
func chunkWindows[T any](ctx context.Context, count uint64, fetch func(context.Context, uint64) (T, error), consume func(uint64, T) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for start := uint64(0); start < count; start += chunkWorkers {
		if e := ctx.Err(); e != nil {
			return e
		}
		n := min(uint64(chunkWorkers), count-start)
		results := make([]T, n)
		var wg sync.WaitGroup
		var once sync.Once
		var failure error
		for j := uint64(0); j < n; j++ {
			wg.Add(1)
			go func(j uint64) {
				defer wg.Done()
				value, e := fetch(ctx, start+j)
				if e != nil {
					once.Do(func() { failure = e; cancel() })
					return
				}
				results[j] = value
			}(j)
		}
		wg.Wait()
		if failure != nil {
			return failure
		}
		for j, value := range results {
			if e := ctx.Err(); e != nil {
				return e
			}
			if e := consume(start+uint64(j), value); e != nil {
				return e
			}
		}
	}
	return nil
}

func (c *Client) waitCooldown(ctx context.Context) error {
	for {
		c.cooldownMu.Lock()
		delay := time.Until(c.cooldown)
		c.cooldownMu.Unlock()
		if delay <= 0 {
			return ctx.Err()
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

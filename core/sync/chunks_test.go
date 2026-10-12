package sync

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestChunkWindowsBoundOrderAndCancellation(t *testing.T) {
	var active, peak atomic.Int32
	next := uint64(0)
	e := chunkWindows(context.Background(), 13, func(ctx context.Context, i uint64) (uint64, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		select {
		case <-time.After(time.Duration(4-i%4) * time.Millisecond):
			return i, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}, func(i, value uint64) error {
		if i != next || value != i {
			t.Errorf("out of order %d %d %d", next, i, value)
		}
		next++
		return nil
	})
	if e != nil || next != 13 || peak.Load() != 4 || active.Load() != 0 {
		t.Fatalf("%v consumed=%d peak=%d active=%d", e, next, peak.Load(), active.Load())
	}
	failure := errors.New("terminal")
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	go func() {
		for i := 0; i < 4; i++ {
			<-started
		}
		close(release)
	}()
	e = chunkWindows(context.Background(), 8, func(ctx context.Context, i uint64) (int, error) {
		active.Add(1)
		defer active.Add(-1)
		started <- struct{}{}
		<-release
		if i == 2 {
			return 0, failure
		}
		<-ctx.Done()
		return 0, ctx.Err()
	}, func(uint64, int) error { t.Error("incomplete window consumed"); return nil })
	if !errors.Is(e, failure) || active.Load() != 0 {
		t.Fatalf("siblings not joined: %v active=%d", e, active.Load())
	}
}

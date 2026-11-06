package xray

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitClock(t *testing.T) {
	oldNow, oldStep, oldMax := clockNow, clockWaitStep, clockWaitMax
	defer func() { clockNow, clockWaitStep, clockWaitMax = oldNow, oldStep, oldMax }()
	clockWaitStep, clockWaitMax = time.Millisecond, time.Second

	// sane clock: no wait
	clockNow = func() time.Time { return time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC) }
	start := time.Now()
	waitClock(context.Background(), t.Logf)
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("waited with a sane clock")
	}
	// 1970 at boot, NTP sets the clock after a few polls
	var calls atomic.Int32
	clockNow = func() time.Time {
		if calls.Add(1) < 5 {
			return time.Unix(10, 0)
		}
		return time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	}
	waitClock(context.Background(), t.Logf)
	if calls.Load() < 5 {
		t.Fatal("did not wait for the clock")
	}
	// never set: gives up after clockWaitMax
	clockNow = func() time.Time { return time.Unix(10, 0) }
	start = time.Now()
	waitClock(context.Background(), t.Logf)
	if d := time.Since(start); d < time.Second || d > 3*time.Second {
		t.Fatalf("give-up after %s", d)
	}
}

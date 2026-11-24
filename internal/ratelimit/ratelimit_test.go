package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestAllowIsExactUnderConcurrency(t *testing.T) {
	l := New(60, time.Minute, nil)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for range 70 {
		wg.Go(func() {
			if allowed, _ := l.Allow("client"); allowed {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 60 {
		t.Fatalf("allowed %d requests, want exactly 60", ok.Load())
	}
}

func TestWindowSlides(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	l := New(2, time.Minute, clock.Now)

	l.Allow("k")
	clock.Advance(30 * time.Second)
	l.Allow("k")

	allowed, retry := l.Allow("k")
	if allowed {
		t.Fatal("third event inside the window should be rejected")
	}
	if Seconds(retry) != 30 {
		t.Fatalf("retry after %ds, want 30s", Seconds(retry))
	}

	clock.Advance(30 * time.Second) // the first event just expired
	if allowed, _ := l.Allow("k"); !allowed {
		t.Fatal("expected a slot once the oldest event left the window")
	}
}

func TestKeysAreIndependentAndResettable(t *testing.T) {
	l := New(1, time.Hour, nil)
	l.Record("a")
	if blocked, _ := l.Exceeded("a"); !blocked {
		t.Fatal("a should be blocked")
	}
	if blocked, _ := l.Exceeded("b"); blocked {
		t.Fatal("b should not be affected by a")
	}
	l.Reset("a")
	if blocked, _ := l.Exceeded("a"); blocked {
		t.Fatal("reset should clear a")
	}
}

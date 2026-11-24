// Package ratelimit implements an in-memory sliding-window limiter.
//
// Every event is kept as a timestamp, so the window is exact rather than
// approximated with fixed buckets. That's fine at this scale; a busy public
// API would want a shared store (Redis or similar) instead.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

// Limiter allows at most Limit events per key within Window.
type Limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu     sync.Mutex
	events map[string][]time.Time
}

// New builds a limiter. now is injectable so tests can move the clock.
func New(limit int, window time.Duration, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{limit: limit, window: window, now: now, events: make(map[string][]time.Time)}
}

// Allow records an event for key if there's room and reports whether it was
// accepted. When it isn't, retryAfter says how long until a slot frees up.
// Check and record happen under one lock, so concurrent callers can never
// squeeze past the limit.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	recent := l.prune(key, now)
	if len(recent) >= l.limit {
		return false, l.wait(recent, now)
	}
	l.events[key] = append(recent, now)
	return true, 0
}

// Exceeded reports whether key has already used up the window, without
// recording anything. Login uses this together with Record, because only
// failed attempts count against the limit.
func (l *Limiter) Exceeded(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	recent := l.prune(key, now)
	if len(recent) >= l.limit {
		return true, l.wait(recent, now)
	}
	return false, 0
}

// Record adds an event for key unconditionally.
func (l *Limiter) Record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.events[key] = append(l.prune(key, now), now)
}

// Reset forgets every event for key.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.events, key)
}

// prune drops events that fell out of the window. Caller holds l.mu.
func (l *Limiter) prune(key string, now time.Time) []time.Time {
	events := l.events[key]
	cutoff := now.Add(-l.window)
	i := 0
	for i < len(events) && !events[i].After(cutoff) {
		i++
	}
	events = events[i:]
	if len(events) == 0 {
		delete(l.events, key) // keeps the map from growing with one-off keys
		return nil
	}
	l.events[key] = events
	return events
}

// wait is the time until the oldest event in the window expires.
func (l *Limiter) wait(events []time.Time, now time.Time) time.Duration {
	return events[0].Add(l.window).Sub(now)
}

// Seconds rounds a retry delay up to whole seconds, never below 1, which is
// what the Retry-After header expects.
func Seconds(d time.Duration) int {
	s := int(math.Ceil(d.Seconds()))
	if s < 1 {
		return 1
	}
	return s
}

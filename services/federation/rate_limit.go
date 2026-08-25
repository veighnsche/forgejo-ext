// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"sync"
	"time"
)

// flagRateLimiter bounds how many inbound Flag (abuse report) activities a
// single remote actor may deliver within a fixed window, so a misbehaving
// peer cannot flood the moderation queue. The window is intentionally
// generous: reports are batched by users, not generated in bulk.
var flagRateLimiter = newWindowRateLimiter(50, time.Hour)

// windowRateLimiter is a fixed-window counter keyed by an arbitrary string
// (typically a federation actor URI). Stale entries are pruned when the map
// grows, so the table cannot grow without bound.
type windowRateLimiter struct {
	limit  int
	window time.Duration

	mu    sync.Mutex
	count map[string]*windowCounter
}

type windowCounter struct {
	count    int
	windowAt time.Time
}

func newWindowRateLimiter(limit int, window time.Duration) *windowRateLimiter {
	return &windowRateLimiter{
		limit:  limit,
		window: window,
		count:  make(map[string]*windowCounter),
	}
}

// Allow records one occurrence for the key and returns true when the key has
// exceeded the configured limit within the window (i.e. the caller should
// reject the occurrence).
func (l *windowRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	c, ok := l.count[key]
	if !ok || now.Sub(c.windowAt) > l.window {
		if len(l.count) > 10_000 {
			for k, v := range l.count {
				if now.Sub(v.windowAt) > l.window {
					delete(l.count, k)
				}
			}
		}
		c = &windowCounter{count: 0, windowAt: now}
		l.count[key] = c
	}
	c.count++
	return c.count > l.limit
}

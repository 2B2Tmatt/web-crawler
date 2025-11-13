package utils

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

type Gate struct {
	delay       time.Duration
	nextAllowed time.Time
	mu          sync.Mutex
}

type RateLimiter struct {
	hosts        map[string]*Gate
	mu           sync.Mutex
	defaultDelay time.Duration
}

func InitRL() *RateLimiter {
	return &RateLimiter{make(map[string]*Gate), sync.Mutex{}, time.Second}
}

func (rl *RateLimiter) Acquire(ctx context.Context, host string) error {
	key := strings.ToLower(host)
	rl.mu.Lock()
	g, exists := rl.hosts[key]
	if exists {
		rl.mu.Unlock()
		g.mu.Lock()
		wait := time.Until(g.nextAllowed)
		if wait > 0 {
			t := time.NewTimer(wait)
			defer t.Stop()
			select {
			case <-t.C:
				log.Println("rate limit exceeded, pausing")
				g.nextAllowed = time.Now().Add(g.delay)
				g.mu.Unlock()
				return nil
			case <-ctx.Done():
				g.mu.Unlock()
				return ctx.Err()
			}
		}
		g.nextAllowed = time.Now().Add(g.delay)
		g.mu.Unlock()
		return nil
	} else {
		g = &Gate{rl.defaultDelay, time.Now().Add(rl.defaultDelay), sync.Mutex{}}
		rl.hosts[key] = g
		rl.mu.Unlock()
		return nil
	}
}

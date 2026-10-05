package harness

import (
	"sync"
	"time"
)

// Clock 是测试用的权威时钟：只在测试显式推进时前进，使租约和退避可以复现。
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

// NewClock 返回从固定时刻开始的时钟。
func NewClock() *Clock {
	return &Clock{t: time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)}
}

// Now 返回当前时间。
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance 推进时钟。
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// Set 把时钟设到 t（只向前）。
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.After(c.t) {
		c.t = t
	}
}

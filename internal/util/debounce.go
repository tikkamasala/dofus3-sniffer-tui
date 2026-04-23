package util

import (
	"sync"
	"time"
)

// Debouncer coalesces bursts of calls into a single delayed invocation.
// Call Trigger() to (re)arm the timer; the most recent Trigger wins.
type Debouncer struct {
	delay time.Duration
	fn    func()
	mu    sync.Mutex
	timer *time.Timer
}

func NewDebouncer(delay time.Duration, fn func()) *Debouncer {
	return &Debouncer{delay: delay, fn: fn}
}

func (d *Debouncer) Trigger() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.delay, d.fn)
}

func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

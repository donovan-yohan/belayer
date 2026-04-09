package broker

import (
	"sync"
	"strings"
	"time"
)

const defaultDebounceWindow = 2 * time.Second

// debouncer coalesces rapid messages within a time window before delivering them.
type debouncer struct {
	mu     sync.Mutex
	buffer []string
	timer  *time.Timer
	flush  func(coalesced string)
	window time.Duration
}

// newDebouncer creates a debouncer that calls flush with coalesced content after window elapses
// with no new additions.
func newDebouncer(window time.Duration, flush func(string)) *debouncer {
	return &debouncer{
		window: window,
		flush:  flush,
	}
}

// add appends content to the buffer and resets the debounce timer.
func (d *debouncer) add(content string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.buffer = append(d.buffer, content)

	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.window, func() {
		d.mu.Lock()
		coalesced, ok := d.drainLocked()
		d.mu.Unlock()
		if ok {
			d.flush(coalesced)
		}
	})
}

// flushNow immediately delivers any buffered content without waiting for the timer.
func (d *debouncer) flushNow() {
	d.mu.Lock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	coalesced, ok := d.drainLocked()
	d.mu.Unlock()

	if ok {
		d.flush(coalesced)
	}
}

// drainLocked coalesces the buffer and clears it. Must be called with d.mu held.
// Returns the coalesced string and true if there was anything to flush.
func (d *debouncer) drainLocked() (string, bool) {
	if len(d.buffer) == 0 {
		return "", false
	}
	coalesced := strings.Join(d.buffer, "\n\n")
	d.buffer = nil
	return coalesced, true
}

package coordinator

import (
	"sync"
	"time"
)

type retryEntry struct {
	attempt   int
	nextRetry time.Time
}

// RetryScheduler manages exponential backoff for failed leads.
type RetryScheduler struct {
	mu         sync.Mutex
	schedule   map[string]retryEntry // leadID -> entry
	baseDelay  time.Duration
	maxDelay   time.Duration
	maxRetries int
}

// NewRetryScheduler creates a scheduler with the given backoff parameters.
func NewRetryScheduler(baseDelay, maxDelay time.Duration, maxRetries int) *RetryScheduler {
	return &RetryScheduler{
		schedule:   make(map[string]retryEntry),
		baseDelay:  baseDelay,
		maxDelay:   maxDelay,
		maxRetries: maxRetries,
	}
}

// Schedule records a retry for the given lead. Returns false if max retries exceeded.
// The delay is: min(baseDelay * 2^attempt, maxDelay)
func (r *RetryScheduler) Schedule(leadID string, attempt int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if attempt >= r.maxRetries {
		return false
	}

	delay := r.baseDelay
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay > r.maxDelay {
			delay = r.maxDelay
			break
		}
	}

	r.schedule[leadID] = retryEntry{
		attempt:   attempt,
		nextRetry: time.Now().Add(delay),
	}
	return true
}

// Ready returns true if the lead's retry delay has elapsed and it's eligible for retry.
func (r *RetryScheduler) Ready(leadID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.schedule[leadID]
	if !ok {
		return false
	}
	return time.Now().After(entry.nextRetry) || time.Now().Equal(entry.nextRetry)
}

// ReadyLeads returns all lead IDs that are ready for retry.
func (r *RetryScheduler) ReadyLeads() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	var ready []string
	for id, entry := range r.schedule {
		if now.After(entry.nextRetry) || now.Equal(entry.nextRetry) {
			ready = append(ready, id)
		}
	}
	return ready
}

// Attempt returns the current attempt count for a lead. Returns 0 if not scheduled.
func (r *RetryScheduler) Attempt(leadID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.schedule[leadID]
	if !ok {
		return 0
	}
	return entry.attempt
}

// Remove clears the retry entry for a lead (called after successful retry or permanent failure).
func (r *RetryScheduler) Remove(leadID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.schedule, leadID)
}

// Has returns true if the lead has a scheduled retry.
func (r *RetryScheduler) Has(leadID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, ok := r.schedule[leadID]
	return ok
}

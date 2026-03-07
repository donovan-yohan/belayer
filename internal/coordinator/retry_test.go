package coordinator

import (
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduleAndReady(t *testing.T) {
	// Use a very short base delay so the retry becomes ready almost immediately.
	rs := NewRetryScheduler(1*time.Millisecond, 1*time.Second, 5)

	ok := rs.Schedule("lead-1", 0)
	require.True(t, ok, "Schedule should succeed for attempt 0")
	require.True(t, rs.Has("lead-1"), "lead-1 should be scheduled")

	// Wait for the delay to elapse.
	time.Sleep(5 * time.Millisecond)

	assert.True(t, rs.Ready("lead-1"), "lead-1 should be ready after delay elapsed")
}

func TestNotReadyBeforeDelay(t *testing.T) {
	// Use a long delay so it will not be ready during the test.
	rs := NewRetryScheduler(10*time.Second, 1*time.Minute, 5)

	ok := rs.Schedule("lead-1", 0)
	require.True(t, ok)

	assert.False(t, rs.Ready("lead-1"), "lead-1 should not be ready before delay elapses")
}

func TestReadyReturnsFalseForUnknownLead(t *testing.T) {
	rs := NewRetryScheduler(1*time.Second, 1*time.Minute, 5)
	assert.False(t, rs.Ready("nonexistent"), "Ready should return false for unknown lead")
}

func TestExponentialBackoff(t *testing.T) {
	baseDelay := 5 * time.Second
	maxDelay := 5 * time.Minute
	rs := NewRetryScheduler(baseDelay, maxDelay, 10)

	// Expected delays: 5s, 10s, 20s, 40s, 80s, 160s, 300s (capped), 300s, ...
	expectedDelays := []time.Duration{
		5 * time.Second,
		10 * time.Second,
		20 * time.Second,
		40 * time.Second,
		80 * time.Second,
		160 * time.Second,
		300 * time.Second, // capped at maxDelay
		300 * time.Second,
	}

	for i, expected := range expectedDelays {
		before := time.Now()
		ok := rs.Schedule("lead-backoff", i)
		after := time.Now()
		require.True(t, ok, "Schedule should succeed for attempt %d", i)

		entry := rs.schedule["lead-backoff"]
		// The nextRetry should be approximately now + expected delay.
		// Allow some tolerance for the time between before/after.
		low := before.Add(expected)
		high := after.Add(expected)

		assert.True(t, !entry.nextRetry.Before(low),
			"attempt %d: nextRetry %v should be >= %v (delay %v)", i, entry.nextRetry, low, expected)
		assert.True(t, !entry.nextRetry.After(high),
			"attempt %d: nextRetry %v should be <= %v (delay %v)", i, entry.nextRetry, high, expected)

		// Clean up for next iteration so the entry is fresh.
		rs.Remove("lead-backoff")
	}
}

func TestMaxRetriesExceeded(t *testing.T) {
	rs := NewRetryScheduler(1*time.Second, 1*time.Minute, 3)

	// Attempts 0, 1, 2 should succeed (maxRetries=3 means attempts 0..2 are allowed).
	assert.True(t, rs.Schedule("lead-1", 0))
	assert.True(t, rs.Schedule("lead-1", 1))
	assert.True(t, rs.Schedule("lead-1", 2))

	// Attempt 3 should fail since it equals maxRetries.
	ok := rs.Schedule("lead-1", 3)
	assert.False(t, ok, "Schedule should return false when attempt >= maxRetries")
}

func TestReadyLeads(t *testing.T) {
	rs := NewRetryScheduler(1*time.Millisecond, 1*time.Second, 5)

	// Schedule three leads: two with tiny delay, one with huge delay.
	rs.Schedule("lead-fast-1", 0)
	rs.Schedule("lead-fast-2", 0)

	// Override lead-slow to have a far-future retry time.
	rs.mu.Lock()
	rs.schedule["lead-slow"] = retryEntry{
		attempt:   0,
		nextRetry: time.Now().Add(1 * time.Hour),
	}
	rs.mu.Unlock()

	// Wait for the fast leads' delays to elapse.
	time.Sleep(5 * time.Millisecond)

	ready := rs.ReadyLeads()
	sort.Strings(ready)

	require.Len(t, ready, 2, "expected exactly 2 ready leads")
	assert.Equal(t, "lead-fast-1", ready[0])
	assert.Equal(t, "lead-fast-2", ready[1])
}

func TestReadyLeadsEmpty(t *testing.T) {
	rs := NewRetryScheduler(1*time.Second, 1*time.Minute, 5)

	ready := rs.ReadyLeads()
	assert.Empty(t, ready, "ReadyLeads should return empty slice when nothing is scheduled")
}

func TestRemove(t *testing.T) {
	rs := NewRetryScheduler(1*time.Millisecond, 1*time.Second, 5)

	rs.Schedule("lead-1", 0)
	require.True(t, rs.Has("lead-1"))

	rs.Remove("lead-1")

	assert.False(t, rs.Has("lead-1"), "Has should return false after Remove")
	assert.False(t, rs.Ready("lead-1"), "Ready should return false after Remove")
	assert.Equal(t, 0, rs.Attempt("lead-1"), "Attempt should return 0 after Remove")
}

func TestRemoveNonexistent(t *testing.T) {
	rs := NewRetryScheduler(1*time.Second, 1*time.Minute, 5)
	// Should not panic.
	rs.Remove("nonexistent")
}

func TestAttempt(t *testing.T) {
	rs := NewRetryScheduler(1*time.Millisecond, 1*time.Second, 10)

	assert.Equal(t, 0, rs.Attempt("lead-1"), "Attempt should return 0 for unscheduled lead")

	rs.Schedule("lead-1", 3)
	assert.Equal(t, 3, rs.Attempt("lead-1"), "Attempt should return the scheduled attempt number")

	rs.Schedule("lead-1", 7)
	assert.Equal(t, 7, rs.Attempt("lead-1"), "Attempt should reflect the latest Schedule call")
}

func TestHas(t *testing.T) {
	rs := NewRetryScheduler(1*time.Second, 1*time.Minute, 5)

	assert.False(t, rs.Has("lead-1"), "Has should return false for unscheduled lead")

	rs.Schedule("lead-1", 0)
	assert.True(t, rs.Has("lead-1"), "Has should return true after Schedule")

	rs.Remove("lead-1")
	assert.False(t, rs.Has("lead-1"), "Has should return false after Remove")
}

func TestMaxDelayCap(t *testing.T) {
	baseDelay := 1 * time.Second
	maxDelay := 10 * time.Second
	rs := NewRetryScheduler(baseDelay, maxDelay, 100)

	// attempt 0: 1s, 1: 2s, 2: 4s, 3: 8s, 4: 16s -> capped to 10s
	// Schedule at a very high attempt to ensure capping.
	before := time.Now()
	ok := rs.Schedule("lead-cap", 20)
	after := time.Now()
	require.True(t, ok)

	entry := rs.schedule["lead-cap"]
	// With attempt=20 the raw delay would be 1s * 2^20 = 1048576s, but capped at 10s.
	low := before.Add(maxDelay)
	high := after.Add(maxDelay)

	assert.True(t, !entry.nextRetry.Before(low),
		"nextRetry %v should be >= %v (capped at maxDelay)", entry.nextRetry, low)
	assert.True(t, !entry.nextRetry.After(high),
		"nextRetry %v should be <= %v (capped at maxDelay)", entry.nextRetry, high)
}

func TestConcurrentAccess(t *testing.T) {
	rs := NewRetryScheduler(1*time.Millisecond, 1*time.Second, 100)

	var wg sync.WaitGroup
	numGoroutines := 50

	// Concurrently schedule, check, and remove leads.
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			leadID := "lead-concurrent"

			// Each goroutine does a mix of operations.
			rs.Schedule(leadID, id%10)
			rs.Has(leadID)
			rs.Ready(leadID)
			rs.Attempt(leadID)
			rs.ReadyLeads()
			if id%3 == 0 {
				rs.Remove(leadID)
			}
		}(i)
	}

	// Also schedule distinct leads concurrently.
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			leadID := "lead-" + string(rune('A'+id%26))
			rs.Schedule(leadID, 0)
			rs.Ready(leadID)
			rs.ReadyLeads()
		}(i)
	}

	wg.Wait()
	// If we get here without a race or deadlock, the test passes.
}

func TestScheduleOverwritesPrevious(t *testing.T) {
	rs := NewRetryScheduler(1*time.Second, 1*time.Minute, 10)

	rs.Schedule("lead-1", 2)
	assert.Equal(t, 2, rs.Attempt("lead-1"))

	rs.Schedule("lead-1", 5)
	assert.Equal(t, 5, rs.Attempt("lead-1"), "Schedule should overwrite previous entry")
}

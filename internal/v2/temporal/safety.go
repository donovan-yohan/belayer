package temporal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

// SafetyTracker enforces pipeline-wide safety limits within a workflow.
type SafetyTracker struct {
	mu            sync.Mutex
	maxDepth      int
	globalBudget  int
	dedupeEnabled bool
	childCount    int
	seenHashes    map[string]bool
}

// NewSafetyTracker creates a tracker with the given limits.
func NewSafetyTracker(maxDepth, globalBudget int, dedupe bool) *SafetyTracker {
	return &SafetyTracker{
		maxDepth:      maxDepth,
		globalBudget:  globalBudget,
		dedupeEnabled: dedupe,
		seenHashes:    make(map[string]bool),
	}
}

// CanSpawnChild checks if a child work item is allowed under safety limits.
// Returns an error describing the violation, or nil if allowed.
func (s *SafetyTracker) CanSpawnChild(depth int, input json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if depth >= s.maxDepth {
		return fmt.Errorf("max child depth reached (%d/%d)", depth, s.maxDepth)
	}

	if s.childCount >= s.globalBudget {
		return fmt.Errorf("global child budget exhausted (%d/%d)", s.childCount, s.globalBudget)
	}

	if s.dedupeEnabled && input != nil {
		hash := hashJSON(input)
		if s.seenHashes[hash] {
			return fmt.Errorf("duplicate child work item (hash: %s)", hash[:8])
		}
		s.seenHashes[hash] = true
	}

	s.childCount++
	return nil
}

// ChildCount returns the current number of spawned children.
func (s *SafetyTracker) ChildCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.childCount
}

func hashJSON(data json.RawMessage) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

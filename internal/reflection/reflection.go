package reflection

import (
	"fmt"
	"strings"
	"sync"

	"github.com/donovan-yohan/belayer/internal/memory"
	"github.com/donovan-yohan/belayer/internal/store"
)

// Reflector is the sleep-time compute agent for belayer. It runs asynchronously
// with full write access to the memory store, consolidating session core memory
// into clean, concise archival entries.
//
// Consolidation is deterministic string formatting for MVP — LLM-powered
// reflection will be added in Phase 4.
type Reflector struct {
	mu     sync.Mutex // ensures serial reflection queue
	memory memory.MemoryStore
	store  *store.Store
	memfs  *memory.MemFS // can be nil (skip markdown writes)
}

// New creates a Reflector backed by the given memory store and session store.
// fs may be nil, in which case markdown writes are skipped.
func New(mem memory.MemoryStore, st *store.Store, fs *memory.MemFS) *Reflector {
	return &Reflector{
		memory: mem,
		store:  st,
		memfs:  fs,
	}
}

// Reflect consolidates the core memory for the given session into a single
// archival entry. It acquires the mutex so only one reflection runs at a time.
//
// Steps:
//  1. Acquire mutex (serial queue).
//  2. Read all core entries for the session.
//  3. Read all session events from the store.
//  4. Consolidate core entries into a structured archival entry.
//  5. Write the archival entry with source = "reflection:<sessionID>".
//
// Returns nil when there are no core entries (no-op).
func (r *Reflector) Reflect(sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := r.memory.ReadCore(sessionID)
	if err != nil {
		return fmt.Errorf("reflection: read core for session %q: %w", sessionID, err)
	}

	if len(entries) == 0 {
		// Nothing to consolidate.
		return nil
	}

	// Read session events — used for future LLM-powered reflection phases.
	// For MVP we read them to establish the full context but don't incorporate
	// them into the deterministic consolidation output.
	_, err = r.store.QueryEvents(sessionID)
	if err != nil {
		return fmt.Errorf("reflection: query events for session %q: %w", sessionID, err)
	}

	content := r.Consolidate(entries)

	// Derive tags from entry keys.
	tags := deriveTagsFromEntries(entries)

	source := "reflection:" + sessionID
	if err := r.memory.WriteArchival(sessionID, content, tags, source); err != nil {
		return fmt.Errorf("reflection: write archival for session %q: %w", sessionID, err)
	}

	// Write markdown layer if MemFS is configured.
	if r.memfs != nil {
		// Write core entries to core.md for this session (repo = sessionID).
		if err := r.memfs.WriteCoreFile(sessionID, entries); err != nil {
			return fmt.Errorf("reflection: write core file for session %q: %w", sessionID, err)
		}

		// Write consolidated archival entry to the "reflection" topic file.
		archEntry := memory.ArchivalEntry{
			SessionID: sessionID,
			Content:   content,
			Tags:      tags,
			Source:    source,
		}
		if err := r.memfs.WriteArchivalFile(sessionID, "reflection", archEntry); err != nil {
			return fmt.Errorf("reflection: write archival file for session %q: %w", sessionID, err)
		}
	}

	return nil
}

// DetectStale returns archival entries that have not been referenced in recent
// session events. An entry is considered stale when none of the most recent
// maxSessionsWithoutReference sessions' events contain the first 50 characters
// of the entry's content.
//
// For MVP this is a simplified heuristic — the first 50 chars of content are
// searched across all events to determine recency of reference.
func (r *Reflector) DetectStale(maxSessionsWithoutReference int) ([]memory.ArchivalEntry, error) {
	archival, err := r.memory.ListArchival(0)
	if err != nil {
		return nil, fmt.Errorf("reflection: list archival for stale detection: %w", err)
	}

	if len(archival) == 0 {
		return []memory.ArchivalEntry{}, nil
	}

	// Collect all recent events across sessions.
	sessions, err := r.store.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("reflection: list sessions for stale detection: %w", err)
	}

	// Gather events from up to maxSessionsWithoutReference most-recent sessions.
	limit := maxSessionsWithoutReference
	if limit > len(sessions) {
		limit = len(sessions)
	}

	var allEventData []string
	for _, sess := range sessions[:limit] {
		events, err := r.store.QueryEvents(sess.ID)
		if err != nil {
			continue
		}
		for _, evt := range events {
			allEventData = append(allEventData, evt.Data, evt.Type)
		}
	}

	var stale []memory.ArchivalEntry
	for _, entry := range archival {
		snippet := entry.Content
		if len(snippet) > 50 {
			snippet = snippet[:50]
		}
		if snippet == "" {
			continue
		}

		referenced := false
		for _, eventText := range allEventData {
			if strings.Contains(eventText, snippet) {
				referenced = true
				break
			}
		}

		if !referenced {
			stale = append(stale, entry)
		}
	}

	if stale == nil {
		stale = []memory.ArchivalEntry{}
	}
	return stale, nil
}

// Consolidate takes a list of core entries and produces a consolidated markdown
// string suitable for archival storage.
//
// Format:
//
//	## Session Memory
//
//	- **key**: value
func (r *Reflector) Consolidate(entries []memory.CoreEntry) string {
	if len(entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Session Memory\n\n")
	for _, e := range entries {
		fmt.Fprintf(&sb, "- **%s**: %s\n", e.Key, e.Value)
	}
	return sb.String()
}

// deriveTagsFromEntries builds a comma-separated tag string from entry keys.
func deriveTagsFromEntries(entries []memory.CoreEntry) string {
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		keys = append(keys, e.Key)
	}
	return strings.Join(keys, ",")
}

package memory

import "time"

// CoreEntry is a key-value pair scoped to a session. Core memory is always
// in context — small, frequently updated, and keyed for fast lookup.
type CoreEntry struct {
	ID        int64
	SessionID string
	Key       string
	Value     string
	UpdatedAt time.Time
}

// ArchivalEntry is a long-term storage record scoped to a session. Archival
// memory is append-mostly and full-text searchable via FTS5.
//
// The Source field records the markdown file path that is the authoritative
// source for this entry. The FTS5 index is a derived index that can be
// rebuilt from the source files — "markdown is authoritative".
type ArchivalEntry struct {
	ID        int64
	SessionID string
	Content   string
	Tags      string // comma-separated
	Source    string // source file path (markdown is authoritative)
	CreatedAt time.Time
}

// RecallResult combines core memory for a session with archival search
// results for a query.
type RecallResult struct {
	Core     []CoreEntry
	Archival []ArchivalEntry
}

// MemoryStore is the interface for the three-tier memory system.
type MemoryStore interface {
	// WriteCore upserts a key-value pair for the given session.
	WriteCore(sessionID, key, value string) error

	// ReadCore returns all core entries for a session ordered by key.
	ReadCore(sessionID string) ([]CoreEntry, error)

	// WriteArchival appends a new archival entry for the given session.
	WriteArchival(sessionID, content, tags, source string) error

	// SearchArchival performs a full-text search across all archival entries,
	// limited to limit results ordered by relevance rank.
	SearchArchival(query string, limit int) ([]ArchivalEntry, error)

	// ListArchival returns all archival entries ordered by created_at ASC,
	// limited to limit results. Pass 0 for no limit.
	ListArchival(limit int) ([]ArchivalEntry, error)

	// Recall combines ReadCore for the session with SearchArchival for the query.
	Recall(sessionID, query string) (RecallResult, error)

	// Close releases underlying resources.
	Close() error
}

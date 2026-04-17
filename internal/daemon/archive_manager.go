package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/donovan-yohan/belayer/internal/archive"
	"github.com/donovan-yohan/belayer/internal/store"
)

// archiveManager coordinates session archiving for a daemon.
//
// Two flows:
//  1. Terminal-transition: ArchiveTerminal fires asynchronously when a
//     session transitions to a terminal status (from handleUpdateSession).
//     The written manifest carries partial=false.
//  2. Shutdown drain: DrainAll runs synchronously from drainArchive. For
//     every non-terminal session, it writes a snapshot archive with
//     partial=true, then waits for in-flight terminal archives to finish
//     up to the supplied context's deadline.
type archiveManager struct {
	store    *store.Store
	inflight sync.WaitGroup

	// stopping is set at the start of DrainAll. Terminal archives that arrive
	// after stopping is true are dropped (drain has already started; we can't
	// guarantee they finish within budget).
	stopping atomic.Bool

	// seen dedupes ArchiveTerminal calls per session so racy status flips
	// (terminal -> non-terminal -> terminal) don't spawn multiple writers.
	seenMu sync.Mutex
	seen   map[string]bool
}

func newArchiveManager(st *store.Store) *archiveManager {
	return &archiveManager{
		store: st,
		seen:  make(map[string]bool),
	}
}

// ArchiveTerminal spawns an async archive for a session that just transitioned
// to a terminal status. Safe to call concurrently; deduped per session.
func (m *archiveManager) ArchiveTerminal(sessionID string) {
	if m.stopping.Load() {
		return
	}
	m.seenMu.Lock()
	if m.seen[sessionID] {
		m.seenMu.Unlock()
		return
	}
	m.seen[sessionID] = true
	m.seenMu.Unlock()

	m.inflight.Add(1)
	go func() {
		defer m.inflight.Done()
		if err := m.doArchive(sessionID, false); err != nil {
			log.Printf("archive: terminal archive %s: %v", sessionID, err)
		}
	}()
}

// DrainAll runs the shutdown archive drain:
//  1. Sets stopping so late ArchiveTerminal calls are dropped.
//  2. Archives every non-terminal session synchronously with partial=true.
//  3. Waits for in-flight terminal archives to finish or ctx to expire.
//
// Returns true if all archives finished before ctx expiry, false on timeout.
func (m *archiveManager) DrainAll(ctx context.Context) bool {
	m.stopping.Store(true)

	sessions, err := m.store.ListSessions()
	if err != nil {
		log.Printf("archive: drain list sessions: %v", err)
	} else {
		for _, sess := range sessions {
			if isTerminalSessionStatus(sess.Status) {
				continue
			}
			if err := m.doArchive(sess.ID, true); err != nil {
				log.Printf("archive: drain archive %s: %v", sess.ID, err)
			}
		}
	}

	done := make(chan struct{})
	go func() {
		m.inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		log.Printf("archive: drain timeout; some archives may be incomplete")
		return false
	}
}

// addForTest injects a blocked goroutine into the WaitGroup without ever
// calling Done. Tests use this to exercise DrainAll timeout behaviour without
// exporting internal state. The returned release func must be called to unblock
// the goroutine (so the test doesn't leak goroutines on cleanup).
func (m *archiveManager) addForTest() (release func()) {
	ch := make(chan struct{})
	m.inflight.Add(1)
	go func() {
		<-ch
		m.inflight.Done()
	}()
	return func() { close(ch) }
}

// doArchive loads session state and writes the archive. destDir is
// <workspace>/.belayer/archive/<session-id>/; sessions without a workspace
// are skipped (logged, not errored).
func (m *archiveManager) doArchive(sessionID string, partial bool) error {
	sess, err := m.store.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	if sess.WorkspaceDir == "" {
		log.Printf("archive: session %s has no workspace; skipping", sessionID)
		return nil
	}

	agents, err := m.store.ListAgentRuns(sessionID)
	if err != nil {
		return fmt.Errorf("list agent runs: %w", err)
	}

	rawEvents, err := m.store.QueryEvents(sessionID)
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}

	roster := make([]archive.AgentInfo, len(agents))
	for i, a := range agents {
		roster[i] = archive.AgentInfo{Name: a.Name, Role: a.Role, Profile: a.Profile}
	}

	events := make([]archive.Event, len(rawEvents))
	for i, e := range rawEvents {
		events[i] = archive.Event{
			ID:        e.ID,
			SessionID: e.SessionID,
			Timestamp: e.Timestamp,
			Type:      e.Type,
			Data:      json.RawMessage(e.Data),
		}
	}

	artifacts, skipped := archive.ExtractArtifacts(events)
	if skipped > 0 {
		log.Printf("archive: session %s: %d artifact_created event(s) had unparseable data",
			sessionID, skipped)
	}

	destDir := filepath.Join(sess.WorkspaceDir, ".belayer", "archive", sessionID)
	meta := archive.Meta{
		SchemaVersion: "belayer-log/v1",
		// TODO(phase-5): populate DaemonInstanceID from GET /health capabilities.
		DaemonInstanceID: "",
		Session: archive.SessionMeta{
			ID:        sess.ID,
			Name:      sess.Name,
			Workspace: sess.WorkspaceDir,
		},
		AgentRoster: roster,
		Artifacts:   artifacts,
		FinalStatus: sess.Status,
		Partial:     partial,
		ArchivedAt:  time.Now().UTC(),
	}
	_, err = archive.Write(destDir, meta, events)
	return err
}

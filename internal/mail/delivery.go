package mail

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/donovan-yohan/belayer/internal/tmux"
)

const (
	sendKeysChunkSize = 512
	nudgeLockTimeout  = 30 * time.Second
	nudgeReadyTimeout = 10 * time.Second
)

// Per-session nudge locks to prevent interleaving.
var sessionNudgeLocks sync.Map

func getSessionNudgeSem(session string) chan struct{} {
	sem := make(chan struct{}, 1)
	actual, _ := sessionNudgeLocks.LoadOrStore(session, sem)
	return actual.(chan struct{})
}

func acquireNudgeLock(session string, timeout time.Duration) bool {
	sem := getSessionNudgeSem(session)
	select {
	case sem <- struct{}{}:
		return true
	case <-time.After(timeout):
		return false
	}
}

func releaseNudgeLock(session string) {
	sem := getSessionNudgeSem(session)
	select {
	case <-sem:
	default:
	}
}

// SanitizeMessage removes control characters that corrupt tmux send-keys delivery.
func SanitizeMessage(msg string) string {
	var b strings.Builder
	b.Grow(len(msg))
	for _, r := range msg {
		switch {
		case r == '\t':
			b.WriteRune(' ')
		case r == '\n':
			b.WriteRune(r)
		case r < 0x20:
			continue
		case r == 0x7f:
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ChunkMessage splits a message into chunks of at most chunkSize bytes.
func ChunkMessage(msg string, chunkSize int) []string {
	if len(msg) <= chunkSize {
		return []string{msg}
	}
	var chunks []string
	for i := 0; i < len(msg); i += chunkSize {
		end := i + chunkSize
		if end > len(msg) {
			end = len(msg)
		}
		chunks = append(chunks, msg[i:end])
	}
	return chunks
}

// NudgeSession sends a short notification to a tmux session.
// Applies all gastown-derived reliability patterns.
func NudgeSession(tm tmux.TmuxManager, session, window, message string) error {
	target := session + ":" + window

	// Serialize nudges to prevent interleaving
	if !acquireNudgeLock(session, nudgeLockTimeout) {
		return fmt.Errorf("nudge lock timeout for session %q", session)
	}
	defer releaseNudgeLock(session)

	// Sanitize
	sanitized := SanitizeMessage(message)

	// Send text chunks via send-keys -l
	chunks := ChunkMessage(sanitized, sendKeysChunkSize)
	for i, chunk := range chunks {
		if err := tm.SendKeysLiteral(target, chunk); err != nil {
			return fmt.Errorf("sending chunk %d: %w", i, err)
		}
		if i < len(chunks)-1 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Wait for text delivery
	time.Sleep(500 * time.Millisecond)

	// Send Escape (for vim mode)
	_ = tm.SendKeysRaw(target, "Escape")

	// Wait 600ms — exceeds bash readline's 500ms keyseq-timeout
	time.Sleep(600 * time.Millisecond)

	// Send Enter with retry
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		if err := tm.SendKeysRaw(target, "Enter"); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("failed to send Enter after 3 attempts: %w", lastErr)
}

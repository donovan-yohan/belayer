package mail

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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

// ChunkMessage splits a message into chunks of at most chunkSize bytes,
// respecting UTF-8 rune boundaries to avoid corrupting multi-byte characters.
func ChunkMessage(msg string, chunkSize int) []string {
	if len(msg) <= chunkSize {
		return []string{msg}
	}
	var chunks []string
	for len(msg) > 0 {
		end := chunkSize
		if end > len(msg) {
			end = len(msg)
		} else {
			// Walk back to a rune boundary
			for end > 0 && !utf8.RuneStart(msg[end]) {
				end--
			}
			if end == 0 {
				end = chunkSize // single rune larger than chunk (shouldn't happen)
			}
		}
		chunks = append(chunks, msg[:end])
		msg = msg[end:]
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

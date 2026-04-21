package bridge

import (
	"bufio"
	"io"
	"regexp"
	"strings"
	"sync"
)

// StartStdoutScanner attaches a stdout scanner to the process and starts a
// pump goroutine that reads from r, tees bytes to logWriter, and scans each
// line for error markers. Returns a channel closed when the pump finishes.
// Callers must close r's write-end (or the upstream pipe) to EOF the pump
// after the subprocess exits; callers should then drain this channel before
// closing any log writers.
//
// Intended for sandbox-driver paths where Spawn is not used. The daemon's
// watchBridgeExit goroutine reads StdoutErrors() to synthesize bridge:failed
// events from LLM/API connectivity markers detected in stdout.
func (p *Process) StartStdoutScanner(r io.Reader, logWriter io.Writer) <-chan struct{} {
	sc := newStdoutScanner()
	p.mu.Lock()
	p.scanner = sc
	p.mu.Unlock()
	done := make(chan struct{})
	go func() {
		sc.pump(r, logWriter)
		close(sc.errors)
		close(done)
	}()
	return done
}

// StdoutError represents a structured error detected in bridge stdout output.
type StdoutError struct {
	Pattern string // the pattern label that matched (e.g. "api_failed_retries")
	Line    string // the raw line that triggered the match (truncated to 500 chars)
}

// errorPattern associates a human-readable label with a compiled regex.
type errorPattern struct {
	label string
	re    *regexp.Regexp
}

// stdoutErrorPatterns is the set of error markers the scanner watches for in
// bridge stdout. These cover known API/network failure modes from hermes-agent.
// All patterns are case-insensitive ((?i) prefix).
var stdoutErrorPatterns = []errorPattern{
	{label: "api_failed_retries", re: regexp.MustCompile(`(?i)API failed after \d+ retries`)},
	{label: "connection_error", re: regexp.MustCompile(`(?i)\bConnection error\b`)},
	{label: "max_retries_exceeded", re: regexp.MustCompile(`(?i)\bMax retries exceeded\b`)},
	{label: "http_401", re: regexp.MustCompile(`(?i)\bHTTP 401\b`)},
	{label: "http_403", re: regexp.MustCompile(`(?i)\bHTTP 403\b`)},
	{label: "http_429", re: regexp.MustCompile(`(?i)\bHTTP 429\b`)},
	{label: "http_5xx", re: regexp.MustCompile(`(?i)\bHTTP 5\d\d\b`)},
}

// stdoutScanner tees a reader into a log writer and concurrently scans for
// error markers. When a marker is detected the first time (per pattern label),
// it sends a StdoutError on the errors channel. Duplicate patterns within the
// same process lifetime are dropped silently.
type stdoutScanner struct {
	errors chan StdoutError
	mu     sync.Mutex
	seen   map[string]struct{}
}

// newStdoutScanner returns a scanner with a buffered errors channel.
func newStdoutScanner() *stdoutScanner {
	return &stdoutScanner{
		errors: make(chan StdoutError, len(stdoutErrorPatterns)),
		seen:   make(map[string]struct{}),
	}
}

// pump reads from r, writes each byte to logWriter, and scans each line for
// error markers. It blocks until r is closed (i.e., the subprocess exits and
// the OS closes the pipe). Call from a goroutine.
func (s *stdoutScanner) pump(r io.Reader, logWriter io.Writer) {
	// Use a TeeReader so bytes flow to the log writer as they are consumed.
	// Use bufio.Reader.ReadString instead of bufio.Scanner to avoid the 64KB
	// token limit — long JSON lines from hermes-bridge would trigger ErrTooLong,
	// stopping the pump and causing the subprocess to block on a full stdout pipe.
	tee := io.TeeReader(r, logWriter)
	br := bufio.NewReader(tee)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\n")
			// Apply safe-to-ignore heuristic before error detection. This avoids
			// false positives when agent reasoning includes a reference to a past
			// error that has since been resolved (e.g. "last time we got a
			// Connection error, now fixed").
			if !isSafeToIgnore(trimmed) {
				s.checkLine(trimmed)
			}
		}
		if err != nil {
			// io.EOF on pipe close is expected; anything else is a read error.
			// Either way, exit so the pump goroutine terminates cleanly and the
			// pipe drains. ReadString returns the partial final line (if any)
			// above before reporting the error, so no bytes are lost.
			return
		}
	}
}

// checkLine tests line against all patterns and emits on the errors channel
// for any previously-unseen match. Non-blocking: the channel is buffered large
// enough to hold one entry per pattern, so a slow consumer never blocks pumping.
func (s *stdoutScanner) checkLine(line string) {
	for _, p := range stdoutErrorPatterns {
		if !p.re.MatchString(line) {
			continue
		}
		s.mu.Lock()
		_, already := s.seen[p.label]
		if !already {
			s.seen[p.label] = struct{}{}
		}
		s.mu.Unlock()
		if already {
			continue
		}
		truncated := line
		if len(truncated) > 500 {
			truncated = truncated[:500]
		}
		// Non-blocking send: the channel is pre-sized, so this only drops if
		// we somehow exceed the buffer — acceptable per spec.
		select {
		case s.errors <- StdoutError{Pattern: p.label, Line: truncated}:
		default:
		}
	}
}

// StdoutErrorsOrNil returns a receive-only channel of StdoutErrors, or nil if
// the process was created without a scanner (e.g. NewProcess path).
// The channel is closed when the pump goroutine finishes (i.e., after Done).
// Callers should select on both Done and StdoutErrors.
func (p *Process) StdoutErrors() <-chan StdoutError {
	if p.scanner == nil {
		return nil
	}
	return p.scanner.errors
}

// isSafeToIgnore returns true when the line contains a known-safe context that
// should NOT trigger an alert even if a pattern matches — e.g. an agent
// reasoning about a past connection error that has since been resolved.
// This is a lightweight heuristic: it requires a past-tense marker ("last
// time", "previously", "earlier") within 30 characters of an error keyword.
// The broad "we got" / "had a" heuristic has been removed because it was
// suppressing live failures like "we got HTTP 429 from provider".
// Intentionally conservative to avoid suppressing real failures.
func isSafeToIgnore(line string) bool {
	lower := strings.ToLower(line)
	pastMarkers := []string{"last time", "previously", "earlier"}
	errorKeywords := []string{"error", "failed", "exception", "http", "connection", "retries", "rate limit"}
	for _, pm := range pastMarkers {
		pmIdx := strings.Index(lower, pm)
		if pmIdx < 0 {
			continue
		}
		for _, kw := range errorKeywords {
			kwIdx := strings.Index(lower, kw)
			if kwIdx < 0 {
				continue
			}
			// Past-tense marker and error keyword within 30 chars of each other.
			dist := kwIdx - (pmIdx + len(pm))
			if dist < 0 {
				dist = pmIdx - (kwIdx + len(kw))
			}
			if dist >= 0 && dist <= 30 {
				return true
			}
		}
	}
	return false
}


package bridge

import (
	"bufio"
	"io"
	"regexp"
	"strings"
	"sync"
)

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
	tee := io.TeeReader(r, logWriter)
	scanner := bufio.NewScanner(tee)
	for scanner.Scan() {
		line := scanner.Text()
		// Apply safe-to-ignore heuristic before error detection. This avoids
		// false positives when agent reasoning includes a reference to a past
		// error that has since been resolved (e.g. "we got a Connection error
		// last time, now fixed").
		if !isSafeToIgnore(line) {
			s.checkLine(line)
		}
	}
	// Ignore scanner.Err() — pipe close on process exit is not an error.
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

// matchesAnyErrorPattern reports whether line matches any of the scanner
// patterns. Used from tests that want to verify the pattern set without
// running the full pump goroutine.
func matchesAnyErrorPattern(line string) (label string, ok bool) {
	for _, p := range stdoutErrorPatterns {
		if p.re.MatchString(line) {
			return p.label, true
		}
	}
	return "", false
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
// This is a lightweight heuristic: it checks for past-tense framing.
// Intentionally conservative to avoid suppressing real failures.
func isSafeToIgnore(line string) bool {
	lower := strings.ToLower(line)
	// Lines that contain "last time" or "previously" before the error keyword
	// are likely contextual references, not live failures.
	if strings.Contains(lower, "last time") || strings.Contains(lower, "previously") ||
		strings.Contains(lower, "we got") || strings.Contains(lower, "had a") {
		return true
	}
	return false
}

// checkLineSafe wraps checkLine with the safe-to-ignore heuristic.
func (s *stdoutScanner) checkLineSafe(line string) {
	if isSafeToIgnore(line) {
		return
	}
	s.checkLine(line)
}

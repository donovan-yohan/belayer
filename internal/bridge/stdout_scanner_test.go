package bridge

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestStdoutScannerDetectsAllPatterns verifies that each listed error marker
// is detected by the scanner and emits a StdoutError with the correct label.
func TestStdoutScannerDetectsAllPatterns(t *testing.T) {
	cases := []struct {
		label string
		line  string
	}{
		{"api_failed_retries", "API failed after 3 retries — Connection error."},
		{"connection_error", "httpx.ConnectError: Connection error"},
		{"max_retries_exceeded", "urllib3.exceptions.MaxError: Max retries exceeded"},
		{"http_401", "Server returned HTTP 401 Unauthorized"},
		{"http_403", "Server returned HTTP 403 Forbidden"},
		{"http_429", "Rate limit hit: HTTP 429 Too Many Requests"},
		{"http_5xx", "Upstream returned HTTP 502 Bad Gateway"},
		{"http_5xx", "Got HTTP 500 Internal Server Error"},
	}

	for _, tc := range cases {
		t.Run(tc.label+"_"+tc.line[:min(30, len(tc.line))], func(t *testing.T) {
			sc := newStdoutScanner()
			var buf bytes.Buffer
			sc.pump(strings.NewReader(tc.line+"\n"), &buf)

			select {
			case se := <-sc.errors:
				if se.Pattern != tc.label {
					t.Errorf("pattern = %q, want %q", se.Pattern, tc.label)
				}
				if !strings.Contains(se.Line, tc.line[:min(20, len(tc.line))]) {
					t.Errorf("Line = %q, want to contain input line", se.Line)
				}
			case <-time.After(200 * time.Millisecond):
				t.Fatalf("no StdoutError emitted for line %q", tc.line)
			}
		})
	}
}

// TestStdoutScannerCaseInsensitive verifies that patterns match regardless of case.
func TestStdoutScannerCaseInsensitive(t *testing.T) {
	cases := []string{
		"api FAILED after 3 retries",
		"connection ERROR occurred",
		"max RETRIES exceeded with status",
		"received http 401",
		"received HTTP 429",
		"http 503 service unavailable",
	}
	for _, line := range cases {
		t.Run(line[:min(30, len(line))], func(t *testing.T) {
			sc := newStdoutScanner()
			var buf bytes.Buffer
			sc.pump(strings.NewReader(line+"\n"), &buf)
			select {
			case <-sc.errors:
				// good
			case <-time.After(200 * time.Millisecond):
				t.Fatalf("case-insensitive match failed for %q", line)
			}
		})
	}
}

// TestStdoutScannerNoFalsePositiveOnInnocentLines verifies that ordinary
// stdout output does not trigger any error events.
func TestStdoutScannerNoFalsePositiveOnInnocentLines(t *testing.T) {
	innocent := []string{
		"Starting hermes bridge for agent supervisor",
		"bridge:started {\"agent\":\"supervisor\"}",
		"Tool completed successfully",
		"Agent finished task with status=complete",
		`{"type":"bridge:heartbeat","agent":"web-dev-1"}`,
		"Thinking about the architecture...",
		"Writing to /workspace/src/index.ts",
		"Running tests: npm test",
		"All tests passed",
		"HTTP request completed in 200ms",
		"Response status: 200 OK",
		"Retrying in 1s (attempt 1/3)",
	}
	for _, line := range innocent {
		t.Run(line[:min(30, len(line))], func(t *testing.T) {
			sc := newStdoutScanner()
			var buf bytes.Buffer
			sc.pump(strings.NewReader(line+"\n"), &buf)
			select {
			case se := <-sc.errors:
				t.Errorf("false positive on innocent line %q: got pattern=%q line=%q", line, se.Pattern, se.Line)
			case <-time.After(50 * time.Millisecond):
				// no error emitted — good
			}
		})
	}
}

// TestStdoutScannerSafeToIgnoreContextualReference verifies that a line
// containing a past-tense contextual reference to an error does not fire,
// even when it contains an error keyword. The heuristic requires a past-tense
// marker ("last time", "previously", "earlier") within 30 characters of an
// error keyword, so genuinely past-tense lines are suppressed.
func TestStdoutScannerSafeToIgnoreContextualReference(t *testing.T) {
	safe := []string{
		"we got a Connection error last time, now fixed",
		"had a Connection error previously but recovered",
		"last time we got HTTP 429 but retried successfully",
		"earlier we had a connection error but it resolved",
	}
	for _, line := range safe {
		t.Run(line[:min(40, len(line))], func(t *testing.T) {
			sc := newStdoutScanner()
			var buf bytes.Buffer
			sc.pump(strings.NewReader(line+"\n"), &buf)
			select {
			case se := <-sc.errors:
				t.Errorf("safe-to-ignore line %q should not trigger but got pattern=%q", line, se.Pattern)
			case <-time.After(50 * time.Millisecond):
				// good — no false positive
			}
		})
	}
}

// TestStdoutScannerLiveFailureNotSuppressed verifies that live failure lines
// are NOT suppressed by isSafeToIgnore. The old heuristic used broad matches
// like "we got" which incorrectly dropped lines like "we got HTTP 429 from
// provider". The new heuristic requires a past-tense marker nearby.
func TestStdoutScannerLiveFailureNotSuppressed(t *testing.T) {
	live := []string{
		"we got HTTP 429 from provider",
		"we got a Connection error",
		"API failed after 3 retries",
		"Max retries exceeded calling backend",
	}
	for _, line := range live {
		t.Run(line[:min(40, len(line))], func(t *testing.T) {
			sc := newStdoutScanner()
			var buf bytes.Buffer
			sc.pump(strings.NewReader(line+"\n"), &buf)
			select {
			case <-sc.errors:
				// good — live failure correctly detected
			case <-time.After(200 * time.Millisecond):
				t.Errorf("live failure line %q should trigger an error but did not", line)
			}
		})
	}
}

// TestStdoutScannerDeduplicatesPatternWithinProcess verifies that multiple
// lines matching the same pattern only emit one StdoutError.
func TestStdoutScannerDeduplicatesPatternWithinProcess(t *testing.T) {
	input := "API failed after 3 retries\nAPI failed after 3 retries\nAPI failed after 3 retries\n"
	sc := newStdoutScanner()
	var buf bytes.Buffer
	sc.pump(strings.NewReader(input), &buf)

	count := 0
	for {
		select {
		case _, ok := <-sc.errors:
			if !ok {
				goto done
			}
			count++
		case <-time.After(100 * time.Millisecond):
			goto done
		}
	}
done:
	if count != 1 {
		t.Fatalf("expected exactly 1 StdoutError for repeated pattern, got %d", count)
	}
}

// TestStdoutScannerDifferentPatternsEachEmit verifies that two different
// patterns on different lines each emit their own StdoutError.
func TestStdoutScannerDifferentPatternsEachEmit(t *testing.T) {
	input := "API failed after 3 retries\nHTTP 429 Too Many Requests\n"
	sc := newStdoutScanner()
	var buf bytes.Buffer
	sc.pump(strings.NewReader(input), &buf)

	received := map[string]bool{}
	for {
		select {
		case se, ok := <-sc.errors:
			if !ok {
				goto done
			}
			received[se.Pattern] = true
		case <-time.After(100 * time.Millisecond):
			goto done
		}
	}
done:
	if !received["api_failed_retries"] {
		t.Error("expected api_failed_retries pattern")
	}
	if !received["http_429"] {
		t.Error("expected http_429 pattern")
	}
}

// TestStdoutScannerTeesToLogWriter verifies that the pump goroutine correctly
// tees all bytes to the provided log writer regardless of pattern matching.
func TestStdoutScannerTeesToLogWriter(t *testing.T) {
	input := "normal line\nAPI failed after 3 retries\nanother line\n"
	sc := newStdoutScanner()
	var buf bytes.Buffer
	sc.pump(strings.NewReader(input), &buf)

	got := buf.String()
	for _, want := range []string{"normal line", "API failed after 3 retries", "another line"} {
		if !strings.Contains(got, want) {
			t.Errorf("log writer missing %q, got: %s", want, got)
		}
	}
}

// TestProcessStdoutErrorsChannelNilForNewProcess verifies that StdoutErrors()
// returns nil for processes created via NewProcess (no scanner).
func TestProcessStdoutErrorsChannelNilForNewProcess(t *testing.T) {
	h := &fakeHandle{exitCh: make(chan struct{})}
	p := NewProcess(h, noopCloser{})
	if p.StdoutErrors() != nil {
		t.Error("expected StdoutErrors()=nil for NewProcess (no scanner)")
	}
	close(h.exitCh)
	<-p.Done()
}

// TestProcessStdoutErrorsChannelSetForSpawn verifies that StdoutErrors()
// returns a non-nil channel for processes created via Spawn.
func TestProcessStdoutErrorsChannelSetForSpawn(t *testing.T) {
	cfg := testConfig(t)
	cfg.Cmd = []string{"true"}
	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if p.StdoutErrors() == nil {
		t.Error("expected non-nil StdoutErrors() channel for Spawn")
	}
	<-p.Done()
}

// TestSpawnStdoutScannerDetectsMarkerInOutput verifies end-to-end: a real
// subprocess that prints an error line causes Spawn to emit a StdoutError.
func TestSpawnStdoutScannerDetectsMarkerInOutput(t *testing.T) {
	cfg := testConfig(t)
	// Use sh -c to print a matching line to stdout.
	cfg.Cmd = []string{"/bin/sh", "-c", "echo 'API failed after 3 retries — Connection error'"}

	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	var se StdoutError
	select {
	case se = <-p.StdoutErrors():
		// good
	case <-p.Done():
		// process exited — channel may be closed; try draining
		select {
		case se = <-p.StdoutErrors():
		default:
			t.Fatal("no StdoutError emitted for matching line in subprocess output")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for StdoutError")
	}
	<-p.Done()

	if se.Pattern == "" {
		t.Fatal("StdoutError has empty pattern")
	}
	if !strings.Contains(strings.ToLower(se.Pattern), "api") && !strings.Contains(strings.ToLower(se.Pattern), "connection") {
		t.Errorf("unexpected pattern %q", se.Pattern)
	}
}

// --- helpers ---

// fakeHandle is a minimal ProcessHandle for tests that don't need real I/O.
type fakeHandle struct {
	exitCh  chan struct{}
	waitErr error
}

func (f *fakeHandle) Wait() error {
	<-f.exitCh
	return f.waitErr
}
func (f *fakeHandle) Kill() error {
	select {
	case <-f.exitCh:
	default:
		close(f.exitCh)
	}
	return nil
}

// noopCloser implements io.WriteCloser and discards all writes.
type noopCloser struct{}

func (noopCloser) Write(p []byte) (int, error) { return len(p), nil }
func (noopCloser) Close() error                 { return nil }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

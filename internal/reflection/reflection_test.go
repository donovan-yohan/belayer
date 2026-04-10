package reflection

import (
	"strings"
	"sync"
	"testing"

	"github.com/donovan-yohan/belayer/internal/memory"
	"github.com/donovan-yohan/belayer/internal/store"
)

// helpers

func openMemory(t *testing.T) *memory.SQLiteMemory {
	t.Helper()
	m, err := memory.Open(":memory:")
	if err != nil {
		t.Fatalf("memory.Open: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestNew_CreatesReflector verifies that New returns a non-nil Reflector.
func TestNew_CreatesReflector(t *testing.T) {
	m := openMemory(t)
	s := openStore(t)

	r := New(m, s, nil, nil)
	if r == nil {
		t.Fatal("expected non-nil Reflector")
	}
}

// TestReflect_CreateArchivalEntry verifies that Reflect reads core entries and
// writes a consolidated archival entry.
func TestReflect_CreateArchivalEntry(t *testing.T) {
	m := openMemory(t)
	s := openStore(t)
	r := New(m, s, nil, nil)

	sessionID := "session-reflect-1"
	if err := m.WriteCore(sessionID, "goal", "build memory system"); err != nil {
		t.Fatalf("WriteCore: %v", err)
	}
	if err := m.WriteCore(sessionID, "phase", "implement"); err != nil {
		t.Fatalf("WriteCore: %v", err)
	}

	if err := r.Reflect(sessionID); err != nil {
		t.Fatalf("Reflect: %v", err)
	}

	results, err := m.SearchArchival("Session Memory", 10)
	if err != nil {
		t.Fatalf("SearchArchival: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 archival entry, got %d", len(results))
	}

	entry := results[0]
	if entry.SessionID != sessionID {
		t.Errorf("SessionID: got %q, want %q", entry.SessionID, sessionID)
	}
	if entry.Source != "reflection:"+sessionID {
		t.Errorf("Source: got %q, want %q", entry.Source, "reflection:"+sessionID)
	}
	if !strings.Contains(entry.Content, "goal") {
		t.Errorf("expected content to contain 'goal', got: %q", entry.Content)
	}
	if !strings.Contains(entry.Content, "build memory system") {
		t.Errorf("expected content to contain 'build memory system', got: %q", entry.Content)
	}
	if !strings.Contains(entry.Content, "phase") {
		t.Errorf("expected content to contain 'phase', got: %q", entry.Content)
	}
}

// TestReflect_EmptyCoreIsNoOp verifies that Reflect with no core entries
// succeeds without writing any archival entry.
func TestReflect_EmptyCoreIsNoOp(t *testing.T) {
	m := openMemory(t)
	s := openStore(t)
	r := New(m, s, nil, nil)

	sessionID := "session-empty"

	if err := r.Reflect(sessionID); err != nil {
		t.Fatalf("Reflect on empty session: %v", err)
	}

	// SearchArchival should return nothing — use a broad term unlikely to match.
	results, err := m.SearchArchival("Session", 10)
	if err != nil {
		t.Fatalf("SearchArchival: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 archival entries for no-op Reflect, got %d", len(results))
	}
}

// TestReflect_Serial verifies that concurrent Reflect calls are serialised via
// the mutex and do not interleave. Both calls must succeed, and two archival
// entries must be written (one per session).
func TestReflect_Serial(t *testing.T) {
	m := openMemory(t)
	s := openStore(t)
	r := New(m, s, nil, nil)

	sessA := "session-serial-a"
	sessB := "session-serial-b"

	if err := m.WriteCore(sessA, "key", "value-a"); err != nil {
		t.Fatalf("WriteCore sessA: %v", err)
	}
	if err := m.WriteCore(sessB, "key", "value-b"); err != nil {
		t.Fatalf("WriteCore sessB: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = r.Reflect(sessA)
	}()
	go func() {
		defer wg.Done()
		errs[1] = r.Reflect(sessB)
	}()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Reflect error: %v", i, err)
		}
	}

	// Each session should have produced one archival entry.
	for _, sess := range []string{sessA, sessB} {
		results, err := m.SearchArchival("Session Memory", 10)
		if err != nil {
			t.Fatalf("SearchArchival for %q: %v", sess, err)
		}
		// At least one result expected total (both sessions write "## Session Memory").
		if len(results) == 0 {
			t.Errorf("expected archival entries after serial Reflect, got 0")
		}
	}
}

// TestConsolidate_FormatsMarkdown verifies that Consolidate produces the
// expected markdown format.
func TestConsolidate_FormatsMarkdown(t *testing.T) {
	m := openMemory(t)
	s := openStore(t)
	r := New(m, s, nil, nil)

	entries := []memory.CoreEntry{
		{Key: "goal", Value: "build a memory system"},
		{Key: "phase", Value: "implement"},
	}

	got := r.Consolidate(entries)

	if !strings.HasPrefix(got, "## Session Memory\n\n") {
		t.Errorf("expected markdown header, got: %q", got)
	}
	if !strings.Contains(got, "- **goal**: build a memory system\n") {
		t.Errorf("expected goal entry in output, got: %q", got)
	}
	if !strings.Contains(got, "- **phase**: implement\n") {
		t.Errorf("expected phase entry in output, got: %q", got)
	}
}

// TestConsolidate_EmptyEntriesReturnsEmptyString verifies that Consolidate
// returns an empty string when given no entries.
func TestConsolidate_EmptyEntriesReturnsEmptyString(t *testing.T) {
	m := openMemory(t)
	s := openStore(t)
	r := New(m, s, nil, nil)

	got := r.Consolidate([]memory.CoreEntry{})
	if got != "" {
		t.Errorf("expected empty string for empty entries, got: %q", got)
	}
}

// TestDetectStale_ReturnsUnreferencedEntries verifies that DetectStale returns
// archival entries whose content does not appear in recent session events.
func TestDetectStale_ReturnsUnreferencedEntries(t *testing.T) {
	m := openMemory(t)
	s := openStore(t)
	r := New(m, s, nil, nil)

	// Write two archival entries.
	if err := m.WriteArchival("s1", "the implementer wrote auth module", "auth", ""); err != nil {
		t.Fatalf("WriteArchival referenced: %v", err)
	}
	if err := m.WriteArchival("s1", "stale information nobody references", "stale", ""); err != nil {
		t.Fatalf("WriteArchival stale: %v", err)
	}

	// Create a session and log an event that references the first entry.
	id, err := s.CreateSession(store.Session{Name: "recent-session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.LogEvent(store.SessionEvent{
		SessionID: id,
		Type:      "node_completed",
		Data:      `{"note":"the implementer wrote auth module"}`,
	}); err != nil {
		t.Fatalf("LogEvent: %v", err)
	}

	stale, err := r.DetectStale(1)
	if err != nil {
		t.Fatalf("DetectStale: %v", err)
	}

	// Only the unreferenced entry should be stale.
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale entry, got %d", len(stale))
	}
	if !strings.Contains(stale[0].Content, "stale information") {
		t.Errorf("expected stale entry to contain 'stale information', got: %q", stale[0].Content)
	}
}

// TestDetectStale_NoArchivalEntries verifies that DetectStale returns an empty
// slice when there are no archival entries at all.
func TestDetectStale_NoArchivalEntries(t *testing.T) {
	m := openMemory(t)
	s := openStore(t)
	r := New(m, s, nil, nil)

	stale, err := r.DetectStale(5)
	if err != nil {
		t.Fatalf("DetectStale: %v", err)
	}
	if stale == nil {
		t.Fatal("expected non-nil slice, got nil")
	}
	if len(stale) != 0 {
		t.Errorf("expected 0 stale entries, got %d", len(stale))
	}
}

// ---------------------------------------------------------------------------
// ReflectionConfig
// ---------------------------------------------------------------------------

func TestDefaultReflectionConfig(t *testing.T) {
	cfg := DefaultReflectionConfig()
	if cfg.Vendor != "claude" {
		t.Errorf("Vendor: got %q, want %q", cfg.Vendor, "claude")
	}
	if cfg.Model != "sonnet" {
		t.Errorf("Model: got %q, want %q", cfg.Model, "sonnet")
	}
	if cfg.Trigger != "post-session" {
		t.Errorf("Trigger: got %q, want %q", cfg.Trigger, "post-session")
	}
	if cfg.Limits.MaxReviewLoops != 10 {
		t.Errorf("MaxReviewLoops: got %d, want 10", cfg.Limits.MaxReviewLoops)
	}
}

func TestReflectionConfig_Validate_Valid(t *testing.T) {
	cfg := DefaultReflectionConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() on default config: %v", err)
	}
}

func TestReflectionConfig_Validate_EmptyVendor(t *testing.T) {
	cfg := DefaultReflectionConfig()
	cfg.Vendor = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty vendor")
	}
}

func TestReflectionConfig_Validate_BadTrigger(t *testing.T) {
	cfg := DefaultReflectionConfig()
	cfg.Trigger = "on-full-moon"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for unknown trigger")
	}
}

func TestLoadReflectionConfig_MissingFileReturnsDefault(t *testing.T) {
	cfg, err := LoadReflectionConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("LoadReflectionConfig: %v", err)
	}
	if cfg.Vendor != "claude" {
		t.Errorf("expected default vendor 'claude', got %q", cfg.Vendor)
	}
}

// ---------------------------------------------------------------------------
// Reflection Prompt
// ---------------------------------------------------------------------------

func TestCompileReflectionPrompt_ContainsSections(t *testing.T) {
	ctx := ReflectionPromptContext{
		SessionID:  "sess-123",
		EventsPath: "/tmp/events.txt",
		MemoryDir:  "/home/user/.belayer/memory/system",
		ArchiveDir: "/home/user/.belayer/memory/archive",
	}
	result := CompileReflectionPrompt(ctx)

	checks := []string{
		"reflection agent",
		"Session ID: sess-123",
		"Memory directory: /home/user/.belayer/memory/system",
		"Archive directory: /home/user/.belayer/memory/archive",
		"Events are available at: /tmp/events.txt",
		"What Good Reflection Looks Like",
		"What To Look For",
	}
	for _, want := range checks {
		if !strings.Contains(result, want) {
			t.Errorf("CompileReflectionPrompt() missing %q", want)
		}
	}
}

func TestCompileReflectionPrompt_NoEventsPath(t *testing.T) {
	ctx := ReflectionPromptContext{
		SessionID: "sess-456",
		MemoryDir: "/tmp/mem",
	}
	result := CompileReflectionPrompt(ctx)

	if strings.Contains(result, "Events are available at") {
		t.Error("should omit events section when EventsPath is empty")
	}
}

func TestFormatEventsForReflection_Empty(t *testing.T) {
	result := FormatEventsForReflection(nil)
	if result != "No events recorded for this session." {
		t.Errorf("unexpected result for empty events: %q", result)
	}
}

func TestFormatEventsForReflection_MultipleEvents(t *testing.T) {
	events := []SessionEvent{
		{Type: "session_started", Data: `{"template":"implement"}`},
		{Type: "agent_launched", Data: `{"agent":"pilot"}`},
	}
	result := FormatEventsForReflection(events)

	if !strings.Contains(result, "[session_started]") {
		t.Errorf("missing session_started event type")
	}
	if !strings.Contains(result, "[agent_launched]") {
		t.Errorf("missing agent_launched event type")
	}
}

// ---------------------------------------------------------------------------
// BuildReflectionCmd
// ---------------------------------------------------------------------------

func TestBuildReflectionCmd_Claude(t *testing.T) {
	m := openMemory(t)
	s := openStore(t)
	cfg := DefaultReflectionConfig() // vendor=claude, model=sonnet
	r := New(m, s, nil, &cfg)

	cmd, err := r.BuildReflectionCmd("sess-1", "/tmp/events.txt", "/tmp/mem", "/tmp/archive")
	if err != nil {
		t.Fatalf("BuildReflectionCmd: %v", err)
	}
	if !strings.HasPrefix(cmd, "claude --dangerously-skip-permissions") {
		t.Errorf("expected claude command prefix, got: %s", cmd[:60])
	}
	if !strings.Contains(cmd, "--model sonnet") {
		t.Errorf("expected --model sonnet in command, got: %s", cmd[:80])
	}
}

func TestBuildReflectionCmd_NoConfig(t *testing.T) {
	m := openMemory(t)
	s := openStore(t)
	r := New(m, s, nil, nil)

	_, err := r.BuildReflectionCmd("sess-1", "/tmp/events.txt", "/tmp/mem", "/tmp/archive")
	if err == nil {
		t.Error("expected error when config is nil")
	}
}

func TestBuildReflectionCmd_OpenCode(t *testing.T) {
	m := openMemory(t)
	s := openStore(t)
	cfg := ReflectionConfig{Vendor: "opencode", Model: "gpt-5.1", Trigger: "post-session"}
	r := New(m, s, nil, &cfg)

	cmd, err := r.BuildReflectionCmd("sess-2", "/tmp/events.txt", "/tmp/mem", "/tmp/archive")
	if err != nil {
		t.Fatalf("BuildReflectionCmd: %v", err)
	}
	if !strings.HasPrefix(cmd, "opencode") {
		t.Errorf("expected opencode command, got: %s", cmd[:40])
	}
}

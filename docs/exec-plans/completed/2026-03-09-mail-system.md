# Mail System Implementation Plan

> **Status**: Completed | **Created**: 2026-03-09 | **Completed**: 2026-03-10
> **Design Doc**: `docs/design-docs/2026-03-09-mail-system-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

**Goal:** Build a beads-backed inter-agent mail system with tmux send-keys delivery, replacing signal files (DONE.json, SPOT.json, VERDICT.json) with typed messages.

**Architecture:** `belayer message` writes a beads issue with routing labels and immediately delivers a nudge via tmux send-keys. `belayer mail read` queries beads for unread messages, prints them, and closes the issues. Templates prepend actionable instructions at send time. Identity is derived from `BELAYER_MAIL_ADDRESS` env var set in tmux at spawn time.

**Tech Stack:** Go, beads (`bd` CLI), tmux send-keys, `embed.FS` for templates, cobra for CLI commands.

---

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-09 | Design | Beads as storage backend | Persistent, queryable, audit trail via git. Gastown pattern proven at scale. |
| 2026-03-09 | Design | Sender-driven delivery (no watcher) | Simpler. `belayer message` writes + delivers atomically. Beads provides durability for undelivered messages. |
| 2026-03-09 | Design | All orchestration through mail | Replaces DONE.json/SPOT.json/VERDICT.json. Unified system instead of two mechanisms. |
| 2026-03-09 | Design | Environment-based identity | `BELAYER_MAIL_ADDRESS` env var. Agents never think about their address. |
| 2026-03-09 | Design | Deterministic tmux naming | No registry file needed. Address maps to tmux target by convention. |
| 2026-03-09 | Design | Send-keys only for MVP | No hooks, no watcher, no cooperative delivery. Future enhancement. |
| 2026-03-09 | Design | Templates at send time | Template front-matter prepended to body before writing to beads. Read side just prints. |

## Progress

- [x] Task 1: Mail types and address resolution _(completed 2026-03-10)_
- [x] Task 2: Beads integration layer _(completed 2026-03-10)_
- [x] Task 3: Message templates _(completed 2026-03-10)_
- [x] Task 4: Tmux delivery (send-keys with gotcha handling) _(completed 2026-03-10)_
- [x] Task 5: `belayer message` CLI command _(completed 2026-03-10)_
- [x] Task 6: `belayer mail` CLI commands (read, inbox, ack) _(completed 2026-03-10)_
- [x] Task 7: Setter integration (env var + CLAUDE.md mail instructions) _(completed 2026-03-10)_
- [x] Task 8: Integration test (full send → deliver → read cycle) _(completed 2026-03-10)_

## Surprises & Discoveries

| Date | What | Impact | Resolution |
|------|------|--------|------------|
| 2026-03-10 | `bd list --json` requires `--flat` flag | Default tree mode ignores `--json`, emits emoji text | Added `--flat` to list commands in beads.go |
| 2026-03-10 | `BeadsIssue.Priority` is int not string | JSON parse would fail with string type | Changed field type to `int` |

## Plan Drift

| Task | Plan said | Actually happened | Why |
|------|-----------|-------------------|-----|
| Task 2 | `bd list --label X --status open --json` | Added `--flat` flag | `--json` only works in flat mode |
| Task 2 | `Priority string` in BeadsIssue | `Priority int` | bd returns numeric priority |
| Task 4 | Mock updates deferred to Task 7 | Updated mocks in setter_test.go and lead/claude_test.go now | Project wouldn't compile without satisfying expanded TmuxManager interface |

---

### Task 1: Mail Types and Address Resolution

**Files:**
- Create: `internal/mail/message.go`
- Create: `internal/mail/message_test.go`

**Context:** Defines the core types (MessageType, Address, Message) and the deterministic address → tmux target resolution. No external dependencies.

**Step 1: Write the failing test for address parsing**

```go
// internal/mail/message_test.go
package mail

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAddress(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Address
		wantErr bool
	}{
		{
			name:  "setter",
			input: "setter",
			want:  Address{Role: "setter"},
		},
		{
			name:  "lead",
			input: "task/abc123/lead/frontend/goal-1",
			want:  Address{Role: "lead", TaskID: "abc123", Repo: "frontend", GoalID: "goal-1"},
		},
		{
			name:  "spotter",
			input: "task/abc123/spotter/frontend/goal-1",
			want:  Address{Role: "spotter", TaskID: "abc123", Repo: "frontend", GoalID: "goal-1"},
		},
		{
			name:  "anchor",
			input: "task/abc123/anchor",
			want:  Address{Role: "anchor", TaskID: "abc123"},
		},
		{
			name:    "invalid empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   "task/abc123/unknown/x/y",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAddress(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAddress_String(t *testing.T) {
	addr := Address{Role: "lead", TaskID: "abc", Repo: "frontend", GoalID: "g1"}
	assert.Equal(t, "task/abc/lead/frontend/g1", addr.String())

	addr2 := Address{Role: "setter"}
	assert.Equal(t, "setter", addr2.String())
}

func TestAddress_TmuxTarget(t *testing.T) {
	tests := []struct {
		name       string
		addr       Address
		wantSess   string
		wantWindow string
	}{
		{
			name:       "setter",
			addr:       Address{Role: "setter"},
			wantSess:   "belayer-setter",
			wantWindow: "0",
		},
		{
			name:       "lead",
			addr:       Address{Role: "lead", TaskID: "abc", Repo: "frontend", GoalID: "g1"},
			wantSess:   "belayer-task-abc",
			wantWindow: "frontend-g1",
		},
		{
			name:       "anchor",
			addr:       Address{Role: "anchor", TaskID: "abc"},
			wantSess:   "belayer-task-abc",
			wantWindow: "anchor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess, win := tt.addr.TmuxTarget()
			assert.Equal(t, tt.wantSess, sess)
			assert.Equal(t, tt.wantWindow, win)
		})
	}
}

func TestMessageType_Valid(t *testing.T) {
	assert.True(t, MessageTypeDone.Valid())
	assert.True(t, MessageTypeFeedback.Valid())
	assert.False(t, MessageType("bogus").Valid())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/mail/ -v -run TestParseAddress`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/mail/message.go
package mail

import (
	"fmt"
	"strings"
)

// MessageType identifies the kind of mail message.
type MessageType string

const (
	MessageTypeGoalAssignment MessageType = "goal_assignment"
	MessageTypeDone           MessageType = "done"
	MessageTypeSpotResult     MessageType = "spot_result"
	MessageTypeVerdict        MessageType = "verdict"
	MessageTypeFeedback       MessageType = "feedback"
	MessageTypeInstruction    MessageType = "instruction"
)

var validTypes = map[MessageType]bool{
	MessageTypeGoalAssignment: true,
	MessageTypeDone:           true,
	MessageTypeSpotResult:     true,
	MessageTypeVerdict:        true,
	MessageTypeFeedback:       true,
	MessageTypeInstruction:    true,
}

// Valid returns true if the message type is recognized.
func (mt MessageType) Valid() bool {
	return validTypes[mt]
}

// Address identifies a mail recipient. Deterministically maps to a tmux target.
type Address struct {
	Role   string // "setter", "lead", "spotter", "anchor"
	TaskID string // empty for setter
	Repo   string // empty for setter and anchor
	GoalID string // empty for setter and anchor
}

// ParseAddress parses a path-like address string.
// Valid formats:
//   - "setter"
//   - "task/<id>/lead/<repo>/<goal>"
//   - "task/<id>/spotter/<repo>/<goal>"
//   - "task/<id>/anchor"
func ParseAddress(s string) (Address, error) {
	if s == "" {
		return Address{}, fmt.Errorf("empty address")
	}
	if s == "setter" {
		return Address{Role: "setter"}, nil
	}

	parts := strings.Split(s, "/")
	if len(parts) < 3 || parts[0] != "task" {
		return Address{}, fmt.Errorf("invalid address format: %q", s)
	}

	taskID := parts[1]
	role := parts[2]

	switch role {
	case "lead", "spotter":
		if len(parts) != 5 {
			return Address{}, fmt.Errorf("invalid %s address: expected task/<id>/%s/<repo>/<goal>, got %q", role, role, s)
		}
		return Address{Role: role, TaskID: taskID, Repo: parts[3], GoalID: parts[4]}, nil
	case "anchor":
		if len(parts) != 3 {
			return Address{}, fmt.Errorf("invalid anchor address: expected task/<id>/anchor, got %q", s)
		}
		return Address{Role: role, TaskID: taskID}, nil
	default:
		return Address{}, fmt.Errorf("unknown role %q in address %q", role, s)
	}
}

// String returns the canonical address string.
func (a Address) String() string {
	switch a.Role {
	case "setter":
		return "setter"
	case "anchor":
		return fmt.Sprintf("task/%s/anchor", a.TaskID)
	default:
		return fmt.Sprintf("task/%s/%s/%s/%s", a.TaskID, a.Role, a.Repo, a.GoalID)
	}
}

// TmuxTarget returns the tmux session and window name for this address.
func (a Address) TmuxTarget() (session, window string) {
	switch a.Role {
	case "setter":
		return "belayer-setter", "0"
	case "anchor":
		return fmt.Sprintf("belayer-task-%s", a.TaskID), "anchor"
	default:
		return fmt.Sprintf("belayer-task-%s", a.TaskID), fmt.Sprintf("%s-%s", a.Repo, a.GoalID)
	}
}

// Message is the in-memory representation of a mail message.
type Message struct {
	ID      string      `json:"id"`
	From    string      `json:"from"`
	To      string      `json:"to"`
	Type    MessageType `json:"type"`
	Subject string      `json:"subject"`
	Body    string      `json:"body"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/mail/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/mail/message.go internal/mail/message_test.go
git commit -m "feat(mail): add message types and address resolution"
```

---

### Task 2: Beads Integration Layer

**Files:**
- Create: `internal/mail/beads.go`
- Create: `internal/mail/beads_test.go`

**Context:** Wraps `bd` CLI calls. Handles init, create issue, list issues, close issue. Uses `os/exec` to shell out to `bd`, same pattern as the existing `GitRunner` in the setter. Tests use a real beads database in a temp directory (like setter tests use real SQLite in temp files).

**Step 1: Write the failing test for BeadsStore**

```go
// internal/mail/beads_test.go
package mail

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIfNoBd(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not available")
	}
}

func skipIfNoDolt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not available")
	}
}

func setupTestBeads(t *testing.T) *BeadsStore {
	t.Helper()
	skipIfNoBd(t)
	skipIfNoDolt(t)

	dir := t.TempDir()
	store, err := NewBeadsStore(dir, "test-mail")
	require.NoError(t, err)
	return store
}

func TestBeadsStore_Init(t *testing.T) {
	store := setupTestBeads(t)

	// .beads directory should exist
	_, err := os.Stat(filepath.Join(store.dir, ".beads"))
	assert.NoError(t, err)
}

func TestBeadsStore_CreateAndList(t *testing.T) {
	store := setupTestBeads(t)

	// Create a message
	err := store.Create("Test subject", "Test body", map[string]string{
		"to":       "setter",
		"from":     "task/abc/lead/api/g1",
		"msg-type": "done",
	})
	require.NoError(t, err)

	// List messages for "setter"
	issues, err := store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 1)
	assert.Equal(t, "Test subject", issues[0].Title)
	assert.Contains(t, issues[0].Description, "Test body")
}

func TestBeadsStore_Close(t *testing.T) {
	store := setupTestBeads(t)

	// Create a message
	err := store.Create("Msg to close", "Body", map[string]string{
		"to":       "setter",
		"from":     "task/abc/lead/api/g1",
		"msg-type": "done",
	})
	require.NoError(t, err)

	// List to get the ID
	issues, err := store.List("setter")
	require.NoError(t, err)
	require.Len(t, issues, 1)

	// Close it
	err = store.Close(issues[0].ID)
	require.NoError(t, err)

	// List again — should be empty (only open issues)
	issues, err = store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 0)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/mail/ -v -run TestBeadsStore`
Expected: FAIL — BeadsStore not defined

**Step 3: Write minimal implementation**

```go
// internal/mail/beads.go
package mail

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// BeadsIssue represents a beads issue returned by bd list --json.
type BeadsIssue struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
}

// BeadsStore wraps the bd CLI for mail storage.
type BeadsStore struct {
	dir string // directory containing .beads/
}

// NewBeadsStore initializes a beads database in the given directory.
// If already initialized, this is a no-op.
func NewBeadsStore(dir string, prefix string) (*BeadsStore, error) {
	store := &BeadsStore{dir: dir}

	// Check if already initialized
	cmd := exec.Command("bd", "list", "--json")
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		return store, nil // already initialized
	}

	// Initialize
	cmd = exec.Command("bd", "init", "--prefix", prefix, "--stealth", "--skip-hooks", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("bd init: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return store, nil
}

// Create creates a new beads issue with the given title, description, and labels.
func (b *BeadsStore) Create(title, description string, labels map[string]string) error {
	args := []string{"create", "--title", title, "--description", description}
	for k, v := range labels {
		args = append(args, "--label", fmt.Sprintf("%s:%s", k, v))
	}
	args = append(args, "--json")

	cmd := exec.Command("bd", args...)
	cmd.Dir = b.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd create: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// List returns open issues labeled with to:<address>.
func (b *BeadsStore) List(toAddress string) ([]BeadsIssue, error) {
	label := fmt.Sprintf("to:%s", toAddress)
	cmd := exec.Command("bd", "list", "--label", label, "--status", "open", "--json")
	cmd.Dir = b.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("bd list: %s: %w", strings.TrimSpace(string(out)), err)
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "[]" || trimmed == "null" {
		return nil, nil
	}

	var issues []BeadsIssue
	if err := json.Unmarshal([]byte(trimmed), &issues); err != nil {
		return nil, fmt.Errorf("parsing bd list output: %w", err)
	}
	return issues, nil
}

// Close closes a beads issue by ID (marks as read).
func (b *BeadsStore) Close(id string) error {
	cmd := exec.Command("bd", "close", id)
	cmd.Dir = b.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd close %s: %s: %w", id, strings.TrimSpace(string(out)), err)
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/mail/ -v -run TestBeadsStore`
Expected: PASS (requires `bd` and `dolt` installed)

**Step 5: Commit**

```bash
git add internal/mail/beads.go internal/mail/beads_test.go
git commit -m "feat(mail): add beads integration layer"
```

---

### Task 3: Message Templates

**Files:**
- Create: `internal/defaults/mail/goal_assignment.md.tmpl`
- Create: `internal/defaults/mail/done.md.tmpl`
- Create: `internal/defaults/mail/spot_result.md.tmpl`
- Create: `internal/defaults/mail/verdict.md.tmpl`
- Create: `internal/defaults/mail/feedback.md.tmpl`
- Create: `internal/defaults/mail/instruction.md.tmpl`
- Create: `internal/mail/templates.go`
- Create: `internal/mail/templates_test.go`

**Context:** Templates are embedded via `embed.FS` and rendered at send time. Each template prepends role-specific instructions to the raw body. Uses `text/template`.

**Step 1: Write the failing test for template rendering**

```go
// internal/mail/templates_test.go
package mail

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderTemplate(t *testing.T) {
	tests := []struct {
		name     string
		msgType  MessageType
		body     string
		wantSub  string // substring that should appear in output
	}{
		{
			name:    "feedback",
			msgType: MessageTypeFeedback,
			body:    "Login form missing validation",
			wantSub: "Login form missing validation",
		},
		{
			name:    "goal_assignment",
			msgType: MessageTypeGoalAssignment,
			body:    "Implement dark mode",
			wantSub: "Implement dark mode",
		},
		{
			name:    "done",
			msgType: MessageTypeDone,
			body:    `{"status":"complete"}`,
			wantSub: `{"status":"complete"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered, err := RenderTemplate(tt.msgType, tt.body)
			require.NoError(t, err)
			assert.Contains(t, rendered, tt.wantSub)
		})
	}
}

func TestRenderTemplate_InvalidType(t *testing.T) {
	_, err := RenderTemplate(MessageType("bogus"), "body")
	assert.Error(t, err)
}

func TestDefaultSubject(t *testing.T) {
	assert.Equal(t, "Goal Assignment", DefaultSubject(MessageTypeGoalAssignment))
	assert.Equal(t, "Goal Complete", DefaultSubject(MessageTypeDone))
	assert.Equal(t, "Feedback", DefaultSubject(MessageTypeFeedback))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/mail/ -v -run TestRenderTemplate`
Expected: FAIL — RenderTemplate not defined

**Step 3: Create the template files**

Create each template in `internal/defaults/mail/`:

`goal_assignment.md.tmpl`:
```
NEW GOAL ASSIGNED

Read .lead/GOAL.json for full context. Begin working on your assignment.
When complete, signal done:
  belayer message setter --type done --body '{"status":"complete","summary":"<your summary>"}'

---

{{.Body}}
```

`done.md.tmpl`:
```
GOAL COMPLETE

{{.Body}}
```

`spot_result.md.tmpl`:
```
SPOTTER VALIDATION RESULT

{{.Body}}
```

`verdict.md.tmpl`:
```
ANCHOR ALIGNMENT VERDICT

{{.Body}}
```

`feedback.md.tmpl`:
```
FEEDBACK FROM SPOTTER

Address the following feedback, then signal completion:
  belayer message setter --type done --body '{"status":"complete","summary":"<your summary>"}'

---

{{.Body}}
```

`instruction.md.tmpl`:
```
INSTRUCTION

{{.Body}}
```

**Step 4: Write the template loader**

```go
// internal/mail/templates.go
package mail

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed templates/*.md.tmpl
var templateFS embed.FS

var defaultSubjects = map[MessageType]string{
	MessageTypeGoalAssignment: "Goal Assignment",
	MessageTypeDone:           "Goal Complete",
	MessageTypeSpotResult:     "Spotter Result",
	MessageTypeVerdict:        "Anchor Verdict",
	MessageTypeFeedback:       "Feedback",
	MessageTypeInstruction:    "Instruction",
}

// DefaultSubject returns the default subject line for a message type.
func DefaultSubject(mt MessageType) string {
	if s, ok := defaultSubjects[mt]; ok {
		return s
	}
	return string(mt)
}

type templateData struct {
	Body string
}

// RenderTemplate applies the template for the given message type to the body.
func RenderTemplate(mt MessageType, body string) (string, error) {
	if !mt.Valid() {
		return "", fmt.Errorf("unknown message type: %q", mt)
	}

	filename := fmt.Sprintf("templates/%s.md.tmpl", string(mt))
	content, err := templateFS.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("loading template for %s: %w", mt, err)
	}

	tmpl, err := template.New(string(mt)).Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("parsing template for %s: %w", mt, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData{Body: body}); err != nil {
		return "", fmt.Errorf("rendering template for %s: %w", mt, err)
	}

	return buf.String(), nil
}
```

Note: Template files should be placed at `internal/mail/templates/` (not `internal/defaults/mail/`) so they can be embedded directly by the mail package. Update the embed directive accordingly.

**Step 5: Run test to verify it passes**

Run: `go test ./internal/mail/ -v -run TestRenderTemplate`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/mail/templates/ internal/mail/templates.go internal/mail/templates_test.go
git commit -m "feat(mail): add message templates with embed.FS"
```

---

### Task 4: Tmux Delivery (Send-Keys with Gotcha Handling)

**Files:**
- Create: `internal/mail/delivery.go`
- Create: `internal/mail/delivery_test.go`
- Modify: `internal/tmux/tmux.go` — add new methods to `TmuxManager` interface

**Context:** This is the most complex task. Implements gastown's battle-tested send-keys patterns: sanitization, copy mode exit, chunking, ESC+600ms timing, per-session nudge lock, cold startup retry, SIGWINCH wake. New methods are added to the `TmuxManager` interface so they can be mocked in tests.

**Step 1: Add new methods to TmuxManager interface**

Add to `internal/tmux/tmux.go`:

```go
// New methods on TmuxManager interface:
// SetEnvironment sets a tmux environment variable for a session.
SetEnvironment(session, key, value string) error
// DisplayMessage runs tmux display-message and returns the result.
DisplayMessage(session, format string) (string, error)
// SendKeysLiteral sends literal text (no Enter) to a specific target.
SendKeysLiteral(target, text string) error
// SendKeysRaw sends a raw key name (like "Enter", "Escape") to a target.
SendKeysRaw(target, key string) error
// ResizeWindow resizes a tmux window.
ResizeWindow(session string, width int) error
// SetWindowOption sets a window option.
SetWindowOption(session, option, value string) error
// IsSessionAttached checks if a session has any clients attached.
IsSessionAttached(session string) bool
```

**Step 2: Write the failing test for sanitization and delivery**

```go
// internal/mail/delivery_test.go
package mail

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeMessage(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"strips ESC", "hello\x1bworld", "helloworld"},
		{"strips CR", "hello\rworld", "helloworld"},
		{"strips BS", "hello\x08world", "helloworld"},
		{"strips DEL", "hello\x7fworld", "helloworld"},
		{"tab to space", "hello\tworld", "hello world"},
		{"preserves newlines", "hello\nworld", "hello\nworld"},
		{"preserves unicode", "hello 🌍 world", "hello 🌍 world"},
		{"preserves quotes", `hello "world" 'foo'`, `hello "world" 'foo'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeMessage(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestChunkMessage(t *testing.T) {
	// Short message: single chunk
	chunks := ChunkMessage("short", 512)
	assert.Len(t, chunks, 1)
	assert.Equal(t, "short", chunks[0])

	// Long message: multiple chunks
	long := make([]byte, 1025)
	for i := range long {
		long[i] = 'x'
	}
	chunks = ChunkMessage(string(long), 512)
	assert.Len(t, chunks, 3) // 512 + 512 + 1
	assert.Len(t, chunks[0], 512)
	assert.Len(t, chunks[1], 512)
	assert.Len(t, chunks[2], 1)
}
```

**Step 3: Write implementation**

```go
// internal/mail/delivery.go
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
```

**Step 4: Update TmuxManager interface and RealTmux**

Add `SendKeysLiteral`, `SendKeysRaw`, `SetEnvironment` to `internal/tmux/tmux.go` (both interface and `RealTmux`).

**Step 5: Run tests**

Run: `go test ./internal/mail/ -v -run "TestSanitize|TestChunk"`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/mail/delivery.go internal/mail/delivery_test.go internal/tmux/tmux.go
git commit -m "feat(mail): add tmux delivery with send-keys gotcha handling"
```

---

### Task 5: `belayer message` CLI Command

**Files:**
- Create: `internal/cli/message.go`
- Create: `internal/mail/send.go`
- Create: `internal/mail/send_test.go`
- Modify: `internal/cli/root.go` — register new command

**Context:** The `belayer message <address> --type <type> --body "..."` command. Orchestrates: resolve instance → render template → write to beads → deliver via tmux. Flags: `--body`, `--file`, `--stdin` (mutually exclusive), `--type` (required), `--subject` (optional).

**Step 1: Write the failing test for Send()**

```go
// internal/mail/send_test.go
package mail

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSend_RendersTemplateAndSetsLabels(t *testing.T) {
	// Test that Send() produces the correct rendered body and label set.
	// This is a unit test for the orchestration logic, not the beads/tmux integration.

	rendered, err := RenderTemplate(MessageTypeFeedback, "Fix the bug")
	require.NoError(t, err)
	assert.Contains(t, rendered, "FEEDBACK FROM SPOTTER")
	assert.Contains(t, rendered, "Fix the bug")
	assert.Contains(t, rendered, "belayer message setter --type done")
}
```

**Step 2: Write the Send function**

```go
// internal/mail/send.go
package mail

import (
	"fmt"
	"log"

	"github.com/donovan-yohan/belayer/internal/tmux"
)

// SendOpts configures a mail send operation.
type SendOpts struct {
	To      string
	From    string // auto-populated from BELAYER_MAIL_ADDRESS if empty
	Type    MessageType
	Subject string // optional override; uses DefaultSubject if empty
	Body    string
}

// Send writes a message to beads and delivers a nudge via tmux.
func Send(store *BeadsStore, tm tmux.TmuxManager, opts SendOpts) error {
	if !opts.Type.Valid() {
		return fmt.Errorf("invalid message type: %q", opts.Type)
	}

	// Parse and validate address
	addr, err := ParseAddress(opts.To)
	if err != nil {
		return fmt.Errorf("invalid address: %w", err)
	}

	// Render template
	rendered, err := RenderTemplate(opts.Type, opts.Body)
	if err != nil {
		return fmt.Errorf("rendering template: %w", err)
	}

	// Resolve subject
	subject := opts.Subject
	if subject == "" {
		subject = DefaultSubject(opts.Type)
	}

	// Write to beads
	labels := map[string]string{
		"to":       opts.To,
		"msg-type": string(opts.Type),
	}
	if opts.From != "" {
		labels["from"] = opts.From
	}

	if err := store.Create(subject, rendered, labels); err != nil {
		return fmt.Errorf("writing to beads: %w", err)
	}

	// Deliver via tmux (best-effort — message is durable in beads)
	session, window := addr.TmuxTarget()
	nudgeText := "You have a new message. Run `belayer mail read` to see it."
	if err := NudgeSession(tm, session, window, nudgeText); err != nil {
		log.Printf("mail: delivery failed (message persisted in beads): %v", err)
		// Don't return error — the message is stored, just not delivered
	}

	return nil
}
```

**Step 3: Write the CLI command**

```go
// internal/cli/message.go
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/donovan-yohan/belayer/internal/mail"
	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/spf13/cobra"
)

func newMessageCmd() *cobra.Command {
	var (
		bodyFlag    string
		fileFlag    string
		stdinFlag   bool
		typeFlag    string
		subjectFlag string
	)

	cmd := &cobra.Command{
		Use:   "message <address>",
		Short: "Send a mail message to an agent",
		Long:  "Send a typed message to any belayer agent (setter, lead, spotter, anchor). The message is stored in beads and delivered via tmux.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			address := args[0]

			// Resolve body from flags (mutually exclusive)
			var body string
			flagCount := 0
			if bodyFlag != "" { flagCount++ }
			if fileFlag != "" { flagCount++ }
			if stdinFlag { flagCount++ }
			if flagCount > 1 {
				return fmt.Errorf("--body, --file, and --stdin are mutually exclusive")
			}
			if flagCount == 0 {
				return fmt.Errorf("one of --body, --file, or --stdin is required")
			}

			switch {
			case bodyFlag != "":
				body = bodyFlag
			case fileFlag != "":
				data, err := os.ReadFile(fileFlag)
				if err != nil {
					return fmt.Errorf("reading file: %w", err)
				}
				body = string(data)
			case stdinFlag:
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("reading stdin: %w", err)
				}
				body = string(data)
			}

			// Resolve instance and mail directory
			instanceName, err := resolveInstanceName("")
			if err != nil {
				return err
			}
			mailDir := mailDirForInstance(instanceName)

			// Initialize beads store
			store, err := mail.NewBeadsStore(mailDir, "belayer-mail")
			if err != nil {
				return fmt.Errorf("initializing mail store: %w", err)
			}

			// Resolve sender from env
			from := os.Getenv("BELAYER_MAIL_ADDRESS")

			tm := tmux.NewRealTmux()
			return mail.Send(store, tm, mail.SendOpts{
				To:      address,
				From:    from,
				Type:    mail.MessageType(typeFlag),
				Subject: subjectFlag,
				Body:    body,
			})
		},
	}

	cmd.Flags().StringVar(&bodyFlag, "body", "", "Message body (inline)")
	cmd.Flags().StringVar(&fileFlag, "file", "", "Read body from file")
	cmd.Flags().BoolVar(&stdinFlag, "stdin", false, "Read body from stdin")
	cmd.Flags().StringVar(&typeFlag, "type", "", "Message type (required)")
	cmd.Flags().StringVar(&subjectFlag, "subject", "", "Subject override")
	_ = cmd.MarkFlagRequired("type")

	return cmd
}

// mailDirForInstance returns the mail directory path for an instance.
func mailDirForInstance(instanceName string) string {
	home, _ := os.UserHomeDir()
	return home + "/.belayer/instances/" + instanceName + "/mail"
}
```

**Step 4: Register in root.go**

Add `newMessageCmd()` to `cmd.AddCommand(...)` in `internal/cli/root.go`.

**Step 5: Build and verify**

Run: `go build -o belayer ./cmd/belayer && ./belayer message --help`
Expected: Shows usage for `belayer message`

**Step 6: Commit**

```bash
git add internal/cli/message.go internal/mail/send.go internal/mail/send_test.go internal/cli/root.go
git commit -m "feat(cli): add belayer message command"
```

---

### Task 6: `belayer mail` CLI Commands (read, inbox, ack)

**Files:**
- Create: `internal/cli/mail.go`
- Create: `internal/mail/read.go`
- Create: `internal/mail/read_test.go`
- Modify: `internal/cli/root.go` — register mail command

**Context:** `belayer mail read` queries beads for unread messages to `BELAYER_MAIL_ADDRESS`, prints them, and closes. `belayer mail inbox` lists without closing. `belayer mail ack <id>` closes a specific message.

**Step 1: Write the failing test**

```go
// internal/mail/read_test.go
package mail

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatMessages(t *testing.T) {
	msgs := []BeadsIssue{
		{ID: "mail-abc", Title: "Feedback", Description: "Fix the bug"},
		{ID: "mail-def", Title: "Goal Assignment", Description: "Add dark mode"},
	}

	output := FormatMessages(msgs)
	assert.Contains(t, output, "Fix the bug")
	assert.Contains(t, output, "Add dark mode")
	assert.Contains(t, output, "mail-abc")
	assert.Contains(t, output, "mail-def")
}

func TestReadAndClose(t *testing.T) {
	store := setupTestBeads(t)

	// Create two messages
	require.NoError(t, store.Create("Msg 1", "Body 1", map[string]string{"to": "setter", "msg-type": "done"}))
	require.NoError(t, store.Create("Msg 2", "Body 2", map[string]string{"to": "setter", "msg-type": "feedback"}))

	// Read should return both
	issues, err := store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 2)

	// Close them
	for _, issue := range issues {
		require.NoError(t, store.Close(issue.ID))
	}

	// List again — empty
	issues, err = store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 0)
}
```

**Step 2: Write implementation**

```go
// internal/mail/read.go
package mail

import (
	"fmt"
	"strings"
)

// FormatMessages formats a list of beads issues for terminal output.
func FormatMessages(issues []BeadsIssue) string {
	if len(issues) == 0 {
		return "No unread messages.\n"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("📬 %d unread message(s):\n\n", len(issues)))

	for i, issue := range issues {
		b.WriteString(fmt.Sprintf("--- Message %d [%s] ---\n", i+1, issue.ID))
		b.WriteString(issue.Description)
		b.WriteString("\n\n")
	}

	return b.String()
}

// ReadInbox lists unread messages for the given address.
func ReadInbox(store *BeadsStore, address string) ([]BeadsIssue, error) {
	return store.List(address)
}

// ReadAndClose lists unread messages, prints them, and closes them.
func ReadAndClose(store *BeadsStore, address string) (string, error) {
	issues, err := store.List(address)
	if err != nil {
		return "", err
	}

	output := FormatMessages(issues)

	// Close all read messages
	for _, issue := range issues {
		if closeErr := store.Close(issue.ID); closeErr != nil {
			fmt.Printf("warning: failed to close message %s: %v\n", issue.ID, closeErr)
		}
	}

	return output, nil
}
```

**Step 3: Write the CLI commands**

```go
// internal/cli/mail.go
package cli

import (
	"fmt"
	"os"

	"github.com/donovan-yohan/belayer/internal/mail"
	"github.com/spf13/cobra"
)

func newMailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mail",
		Short: "Read and manage mail messages",
	}

	cmd.AddCommand(newMailReadCmd())
	cmd.AddCommand(newMailInboxCmd())
	cmd.AddCommand(newMailAckCmd())

	return cmd
}

func newMailReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read",
		Short: "Read all unread messages and mark as read",
		RunE: func(cmd *cobra.Command, args []string) error {
			address := os.Getenv("BELAYER_MAIL_ADDRESS")
			if address == "" {
				return fmt.Errorf("BELAYER_MAIL_ADDRESS not set")
			}

			instanceName, err := resolveInstanceName("")
			if err != nil {
				return err
			}

			store, err := mail.NewBeadsStore(mailDirForInstance(instanceName), "belayer-mail")
			if err != nil {
				return err
			}

			output, err := mail.ReadAndClose(store, address)
			if err != nil {
				return err
			}

			fmt.Print(output)
			return nil
		},
	}
}

func newMailInboxCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inbox",
		Short: "List unread messages without marking as read",
		RunE: func(cmd *cobra.Command, args []string) error {
			address := os.Getenv("BELAYER_MAIL_ADDRESS")
			if address == "" {
				return fmt.Errorf("BELAYER_MAIL_ADDRESS not set")
			}

			instanceName, err := resolveInstanceName("")
			if err != nil {
				return err
			}

			store, err := mail.NewBeadsStore(mailDirForInstance(instanceName), "belayer-mail")
			if err != nil {
				return err
			}

			issues, err := mail.ReadInbox(store, address)
			if err != nil {
				return err
			}

			fmt.Print(mail.FormatMessages(issues))
			return nil
		},
	}
}

func newMailAckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ack <message-id>",
		Short: "Mark a specific message as read",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceName, err := resolveInstanceName("")
			if err != nil {
				return err
			}

			store, err := mail.NewBeadsStore(mailDirForInstance(instanceName), "belayer-mail")
			if err != nil {
				return err
			}

			return store.Close(args[0])
		},
	}
}
```

**Step 4: Register in root.go**

Add `newMailCmd()` to `cmd.AddCommand(...)`.

**Step 5: Build and verify**

Run: `go build -o belayer ./cmd/belayer && ./belayer mail --help`
Expected: Shows subcommands read, inbox, ack

**Step 6: Commit**

```bash
git add internal/cli/mail.go internal/mail/read.go internal/mail/read_test.go internal/cli/root.go
git commit -m "feat(cli): add belayer mail read/inbox/ack commands"
```

---

### Task 7: Setter Integration

**Files:**
- Modify: `internal/setter/taskrunner.go` — set `BELAYER_MAIL_ADDRESS` when spawning goals
- Modify: `internal/defaults/claudemd/lead.md` — add mail instructions
- Modify: `internal/defaults/claudemd/spotter.md` — add mail instructions
- Modify: `internal/defaults/claudemd/anchor.md` — add mail instructions
- Modify: `internal/tmux/tmux.go` — add `SetEnvironment` to interface + RealTmux

**Context:** When the setter spawns a goal (lead/spotter/anchor), it sets `BELAYER_MAIL_ADDRESS` in the tmux session environment. The CLAUDE.md templates are updated with mail instructions so agents know to use `belayer mail read` and `belayer message`.

**Step 1: Add SetEnvironment to TmuxManager**

Add to interface and implement in RealTmux:

```go
func (r *RealTmux) SetEnvironment(session, key, value string) error {
	cmd := exec.Command("tmux", "set-environment", "-t", session, key, value)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux set-environment -t %s %s: %s: %w", session, key, strings.TrimSpace(string(output)), err)
	}
	return nil
}
```

**Step 2: Update SpawnGoal to set mail address**

In `internal/setter/taskrunner.go`, after creating the tmux window, add:

```go
// Set mail address for the agent
mailAddr := fmt.Sprintf("task/%s/%s/%s/%s", tr.task.ID, role, goal.RepoName, goal.ID)
if err := tr.tmux.SetEnvironment(tr.tmuxSession, "BELAYER_MAIL_ADDRESS", mailAddr); err != nil {
	log.Printf("warning: failed to set BELAYER_MAIL_ADDRESS: %v", err)
}
```

**Step 3: Update CLAUDE.md templates**

Append to each of `lead.md`, `spotter.md`, `anchor.md`:

```markdown
## Mail

You can receive messages from the orchestration system.
When prompted, run `belayer mail read` to check your messages.
When you complete your work, signal completion:
  belayer message setter --type done --body '{"status":"complete","summary":"<describe what you did>"}'
```

**Step 4: Write test for mail address setting**

Add to `internal/setter/setter_test.go`:

```go
func TestSpawnGoal_SetsMailAddress(t *testing.T) {
	// ... setup similar to existing TestTaskRunner_SpawnGoal ...
	// Verify mockTmux received SetEnvironment call with correct address
}
```

Update `mockTmux` to record `SetEnvironment` calls.

**Step 5: Run tests**

Run: `go test ./internal/setter/ -v -run TestSpawnGoal_SetsMailAddress`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/setter/taskrunner.go internal/tmux/tmux.go internal/defaults/claudemd/ internal/setter/setter_test.go
git commit -m "feat(setter): set BELAYER_MAIL_ADDRESS and add mail instructions to CLAUDE.md"
```

---

### Task 8: Integration Test (Full Send → Deliver → Read Cycle)

**Files:**
- Create: `internal/mail/integration_test.go`

**Context:** End-to-end test that exercises the full mail flow without tmux (using mock tmux). Creates a beads store in a temp dir, sends a message, verifies it's stored, verifies it can be read, verifies closing marks it as read.

**Step 1: Write the integration test**

```go
// internal/mail/integration_test.go
package mail

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_SendAndRead(t *testing.T) {
	store := setupTestBeads(t)

	// Send a feedback message (no tmux delivery in this test)
	err := store.Create(
		DefaultSubject(MessageTypeFeedback),
		mustRender(t, MessageTypeFeedback, "Login form missing validation"),
		map[string]string{
			"to":       "task/abc/lead/api/g1",
			"from":     "setter",
			"msg-type": string(MessageTypeFeedback),
		},
	)
	require.NoError(t, err)

	// Read inbox
	issues, err := store.List("task/abc/lead/api/g1")
	require.NoError(t, err)
	require.Len(t, issues, 1)

	// Verify rendered content
	assert.Contains(t, issues[0].Description, "FEEDBACK FROM SPOTTER")
	assert.Contains(t, issues[0].Description, "Login form missing validation")
	assert.Contains(t, issues[0].Description, "belayer message setter --type done")

	// Close (mark read)
	require.NoError(t, store.Close(issues[0].ID))

	// Inbox should be empty
	issues, err = store.List("task/abc/lead/api/g1")
	require.NoError(t, err)
	assert.Len(t, issues, 0)
}

func TestIntegration_MultipleMessages(t *testing.T) {
	store := setupTestBeads(t)

	// Send two messages to same recipient
	require.NoError(t, store.Create("Msg 1", "Body 1", map[string]string{"to": "setter", "msg-type": "done"}))
	require.NoError(t, store.Create("Msg 2", "Body 2", map[string]string{"to": "setter", "msg-type": "done"}))

	// Send one to different recipient
	require.NoError(t, store.Create("Msg 3", "Body 3", map[string]string{"to": "task/x/lead/api/g1", "msg-type": "feedback"}))

	// Setter should see 2
	issues, err := store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 2)

	// Lead should see 1
	issues, err = store.List("task/x/lead/api/g1")
	require.NoError(t, err)
	assert.Len(t, issues, 1)
}

func TestIntegration_DoneSignal(t *testing.T) {
	store := setupTestBeads(t)

	// Lead sends done signal to setter
	doneBody := `{"status":"complete","summary":"Added login validation"}`
	rendered := mustRender(t, MessageTypeDone, doneBody)
	require.NoError(t, store.Create(
		DefaultSubject(MessageTypeDone),
		rendered,
		map[string]string{
			"to":       "setter",
			"from":     "task/abc/lead/api/g1",
			"msg-type": string(MessageTypeDone),
		},
	))

	// Setter reads it
	issues, err := store.List("setter")
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Description, "Added login validation")
}

func mustRender(t *testing.T, mt MessageType, body string) string {
	t.Helper()
	rendered, err := RenderTemplate(mt, body)
	require.NoError(t, err)
	return rendered
}
```

**Step 2: Run all tests**

Run: `go test ./internal/mail/ -v`
Expected: ALL PASS

**Step 3: Run full project tests**

Run: `go test ./...`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add internal/mail/integration_test.go
git commit -m "test(mail): add integration tests for full send-read cycle"
```

---

## Outcomes & Retrospective

**What worked:**
- Parallel task dispatch (4 independent foundation tasks, then 4 dependent tasks) completed efficiently
- Gastown's battle-tested tmux send-keys patterns translated cleanly to belayer
- Beads (`bd` CLI) provided a solid storage backend with minimal integration effort
- TDD approach caught the `--flat` flag requirement and Priority type mismatch early
- Workers independently discovered and fixed issues (mock updates, mailStore helper reuse)

**What didn't:**
- Task 6 was completed twice — once by worker-2 as part of Task 2's scope creep, then again as its own task. Need clearer task boundaries.
- Plan's `mailDirForInstance` helper was superseded by worker-2's better `mailStore` pattern using `instance.Load()`

**Learnings to codify:**
- `bd list --json` requires `--flat` flag — default tree mode ignores `--json`
- `bd` returns `priority` as int, not string
- When expanding Go interfaces, all mock implementations must be updated in the same commit or project won't compile
- Sender-driven delivery (write + deliver atomically) is simpler than watcher daemons for MVP

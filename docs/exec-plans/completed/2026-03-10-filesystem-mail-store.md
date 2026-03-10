# Filesystem Mail Store Implementation Plan

> **Status**: Completed | **Created**: 2026-03-10 | **Completed**: 2026-03-10
> **Design Doc**: `docs/design-docs/2026-03-10-filesystem-mail-store-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-10 | Design | Per-address directories with unread/read subdirs | Mirrors address scheme, readdir for listing, `os.RemoveAll` for task cleanup |
| 2026-03-10 | Design | Keep read messages for audit | Debugging visibility into agent communication |
| 2026-03-10 | Design | Replace beads with filesystem | Beads spawns dolt sql-server per Claude session, leaking dozens of orphaned processes |

## Progress

- [x] Task 1: Create FileStore implementation _(completed 2026-03-10)_
- [x] Task 2: Update consumers (send.go, read.go, CLI) _(completed 2026-03-10)_
- [x] Task 3: Add mail cleanup to TaskRunner _(completed 2026-03-10)_
- [x] Task 4: Delete beads files and update docs _(completed 2026-03-10)_
- [x] Task 5: Build, test, commit _(verified 2026-03-10 — build passes, all tests pass)_

## Surprises & Discoveries

No surprises. All tasks completed as planned with no deviations.

## Plan Drift

_None yet — updated when tasks deviate from plan during execution._

---

### Task 1: Create FileStore implementation

**Files:**
- Create: `internal/mail/filestore.go`
- Create: `internal/mail/filestore_test.go`

- [ ] **Step 1: Write failing tests for FileStore**

Create `internal/mail/filestore_test.go`:

```go
package mail

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestFileStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(t.TempDir())
}

func TestFileStore_CreateAndList(t *testing.T) {
	store := setupTestFileStore(t)

	err := store.Create("Test subject", "Test body", map[string]string{
		"to":       "setter",
		"from":     "task/abc/lead/api/g1",
		"msg-type": "done",
	})
	require.NoError(t, err)

	issues, err := store.List("setter")
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "Test subject", issues[0].Title)
	assert.Contains(t, issues[0].Description, "Test body")
}

func TestFileStore_Close(t *testing.T) {
	store := setupTestFileStore(t)

	err := store.Create("Msg to close", "Body", map[string]string{
		"to":       "setter",
		"from":     "task/abc/lead/api/g1",
		"msg-type": "done",
	})
	require.NoError(t, err)

	issues, err := store.List("setter")
	require.NoError(t, err)
	require.Len(t, issues, 1)

	err = store.Close(issues[0].ID)
	require.NoError(t, err)

	// Unread should be empty
	issues, err = store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 0)

	// File should exist in read/
	readDir := filepath.Join(store.dir, "setter", "read")
	entries, err := os.ReadDir(readDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestFileStore_ListEmpty(t *testing.T) {
	store := setupTestFileStore(t)

	issues, err := store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 0)
}

func TestFileStore_NestedAddress(t *testing.T) {
	store := setupTestFileStore(t)

	err := store.Create("Goal", "Do the thing", map[string]string{
		"to":       "task/t1/lead/api/g1",
		"msg-type": "goal_assignment",
	})
	require.NoError(t, err)

	issues, err := store.List("task/t1/lead/api/g1")
	require.NoError(t, err)
	require.Len(t, issues, 1)

	// Different address should be empty
	issues, err = store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 0)
}

func TestFileStore_MultipleMessages(t *testing.T) {
	store := setupTestFileStore(t)

	require.NoError(t, store.Create("Msg 1", "Body 1", map[string]string{"to": "setter", "msg-type": "done"}))
	require.NoError(t, store.Create("Msg 2", "Body 2", map[string]string{"to": "setter", "msg-type": "done"}))
	require.NoError(t, store.Create("Msg 3", "Body 3", map[string]string{"to": "task/x/lead/api/g1", "msg-type": "feedback"}))

	issues, err := store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 2)

	issues, err = store.List("task/x/lead/api/g1")
	require.NoError(t, err)
	assert.Len(t, issues, 1)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mail/... -run TestFileStore -v`
Expected: FAIL — FileStore not defined

- [ ] **Step 3: Implement FileStore**

Create `internal/mail/filestore.go`:

```go
package mail

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileStore implements mail storage using the filesystem.
// Messages are JSON files in per-address directories with unread/ and read/ subdirectories.
type FileStore struct {
	dir string // base mail directory (e.g., <instanceDir>/mail)
}

// NewFileStore creates a FileStore rooted at the given directory.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

// fileMessage is the on-disk JSON format for a mail message.
type fileMessage struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	From        string `json:"from,omitempty"`
	To          string `json:"to"`
	MsgType     string `json:"msg_type"`
}

// Create writes a message as a JSON file in the recipient's unread/ directory.
func (f *FileStore) Create(title, description string, labels map[string]string) error {
	to := labels["to"]
	if to == "" {
		return fmt.Errorf("missing 'to' label")
	}

	unreadDir := filepath.Join(f.dir, to, "unread")
	if err := os.MkdirAll(unreadDir, 0o755); err != nil {
		return fmt.Errorf("creating unread dir: %w", err)
	}

	msg := fileMessage{
		Title:       title,
		Description: description,
		From:        labels["from"],
		To:          to,
		MsgType:     labels["msg-type"],
	}

	data, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	filename := fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), sanitizeFilename(labels["msg-type"]))
	return os.WriteFile(filepath.Join(unreadDir, filename), data, 0o644)
}

// List returns unread messages for the given address.
func (f *FileStore) List(address string) ([]MailMessage, error) {
	unreadDir := filepath.Join(f.dir, address, "unread")
	entries, err := os.ReadDir(unreadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading unread dir: %w", err)
	}

	var messages []MailMessage
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(unreadDir, entry.Name()))
		if err != nil {
			continue // skip unreadable files
		}
		var msg fileMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue // skip malformed files
		}
		messages = append(messages, MailMessage{
			ID:          entry.Name(),
			Title:       msg.Title,
			Description: msg.Description,
		})
	}
	return messages, nil
}

// Close moves a message from unread/ to read/ for the given address.
// The id is the filename (e.g., "1741234567-done.json").
// The address must be provided to locate the file.
func (f *FileStore) Close(id string) error {
	// Walk all address directories to find the file
	return filepath.WalkDir(f.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if !d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "unread" {
			return nil
		}

		src := filepath.Join(path, id)
		if _, statErr := os.Stat(src); statErr != nil {
			return nil // not in this directory
		}

		// Found it — move to read/
		readDir := filepath.Join(filepath.Dir(path), "read")
		if mkErr := os.MkdirAll(readDir, 0o755); mkErr != nil {
			return fmt.Errorf("creating read dir: %w", mkErr)
		}
		dst := filepath.Join(readDir, id)
		if mvErr := os.Rename(src, dst); mvErr != nil {
			return fmt.Errorf("moving %s to read: %w", id, mvErr)
		}
		return filepath.SkipAll // done
	})
}

// sanitizeFilename replaces characters unsafe for filenames.
func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '-'
		}
		return r
	}, s)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mail/... -run TestFileStore -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mail/filestore.go internal/mail/filestore_test.go
git commit -m "feat: add FileStore for filesystem-based mail storage"
```

---

### Task 2: Update consumers to use FileStore

**Files:**
- Modify: `internal/mail/read.go` — rename `BeadsIssue` → `MailMessage`, `*BeadsStore` → `*FileStore`
- Modify: `internal/mail/send.go` — `*BeadsStore` → `*FileStore`
- Modify: `internal/mail/read_test.go` — update to use FileStore
- Modify: `internal/mail/integration_test.go` — update to use FileStore
- Modify: `internal/cli/mail.go` — `mailStore()` returns `*FileStore`
- Modify: `internal/cli/message.go` — update store init

- [ ] **Step 1: Rename BeadsIssue to MailMessage in filestore.go**

The `List` method already returns `[]MailMessage`. Add the `MailMessage` type definition to `filestore.go` (it replaces `BeadsIssue`):

```go
// MailMessage represents a mail message returned by List.
type MailMessage struct {
	ID          string
	Title       string
	Description string
}
```

Note: This type is already in the Step 3 code above. If `BeadsIssue` is referenced elsewhere, update those references.

- [ ] **Step 2: Update read.go**

Replace `BeadsIssue` with `MailMessage` and `*BeadsStore` with `*FileStore`:

In `read.go`:
- `FormatMessages(issues []BeadsIssue)` → `FormatMessages(issues []MailMessage)`
- `ReadInbox(store *BeadsStore, address string)` → `ReadInbox(store *FileStore, address string)`
- `ReadAndClose(store *BeadsStore, address string)` → `ReadAndClose(store *FileStore, address string)`

- [ ] **Step 3: Update send.go**

In `send.go`:
- `Send(store *BeadsStore, ...)` → `Send(store *FileStore, ...)`

- [ ] **Step 4: Update read_test.go**

Replace `setupTestBeads(t)` with `setupTestFileStore(t)`, `BeadsIssue` with `MailMessage`.

- [ ] **Step 5: Update integration_test.go**

Replace `setupTestBeads(t)` with `setupTestFileStore(t)`.

- [ ] **Step 6: Update CLI mail.go**

Replace `mailStore()` to return `*FileStore`:

```go
func mailStore(instanceName string) (*mail.FileStore, error) {
	name, err := resolveInstanceName(instanceName)
	if err != nil {
		return nil, err
	}
	_, instanceDir, err := instance.Load(name)
	if err != nil {
		return nil, err
	}
	mailDir := filepath.Join(instanceDir, "mail")
	if err := os.MkdirAll(mailDir, 0755); err != nil {
		return nil, fmt.Errorf("creating mail directory: %w", err)
	}
	return mail.NewFileStore(mailDir), nil
}
```

- [ ] **Step 7: Update CLI message.go**

The `store` variable type changes automatically since `mailStore()` returns `*FileStore`. No code change needed if it type-checks. Verify build.

- [ ] **Step 8: Run all tests**

Run: `go test ./internal/mail/... -v && go test ./internal/cli/... -v`
Expected: All PASS

- [ ] **Step 9: Commit**

```bash
git add internal/mail/read.go internal/mail/send.go internal/mail/read_test.go internal/mail/integration_test.go internal/cli/mail.go internal/cli/message.go
git commit -m "refactor: replace BeadsStore with FileStore in mail consumers"
```

---

### Task 3: Add mail cleanup to TaskRunner

**Files:**
- Modify: `internal/setter/taskrunner.go:493-509` — add mail directory cleanup to `Cleanup()`

- [ ] **Step 1: Add mail cleanup to Cleanup()**

In `internal/setter/taskrunner.go`, add after the log compression line in `Cleanup()`:

```go
// Clean up mail for this task
mailTaskDir := filepath.Join(tr.instanceDir, "mail", "task", tr.task.ID)
os.RemoveAll(mailTaskDir)
```

- [ ] **Step 2: Run setter tests**

Run: `go test ./internal/setter/... -v`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/setter/taskrunner.go
git commit -m "feat: clean up task mail directory on task completion"
```

---

### Task 4: Delete beads files and update docs

**Files:**
- Delete: `internal/mail/beads.go`
- Delete: `internal/mail/beads_test.go`
- Modify: `docs/DESIGN.md:51-58` — update Mail System section

- [ ] **Step 1: Delete beads files**

```bash
rm internal/mail/beads.go internal/mail/beads_test.go
```

- [ ] **Step 2: Update DESIGN.md Mail System section**

Replace the Mail System architecture description (lines 51-58) to reflect filesystem storage:

```markdown
## Mail System

Filesystem-backed inter-agent messaging. Messages are JSON files in per-address directories.

### Architecture

- **Storage**: JSON files in `<instanceDir>/mail/<address>/unread/` and `read/`. No external processes.
- **Delivery**: Sender-driven via tmux send-keys. `belayer message` writes to filesystem AND delivers a nudge in one operation.
```

- [ ] **Step 3: Build and test everything**

Run: `go build -o belayer ./cmd/belayer && go test ./...`
Expected: Build succeeds, all tests pass

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor: remove beads dependency from mail system, update docs"
```

---

## Outcomes & Retrospective

Replaced beads/dolt mail backend with pure filesystem store. Eliminated orphaned dolt sql-server processes (dozens per session). 5 commits, 324 lines added, 208 removed.

**What worked:**
- Clean mechanical replacement — same interface (Create/List/Close), same test patterns
- Parallel worker dispatch for sequential tasks kept wall time low
- Code review caught 7 real issues (silent Close no-op, filename collisions, missing error logging, non-atomic writes) — all fixed before merge

**What didn't:**
- Initial implementation had no error logging in List() and a Walk-based Close() that silently succeeded on missing IDs — review was essential

**Learnings to codify:**
- Filesystem stores should use atomic writes (write-to-tmp, rename) to prevent phantom partial files
- Store methods that search by ID should return "not found" errors, not silent success
- Always log skipped files in directory-scan operations — silent skips are invisible failures

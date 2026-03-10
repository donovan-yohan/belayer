# Manage Session Runtime Context Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace `belayer manage`'s `--system-prompt` with a full `.claude/` workspace (CLAUDE.md + slash commands) so the manage session naturally understands belayer context.

**Architecture:** Create a temp directory with `.claude/CLAUDE.md` (rendered from template with instance/repo data) and `.claude/commands/*.md` (static). Add `BELAYER_INSTANCE` env var fallback to `resolveInstanceName`. Exec `claude` from the temp dir so it picks up `.claude/` files naturally.

**Tech Stack:** Go, embed.FS, text/template, cobra CLI

---

### Task 1: Add BELAYER_INSTANCE env var fallback

**Files:**
- Modify: `internal/cli/helpers.go:13-25`
- Test: `internal/cli/helpers_test.go` (create)

**Step 1: Write the failing test**

Create `internal/cli/helpers_test.go`:

```go
package cli

import (
	"os"
	"testing"
)

func TestResolveInstanceName_EnvFallback(t *testing.T) {
	// When --instance flag is set, it wins
	name, err := resolveInstanceName("from-flag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "from-flag" {
		t.Errorf("expected 'from-flag', got %q", name)
	}

	// When flag is empty but BELAYER_INSTANCE is set, use env
	t.Setenv("BELAYER_INSTANCE", "from-env")
	name, err = resolveInstanceName("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "from-env" {
		t.Errorf("expected 'from-env', got %q", name)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestResolveInstanceName_EnvFallback -v`
Expected: FAIL — env var check doesn't exist yet, falls through to config load.

**Step 3: Implement the env var fallback**

Edit `internal/cli/helpers.go` — add env var check between flag check and config fallback:

```go
func resolveInstanceName(instanceName string) (string, error) {
	if instanceName != "" {
		return instanceName, nil
	}
	if envName := os.Getenv("BELAYER_INSTANCE"); envName != "" {
		return envName, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}
	if cfg.DefaultInstance == "" {
		return "", fmt.Errorf("no default instance set; use --instance or run `belayer instance create` first")
	}
	return cfg.DefaultInstance, nil
}
```

Add `"os"` to the import block.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestResolveInstanceName_EnvFallback -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/helpers.go internal/cli/helpers_test.go
git commit -m "feat(cli): add BELAYER_INSTANCE env var fallback to resolveInstanceName"
```

---

### Task 2: Create manage.md CLAUDE.md template

**Files:**
- Create: `internal/defaults/claudemd/manage.md`

This is the core file — it makes the manage session "know" it's a belayer session.

**Step 1: Create the template**

Create `internal/defaults/claudemd/manage.md`:

```markdown
# Belayer Manage Session

You are an interactive belayer assistant managing instance "{{.InstanceName}}".

## Your Identity

You ARE belayer. Every user request is a belayer operation. You don't need to be told to use belayer — you route all requests through belayer commands automatically.

## Instance Context

**Instance:** {{.InstanceName}}
**Repositories:**
{{range .RepoNames}}- {{.}}
{{end}}

## What You Do

- **Create tasks** — Help users design work items, write spec.md and goals.json, and publish tasks
- **Monitor progress** — Show task status, goal progress, and agent activity
- **Communicate with agents** — Send messages to running leads, read mail
- **View logs** — Show lead session output and debug issues

## Core Workflow: Task Creation

The primary workflow is creating tasks for the setter daemon to execute.

### 1. Understand the request

Ask clarifying questions. Understand what the user wants built, which repos are involved, and how the work decomposes.

### 2. Write spec.md

A specification document describing the work:
- Problem statement
- Requirements and acceptance criteria
- Technical constraints
- Relevant context

### 3. Write goals.json

Decompose the spec into per-repo goals:

```json
{
  "repos": {
    "<repo-name>": {
      "goals": [
        {
          "id": "<repo>-<n>",
          "description": "What this goal accomplishes",
          "depends_on": []
        }
      ]
    }
  }
}
```

Rules:
- Repo names MUST be one of: {{range $i, $name := .RepoNames}}{{if $i}}, {{end}}{{$name}}{{end}}
- Goal IDs must be unique across all repos
- `depends_on` references goals within the SAME repo only
- Independent goals run in parallel
- One clear deliverable per goal

### 4. Publish the task

```bash
belayer task create --spec spec.md --goals goals.json
```

Add `--jira PROJ-123` to link a Jira ticket.

## CLI Reference

| Command | Purpose |
|---------|---------|
| `belayer task create --spec FILE --goals FILE` | Create a task from spec and goals |
| `belayer task list` | List all tasks for this instance |
| `belayer status` | Show task and goal status |
| `belayer logs` | View lead session logs |
| `belayer message <address> --type <type> --body <body>` | Send message to an agent |
| `belayer mail read` | Read incoming mail |
| `belayer mail inbox` | Show unread message count |
| `belayer mail ack <id>` | Acknowledge a message |

All commands automatically use instance "{{.InstanceName}}" via the BELAYER_INSTANCE environment variable. You do not need to pass `--instance`.

## Jira Integration

If the user provides a Jira ticket, use available tools (MCP, curl, etc.) to fetch ticket details and convert to spec.md + goals.json format.

## Slash Commands

Use the available `/` commands for common operations — they handle the CLI invocation and output formatting for you.
```

**Step 2: Verify the template parses**

Run: `go test ./internal/manage/ -run TestBuildManageCLAUDEMD -v` (will write this test in Task 4)

For now, just verify it's valid Go template syntax by checking the build:

Run: `go build ./...`
Expected: Builds successfully (embed directive updated in next task)

**Step 3: Commit**

```bash
git add internal/defaults/claudemd/manage.md
git commit -m "feat: add manage session CLAUDE.md template"
```

---

### Task 3: Create static command files

**Files:**
- Create: `internal/defaults/commands/status.md`
- Create: `internal/defaults/commands/task-create.md`
- Create: `internal/defaults/commands/task-list.md`
- Create: `internal/defaults/commands/logs.md`
- Create: `internal/defaults/commands/message.md`
- Create: `internal/defaults/commands/mail.md`

**Step 1: Create all 6 command files**

Create `internal/defaults/commands/status.md`:

```markdown
---
description: Show task and goal status for the current belayer instance
argument-hint: "[task-id]"
allowed-tools: ["Bash", "Read"]
---

Run the belayer status command and present the results clearly.

If the user provides a task ID, show detailed status for that task:

```bash
belayer status [task-id]
```

Otherwise show overview status:

```bash
belayer status
```

Format the output for readability — group by task, show goal progress, highlight any failed or stuck items.
```

Create `internal/defaults/commands/task-create.md`:

```markdown
---
description: Create a new belayer task from spec and goals
argument-hint: "[--jira TICKET]"
allowed-tools: ["Bash", "Read", "Write"]
---

Guide the user through creating a belayer task:

1. If spec.md doesn't exist in the current directory, help the user write one
2. If goals.json doesn't exist, help decompose the spec into per-repo goals
3. Validate that goal repo names match available instance repos
4. Run:

```bash
belayer task create --spec spec.md --goals goals.json
```

If the user provides a `--jira` argument, append it:

```bash
belayer task create --spec spec.md --goals goals.json --jira <ticket>
```

After creation, show the task ID and suggest running `belayer status` to monitor.
```

Create `internal/defaults/commands/task-list.md`:

```markdown
---
description: List all tasks for the current belayer instance
allowed-tools: ["Bash"]
---

Run the belayer task list command and present results:

```bash
belayer task list
```

Format the output as a table showing status, task ID, goal count, and creation date.
```

Create `internal/defaults/commands/logs.md`:

```markdown
---
description: View lead session logs
argument-hint: "[task-id] [--goal GOAL] [--tail N]"
allowed-tools: ["Bash", "Read"]
---

Run the belayer logs command with any provided arguments:

```bash
belayer logs [arguments...]
```

Present the log output clearly. If the output is long, summarize key events and offer to show the full log.
```

Create `internal/defaults/commands/message.md`:

```markdown
---
description: Send a message to a running belayer agent
argument-hint: "<address> --type <type> --body <body>"
allowed-tools: ["Bash"]
---

Send a message to a belayer agent using the mail system.

```bash
belayer message <address> --type <type> --body "<body>"
```

Message types: goal_assignment, done, spot_result, verdict, feedback, instruction

Address format: `task/<task-id>/lead/<repo>/<goal-id>`

If the user doesn't specify all arguments, ask for:
1. Which agent to message (show running agents if possible via `belayer status`)
2. Message type (default: instruction)
3. Message body
```

Create `internal/defaults/commands/mail.md`:

```markdown
---
description: Check mail inbox for messages
argument-hint: "[read|inbox|ack <id>]"
allowed-tools: ["Bash"]
---

Check the belayer mail inbox:

- `belayer mail read` — Read and display all unread messages
- `belayer mail inbox` — Show unread message count
- `belayer mail ack <id>` — Acknowledge a specific message

Default to `belayer mail read` if no subcommand specified.
```

**Step 2: Verify files are well-formed**

Run: `ls internal/defaults/commands/`
Expected: 6 .md files listed

**Step 3: Commit**

```bash
git add internal/defaults/commands/
git commit -m "feat: add manage session slash commands"
```

---

### Task 4: Update embed directive and add PrepareManageDir function

**Files:**
- Modify: `internal/defaults/defaults.go:5-6`
- Modify: `internal/manage/prompt.go` (repurpose to workspace preparation)
- Modify: `internal/manage/prompt_test.go` (update tests)

**Step 1: Update embed directive**

Edit `internal/defaults/defaults.go` to include commands:

```go
package defaults

import "embed"

//go:embed belayer.toml profiles/*.toml claudemd/*.md commands/*.md
var FS embed.FS
```

**Step 2: Write the failing test**

Replace `internal/manage/prompt_test.go` with:

```go
package manage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareManageDir(t *testing.T) {
	dir := t.TempDir()

	err := PrepareManageDir(dir, PromptData{
		InstanceName: "my-project",
		RepoNames:    []string{"api", "frontend"},
	})
	if err != nil {
		t.Fatalf("PrepareManageDir() error: %v", err)
	}

	// Verify .claude/CLAUDE.md was written with rendered template
	claudeMD, err := os.ReadFile(filepath.Join(dir, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading CLAUDE.md: %v", err)
	}
	content := string(claudeMD)
	if !strings.Contains(content, `instance "my-project"`) {
		t.Error("CLAUDE.md should contain instance name")
	}
	if !strings.Contains(content, "api") || !strings.Contains(content, "frontend") {
		t.Error("CLAUDE.md should contain repo names")
	}

	// Verify commands were copied
	commands := []string{"status.md", "task-create.md", "task-list.md", "logs.md", "message.md", "mail.md"}
	for _, cmd := range commands {
		path := filepath.Join(dir, ".claude", "commands", cmd)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected command file %s to exist", cmd)
		}
	}
}

func TestPrepareManageDir_TemplateRendering(t *testing.T) {
	dir := t.TempDir()

	err := PrepareManageDir(dir, PromptData{
		InstanceName: "solo",
		RepoNames:    []string{"monorepo"},
	})
	if err != nil {
		t.Fatalf("PrepareManageDir() error: %v", err)
	}

	claudeMD, _ := os.ReadFile(filepath.Join(dir, ".claude", "CLAUDE.md"))
	content := string(claudeMD)

	if !strings.Contains(content, `instance "solo"`) {
		t.Error("CLAUDE.md should contain instance name")
	}
	if !strings.Contains(content, "monorepo") {
		t.Error("CLAUDE.md should contain repo name")
	}
	// Verify CLI reference section
	if !strings.Contains(content, "belayer task create") {
		t.Error("CLAUDE.md should contain CLI reference")
	}
}
```

**Step 3: Run test to verify it fails**

Run: `go test ./internal/manage/ -run TestPrepareManageDir -v`
Expected: FAIL — `PrepareManageDir` doesn't exist yet.

**Step 4: Implement PrepareManageDir**

Replace `internal/manage/prompt.go` with:

```go
package manage

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"

	"github.com/donovan-yohan/belayer/internal/defaults"
)

// PromptData holds the values injected into the manage CLAUDE.md template.
type PromptData struct {
	InstanceName string
	RepoNames    []string
}

// PrepareManageDir writes .claude/CLAUDE.md (rendered) and .claude/commands/*.md (static)
// to the given directory, preparing it as a workspace for the manage Claude session.
func PrepareManageDir(dir string, data PromptData) error {
	claudeDir := filepath.Join(dir, ".claude")
	commandsDir := filepath.Join(claudeDir, "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		return fmt.Errorf("creating .claude/commands: %w", err)
	}

	// Render and write CLAUDE.md
	tmplBytes, err := defaults.FS.ReadFile("claudemd/manage.md")
	if err != nil {
		return fmt.Errorf("reading manage template: %w", err)
	}

	tmpl, err := template.New("manage-claude-md").Parse(string(tmplBytes))
	if err != nil {
		return fmt.Errorf("parsing manage template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("rendering manage template: %w", err)
	}

	if err := os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing CLAUDE.md: %w", err)
	}

	// Copy static command files
	entries, err := defaults.FS.ReadDir("commands")
	if err != nil {
		return fmt.Errorf("reading commands dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := fs.ReadFile(defaults.FS, "commands/"+entry.Name())
		if err != nil {
			return fmt.Errorf("reading command %s: %w", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(commandsDir, entry.Name()), data, 0o644); err != nil {
			return fmt.Errorf("writing command %s: %w", entry.Name(), err)
		}
	}

	return nil
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/manage/ -run TestPrepareManageDir -v`
Expected: PASS

**Step 6: Run all tests**

Run: `go test ./...`
Expected: All pass (old BuildPrompt tests will fail — that's expected, we removed the function)

**Step 7: Commit**

```bash
git add internal/defaults/defaults.go internal/manage/prompt.go internal/manage/prompt_test.go
git commit -m "feat(manage): add PrepareManageDir for .claude/ workspace setup"
```

---

### Task 5: Update belayer manage to use PrepareManageDir

**Files:**
- Modify: `internal/cli/manage.go`

**Step 1: Write the failing test**

The existing `execClaude` var is testable. Add to a new file `internal/cli/manage_test.go`:

```go
package cli

import (
	"os"
	"testing"
)

func TestManageCmd_SetsEnvAndWorkdir(t *testing.T) {
	// Capture what execClaude receives
	var capturedDir string
	var capturedEnv []string
	origExec := execClaudeInDir
	execClaudeInDir = func(dir string, env []string) error {
		capturedDir = dir
		capturedEnv = env
		return nil
	}
	defer func() { execClaudeInDir = origExec }()

	// This test requires a valid instance, so we skip if not configured
	// The key thing is verifying the wiring — PrepareManageDir is tested separately
	t.Skip("integration test — requires configured instance")
}
```

Actually, the better approach is to just update manage.go and verify the build + existing tests pass, since PrepareManageDir is already well-tested.

**Step 2: Update manage.go**

Replace `internal/cli/manage.go`:

```go
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/manage"
	"github.com/spf13/cobra"
)

func newManageCmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "manage",
		Short: "Start an interactive agent session for task creation",
		Long:  "Launches a Claude Code session with belayer context. The session has slash commands for task creation, status, messaging, and more.",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			instConfig, _, err := instance.Load(name)
			if err != nil {
				return fmt.Errorf("loading instance %q: %w", name, err)
			}

			var repoNames []string
			for _, r := range instConfig.Repos {
				repoNames = append(repoNames, r.Name)
			}

			// Create temp workspace with .claude/ context
			tmpDir, err := os.MkdirTemp("", "belayer-manage-*")
			if err != nil {
				return fmt.Errorf("creating temp dir: %w", err)
			}
			// Note: no defer cleanup — process is replaced by exec

			if err := manage.PrepareManageDir(tmpDir, manage.PromptData{
				InstanceName: name,
				RepoNames:    repoNames,
			}); err != nil {
				return fmt.Errorf("preparing manage workspace: %w", err)
			}

			return execClaudeInDir(tmpDir, name)
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name (defaults to default instance)")
	return cmd
}

// execClaudeInDir replaces the current process with a claude session in the given directory.
// Sets BELAYER_INSTANCE env var so all belayer commands auto-resolve the instance.
var execClaudeInDir = func(dir string, instanceName string) error {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found in PATH: %w", err)
	}

	env := append(os.Environ(), "BELAYER_INSTANCE="+instanceName)

	// Change to the temp dir so claude picks up .claude/ files
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("changing to manage dir: %w", err)
	}

	return syscall.Exec(claudePath, []string{"claude"}, env)
}
```

**Step 3: Verify build**

Run: `go build ./...`
Expected: Builds successfully

**Step 4: Run all tests**

Run: `go test ./...`
Expected: All pass

**Step 5: Commit**

```bash
git add internal/cli/manage.go
git commit -m "feat(manage): use .claude/ workspace instead of --system-prompt"
```

---

### Task 6: Update CLAUDE.md with maintenance rule

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Add maintenance rule**

Add to the Key Patterns section of `CLAUDE.md`:

```markdown
- **Manage session context**: `internal/defaults/claudemd/manage.md` and `internal/defaults/commands/*.md` are deployed into `belayer manage` sessions. When CLI commands change, update these files. Verify during code review.
```

**Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add manage session maintenance rule to CLAUDE.md"
```

---

### Task 7: Clean up old execClaude and unused imports

**Files:**
- Modify: `internal/cli/manage.go` (remove old `execClaude` if still present)

**Step 1: Verify no other code references `execClaude`**

Run: `grep -r "execClaude" internal/`

If nothing references the old `execClaude` (the non-dir version), it was already replaced in Task 5.

If there are references in test files, update them to use `execClaudeInDir`.

**Step 2: Run full test suite**

Run: `go test ./...`
Expected: All pass

**Step 3: Commit if changes needed**

```bash
git add internal/
git commit -m "refactor: clean up old execClaude references"
```

---

### Task 8: Integration verification

**Step 1: Build belayer**

Run: `go build -o belayer ./cmd/belayer`
Expected: Builds successfully

**Step 2: Verify manage help text**

Run: `./belayer manage --help`
Expected: Shows updated description mentioning slash commands

**Step 3: Verify env var works**

Run: `BELAYER_INSTANCE=test ./belayer task list 2>&1 || true`
Expected: Either lists tasks or shows "loading instance" error (not "no default instance" error — proving env var was read)

**Step 4: Verify command files are embedded**

Run: `go test ./internal/manage/ -v`
Expected: All PrepareManageDir tests pass, commands are written to disk

**Step 5: Final commit (if any fixes needed)**

```bash
git add .
git commit -m "fix: integration verification fixes"
```

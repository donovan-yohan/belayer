package reflection

import (
	"fmt"
	"strings"
)

// ReflectionPromptContext holds the inputs for compiling the reflection agent's prompt.
type ReflectionPromptContext struct {
	SessionID  string
	EventsPath string // path to session events (or the events as text)
	MemoryDir  string // path to memory/system/ directory
	ArchiveDir string // path to memory/archive/ directory
}

// DefaultReflectionPrompt is the system prompt for the reflection agent.
// It describes the agent's role and what good reflection looks like.
// The reflection agent figures out the process — we just tell it where to look.
const DefaultReflectionPrompt = `You are the reflection agent for a belayer session. You run after sessions end.
Your job: review what happened and update the pilot's memory so future sessions are better.

You are NOT the pilot. You are NOT reviewing code. You are reviewing the session's
events and updating memory files so the pilot is smarter next time.

# What You Have Access To

- Session events (provided below) — the full event log including agent launches, messages, notes, and status changes
- Pilot memory files at the memory directory — what the pilot sees every session
- Archive directory — historical session records

# What Good Reflection Looks Like

- Generalize, don't memorize. "This codebase's API layer lacks input validation" not "file X had a bug on line 42"
- Check existing memory before writing. Don't duplicate. Update or replace stale entries.
- Core memory (system/ files) is expensive context — only promote truly durable learnings that will help across future sessions
- Archive detailed session records for recall, not for always-in-context
- If something contradicts existing memory, trust the fresh evidence and update
- Not every session produces learnings. If nothing generalizable happened, do nothing.

# What To Look For

- Patterns in what went wrong or caused friction
- User feedback or corrections that reveal preferences
- Codebase knowledge that would help future sessions (architecture, gotchas, conventions)
- Team dynamics — which agents needed more context, which produced good results
- Workflow patterns that worked well or poorly

# Output

Edit the memory files directly:
- system/ files for durable, generalizable learnings (persona, codebase knowledge, team patterns, user preferences)
- Write an archive entry summarizing this session's key events and outcomes
- Delete anything the session proved wrong or stale`

// CompileReflectionPrompt builds the full prompt for the reflection agent,
// injecting session-specific paths and event data.
func CompileReflectionPrompt(ctx ReflectionPromptContext) string {
	var b strings.Builder

	b.WriteString(DefaultReflectionPrompt)
	b.WriteString("\n")

	// Session context
	b.WriteString("\n# Session Details\n")
	fmt.Fprintf(&b, "Session ID: %s\n", ctx.SessionID)
	fmt.Fprintf(&b, "Memory directory: %s\n", ctx.MemoryDir)
	fmt.Fprintf(&b, "Archive directory: %s\n", ctx.ArchiveDir)

	// Events — injected as the task input
	if ctx.EventsPath != "" {
		b.WriteString("\n# Session Events\n")
		fmt.Fprintf(&b, "Events are available at: %s\n", ctx.EventsPath)
		b.WriteString("Read this file to understand what happened during the session.\n")
	}

	return b.String()
}

// FormatEventsForReflection formats session events as a readable text block
// suitable for injection into the reflection prompt.
func FormatEventsForReflection(events []SessionEvent) string {
	if len(events) == 0 {
		return "No events recorded for this session."
	}

	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "[%s] %s", e.Type, e.Data)
		if !strings.HasSuffix(e.Data, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// SessionEvent is a minimal event representation for reflection.
// Avoids importing the store package to prevent circular dependencies.
type SessionEvent struct {
	Type string
	Data string
}

package agent

import (
	"fmt"
	"strings"
)

// TeamMember describes another agent in the session for team roster rendering.
type TeamMember struct {
	Name   string
	Vendor string
	Model  string
	Role   string // human-readable description of what this agent does
}

// PromptContext holds all the inputs for compiling an agent's prompt.
type PromptContext struct {
	Config      AgentConfig
	TaskInput   string       // spec.md content or task description
	Team        []TeamMember // other agents in the session (for team roster)
	CoreMemory  string       // always-in-context memory (from memory/system/ files)
	MemoryIndex string       // index of archival memory files (names + descriptions)
	PersonalMem string       // personal agent memory (from .belayer/agents/{name}/memory/)
	RestartCtx  string       // restart context if resuming
	SessionID   string
}

// CompilePrompt assembles the full system prompt for an agent.
//
// Sections:
//   - Role: the agent's identity and behavioral instructions
//   - Task: what the agent should work on
//   - Your Team: other agents available in this session
//   - Your Memory: always-in-context core memory
//   - Memory Index: discoverable archival memory
//   - Belayer CLI: messaging and tooling commands
//   - Restart Context: prior session state (if resuming)
func CompilePrompt(ctx PromptContext) string {
	var b strings.Builder

	// Role
	b.WriteString("# Role\n")
	b.WriteString(ctx.Config.SystemPrompt)
	b.WriteString("\n")

	// Task
	b.WriteString("\n# Task\n")
	b.WriteString(ctx.TaskInput)
	b.WriteString("\n")

	// Your Team — rendered if there are other agents
	if len(ctx.Team) > 0 {
		b.WriteString("\n# Your Team\n")
		b.WriteString("Session: ")
		b.WriteString(ctx.SessionID)
		b.WriteString("\n\n")
		for _, m := range ctx.Team {
			fmt.Fprintf(&b, "- **%s** (%s/%s)", m.Name, m.Vendor, m.Model)
			if m.Role != "" {
				fmt.Fprintf(&b, " — %s", m.Role)
			}
			b.WriteString("\n")
		}
		b.WriteString("\nThese are your teammates. Use `belayer message` to communicate with them.\n")
	} else {
		b.WriteString("\n# Session\n")
		b.WriteString("Session: ")
		b.WriteString(ctx.SessionID)
		b.WriteString("\nYou are the only agent in this session.\n")
	}

	// Your Memory — always-in-context core memory
	if ctx.CoreMemory != "" {
		b.WriteString("\n# Your Memory\n")
		b.WriteString("<core_memory>\n")
		b.WriteString(ctx.CoreMemory)
		b.WriteString("\n</core_memory>\n")
	}

	// Memory Index — discoverable archival memory
	if ctx.MemoryIndex != "" {
		b.WriteString("\n# Memory Index\n")
		b.WriteString("The following memory files are available. Use `belayer recall` to search them.\n\n")
		b.WriteString(ctx.MemoryIndex)
		b.WriteString("\n")
	}

	// Personal Memory — omit section if empty
	if ctx.PersonalMem != "" {
		b.WriteString("\n# Personal Memory\n")
		b.WriteString(ctx.PersonalMem)
		b.WriteString("\n")
	}

	// Belayer CLI — always present
	b.WriteString("\n# Belayer CLI\n")
	b.WriteString("You are running inside a belayer session. Use these commands:\n")
	b.WriteString("- `belayer message send --to <agent> \"text\"` — send a message to another agent\n")
	b.WriteString("- `belayer message broadcast \"text\"` — broadcast to all agents\n")
	b.WriteString("- `belayer context` — see session info and other agents\n")
	b.WriteString("- `belayer recall \"query\"` — search archival memory\n")
	b.WriteString("- `belayer note \"observation\"` — log an observation for reflection\n")

	// Restart Context — omit section if empty
	if ctx.RestartCtx != "" {
		b.WriteString("\n# Restart Context\n")
		b.WriteString(ctx.RestartCtx)
		b.WriteString("\n")
	}

	return b.String()
}

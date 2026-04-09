package agent

import (
	"strings"
)

// PromptContext holds all the inputs for compiling an agent's prompt.
type PromptContext struct {
	Config        AgentConfig
	TaskInput     string   // spec.md content or task description
	CoreMemory    string   // core learnings content (from .belayer/learnings/core.md)
	PersonalMem   string   // personal agent memory (from .belayer/agents/{name}/memory/)
	StaleWarnings []string // warnings about stale learnings
	RestartCtx    string   // restart context if resuming
	OtherAgents   []string // names of other agents in the session
	SessionID     string
}

// CompilePrompt assembles the full system prompt for an agent.
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

	// Session Context
	b.WriteString("\n# Session Context\n")
	b.WriteString("Session: ")
	b.WriteString(ctx.SessionID)
	b.WriteString("\n")
	b.WriteString("Other agents in this session: ")
	b.WriteString(strings.Join(ctx.OtherAgents, ", "))
	b.WriteString("\n")

	// Core Learnings
	b.WriteString("\n# Core Learnings\n")
	if ctx.CoreMemory == "" {
		b.WriteString("No core learnings available.\n")
	} else {
		b.WriteString(ctx.CoreMemory)
		b.WriteString("\n")
	}

	// Personal Memory — omit section if empty
	if ctx.PersonalMem != "" {
		b.WriteString("\n# Personal Memory\n")
		b.WriteString(ctx.PersonalMem)
		b.WriteString("\n")
	}

	// Stale Warnings — omit section if none
	if len(ctx.StaleWarnings) > 0 {
		b.WriteString("\n# Stale Warnings\n")
		for _, w := range ctx.StaleWarnings {
			b.WriteString("WARNING: ")
			b.WriteString(w)
			b.WriteString("\n")
		}
	}

	// Belayer CLI — always present
	b.WriteString("\n# Belayer CLI (Messaging Plane)\n")
	b.WriteString("You are running inside a belayer session. Use these commands:\n")
	b.WriteString("- `belayer message send --to <agent> \"text\"` — send a message to another agent\n")
	b.WriteString("- `belayer message broadcast \"text\"` — broadcast to all agents\n")
	b.WriteString("- `belayer context` — see session info and other agents\n")
	b.WriteString("- `belayer recall search \"query\"` — search archival learnings\n")
	b.WriteString("- `belayer note \"observation\"` — flag an observation for reflection\n")
	b.WriteString("- `belayer tool run <name> --input '{}'` — run a registered tool\n")

	// Restart Context — omit section if empty
	if ctx.RestartCtx != "" {
		b.WriteString("\n# Restart Context\n")
		b.WriteString(ctx.RestartCtx)
		b.WriteString("\n")
	}

	return b.String()
}

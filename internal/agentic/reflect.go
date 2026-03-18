package agentic

import (
	"context"
	"fmt"
	"strings"

	"github.com/donovan-yohan/belayer/internal/model"
)

// ReflectInput is the structured input to the reflect agentic node.
type ReflectInput struct {
	ProblemID   string
	ProblemSpec string
	Events      []model.Event
	CragID      string
}

// ReflectOutput is the structured output from the reflect agentic node.
type ReflectOutput struct {
	Learnings []ReflectLearning `json:"learnings"`
	Summary   string            `json:"summary"`
}

// ReflectLearning is a single learning extracted by the reflect node.
type ReflectLearning struct {
	Category       model.LearningCategory `json:"category"`
	Description    string                 `json:"description"`
	Recommendation string                 `json:"recommendation"`
	Severity       model.LearningSeverity `json:"severity"`
}

// RunReflect runs the reflect agentic node which classifies errors from a
// completed problem and extracts learnings.
func RunReflect(ctx context.Context, modelName string, input ReflectInput) (*ReflectOutput, error) {
	prompt := buildReflectPrompt(input)
	var out ReflectOutput
	if err := RunNodeJSON(ctx, NodeConfig{Model: modelName, Prompt: prompt}, &out); err != nil {
		return nil, fmt.Errorf("reflect node: %w", err)
	}
	return &out, nil
}

func buildReflectPrompt(input ReflectInput) string {
	var sb strings.Builder

	sb.WriteString("You are a reflect node analyzing a completed belayer problem. ")
	sb.WriteString("Your job is to classify errors and extract learnings that will help future problems.\n\n")

	sb.WriteString("## Problem\n\n")
	sb.WriteString(fmt.Sprintf("ID: %s\nCrag: %s\n\n", input.ProblemID, input.CragID))
	sb.WriteString("### Spec\n")
	sb.WriteString(input.ProblemSpec)
	sb.WriteString("\n\n")

	sb.WriteString("## Event History\n\n")
	for _, e := range input.Events {
		sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", e.CreatedAt.Format("15:04:05"), e.Type, truncate(e.Payload, 200)))
	}
	sb.WriteString("\n\n")

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("Analyze the problem's event history and classify what went wrong or could be improved.\n\n")
	sb.WriteString("For each issue, create a learning with:\n")
	sb.WriteString("- **category**: one of: test_gap, spec_ambiguity, infra_issue, review_miss, pattern\n")
	sb.WriteString("- **description**: what happened\n")
	sb.WriteString("- **recommendation**: what to do differently next time\n")
	sb.WriteString("- **severity**: high, medium, or low\n\n")
	sb.WriteString("Look for:\n")
	sb.WriteString("- How many review loop cycles did leads need? (correction_climb_created, spotter_correction_loop events)\n")
	sb.WriteString("- Did the spotter find issues the lead review missed? (spotter verdict events with pass=false)\n")
	sb.WriteString("- Did the anchor reject? (anchor verdict events with reject)\n")
	sb.WriteString("- Were there stuck leads? (stuck events)\n")
	sb.WriteString("- Were there needs_human escalations?\n\n")
	sb.WriteString("Also write a brief human-readable summary of the problem's execution.\n\n")
	sb.WriteString("Respond with ONLY a JSON object:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"learnings\": [\n")
	sb.WriteString("    {\"category\": \"...\", \"description\": \"...\", \"recommendation\": \"...\", \"severity\": \"...\"}\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"summary\": \"Brief execution summary\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n")
	sb.WriteString("If no learnings are needed (clean execution), return an empty learnings array.")

	return sb.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

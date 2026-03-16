package agentic

import (
	"context"
	"fmt"
	"strings"

	"github.com/donovan-yohan/belayer/internal/model"
)

// CompactionOutput is the output from the learning compaction node.
type CompactionOutput struct {
	CompactedLearnings []ReflectLearning `json:"compacted_learnings"`
	ResolvedIDs        []string          `json:"resolved_ids"`
	Summary            string            `json:"summary"`
}

// RunCompaction runs the learning compaction agentic node which merges
// duplicates, archives resolved issues, and distills recurring patterns.
func RunCompaction(ctx context.Context, modelName string, learnings []model.Learning) (*CompactionOutput, error) {
	prompt := buildCompactionPrompt(learnings)
	var out CompactionOutput
	if err := RunNodeJSON(ctx, NodeConfig{Model: modelName, Prompt: prompt}, &out); err != nil {
		return nil, fmt.Errorf("learning compaction node: %w", err)
	}
	return &out, nil
}

func buildCompactionPrompt(learnings []model.Learning) string {
	var sb strings.Builder

	sb.WriteString("You are a learning compaction node for belayer. ")
	sb.WriteString("Your job is to consolidate a set of learnings by merging duplicates, ")
	sb.WriteString("identifying resolved issues, and distilling recurring patterns into principles.\n\n")

	sb.WriteString("## Current Learnings\n\n")
	for _, l := range learnings {
		resolved := ""
		if l.Resolved {
			resolved = " [RESOLVED]"
		}
		sb.WriteString(fmt.Sprintf("### %s [%s] (%s)%s\n", l.ID, l.Category, l.Severity, resolved))
		if l.ProblemID != "" {
			sb.WriteString(fmt.Sprintf("**Problem:** %s\n", l.ProblemID))
		}
		sb.WriteString(fmt.Sprintf("**Description:** %s\n", l.Description))
		sb.WriteString(fmt.Sprintf("**Recommendation:** %s\n", l.Recommendation))
		sb.WriteString(fmt.Sprintf("**Access count:** %d\n\n", l.AccessCount))
	}

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("1. **Merge duplicates**: If multiple learnings describe the same issue, combine them into one.\n")
	sb.WriteString("2. **Identify resolved**: If a learning describes something that later learnings show was fixed, mark it as resolved.\n")
	sb.WriteString("3. **Distill patterns**: If 3+ learnings describe variations of the same theme, create a single principle-level learning.\n")
	sb.WriteString("4. **Preserve unique insights**: Don't merge learnings that happen to share a category but describe different issues.\n\n")
	sb.WriteString("Respond with ONLY a JSON object:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"compacted_learnings\": [\n")
	sb.WriteString("    {\"category\": \"...\", \"description\": \"...\", \"recommendation\": \"...\", \"severity\": \"...\"}\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"resolved_ids\": [\"ids-of-learnings-to-mark-resolved\"],\n")
	sb.WriteString("  \"summary\": \"Brief description of what was compacted\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n")
	sb.WriteString("compacted_learnings should contain the NEW compacted entries (merged, distilled). ")
	sb.WriteString("resolved_ids should list ALL existing learning IDs that are subsumed by the compacted entries or are no longer relevant.")

	return sb.String()
}

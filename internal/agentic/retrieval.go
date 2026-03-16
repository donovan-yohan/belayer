package agentic

import (
	"context"
	"fmt"
	"strings"

	"github.com/donovan-yohan/belayer/internal/model"
)

// RetrievalInput is the input to the learning retrieval agentic node.
type RetrievalInput struct {
	ProblemSpec string
	Learnings  []model.Learning
}

// RetrievalOutput is the output from the learning retrieval node.
type RetrievalOutput struct {
	RelevantLearnings []RelevantLearning `json:"relevant_learnings"`
	SetterGuidance    string             `json:"setter_guidance"`
}

// RelevantLearning is a single learning deemed relevant to the current problem.
type RelevantLearning struct {
	ID             string `json:"id"`
	Description    string `json:"description"`
	Recommendation string `json:"recommendation"`
	Relevance      string `json:"relevance"`
}

// RunRetrieval runs the learning retrieval agentic node which selects
// relevant learnings for a new problem spec.
func RunRetrieval(ctx context.Context, modelName string, input RetrievalInput) (*RetrievalOutput, error) {
	prompt := buildRetrievalPrompt(input)
	var out RetrievalOutput
	if err := RunNodeJSON(ctx, NodeConfig{Model: modelName, Prompt: prompt}, &out); err != nil {
		return nil, fmt.Errorf("learning retrieval node: %w", err)
	}
	return &out, nil
}

func buildRetrievalPrompt(input RetrievalInput) string {
	var sb strings.Builder

	sb.WriteString("You are a learning retrieval node for belayer. ")
	sb.WriteString("Given a new problem spec and a list of past learnings, select the learnings that are relevant to this problem.\n\n")

	sb.WriteString("## New Problem Spec\n\n")
	sb.WriteString(input.ProblemSpec)
	sb.WriteString("\n\n")

	sb.WriteString("## Past Learnings\n\n")
	for _, l := range input.Learnings {
		sb.WriteString(fmt.Sprintf("### Learning %s [%s] (%s)\n", l.ID, l.Category, l.Severity))
		sb.WriteString(fmt.Sprintf("**Description:** %s\n", l.Description))
		sb.WriteString(fmt.Sprintf("**Recommendation:** %s\n\n", l.Recommendation))
	}

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("Select learnings that are relevant to the new problem. A learning is relevant if:\n")
	sb.WriteString("- The problem involves similar technology, domain, or patterns\n")
	sb.WriteString("- The learning's recommendation would improve this problem's spec or execution\n")
	sb.WriteString("- Past failures in similar areas could recur\n\n")
	sb.WriteString("Also write setter_guidance — a brief message to the setter about what to watch out for based on past learnings.\n\n")
	sb.WriteString("Respond with ONLY a JSON object:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"relevant_learnings\": [\n")
	sb.WriteString("    {\"id\": \"...\", \"description\": \"...\", \"recommendation\": \"...\", \"relevance\": \"Why this matters for the new problem\"}\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"setter_guidance\": \"Brief guidance for the setter\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n")
	sb.WriteString("If no learnings are relevant, return empty arrays and empty guidance.")

	return sb.String()
}

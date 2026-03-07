package intake

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// AgenticExecutor abstracts the execution of agentic nodes for testability.
type AgenticExecutor interface {
	Execute(ctx context.Context, prompt string) (string, error)
}

// SufficiencyOutput is the expected JSON from the sufficiency check agentic node.
type SufficiencyOutput struct {
	Sufficient bool     `json:"sufficient"`
	Gaps       []string `json:"gaps"`
}

// PipelineConfig configures the intake pipeline.
type PipelineConfig struct {
	Description  string   // Raw description (for text source)
	JiraTickets  []string // Jira ticket IDs (for jira source)
	RepoNames    []string // Available repo names from the instance
	NoBrainstorm bool     // Skip interactive brainstorm even if insufficient
	Stdin        io.Reader
	Stdout       io.Writer
}

// PipelineResult is the output of a successful intake pipeline run.
type PipelineResult struct {
	Description        string // Enriched description
	Source             string // "text" or "jira"
	SourceRef          string // Original reference (ticket IDs or empty)
	SufficiencyChecked bool
}

// Pipeline orchestrates the task intake flow:
// parse input -> sufficiency check -> optional brainstorm -> enriched result.
type Pipeline struct {
	executor AgenticExecutor
}

// NewPipeline creates a new intake pipeline.
func NewPipeline(executor AgenticExecutor) *Pipeline {
	return &Pipeline{executor: executor}
}

// Run executes the intake pipeline and returns an enriched result.
func (p *Pipeline) Run(ctx context.Context, cfg PipelineConfig) (*PipelineResult, error) {
	// Step 1: Parse input
	description, source, sourceRef := p.parseInput(cfg)

	// Step 2: Sufficiency check
	suffOutput, err := p.checkSufficiency(ctx, description, cfg.RepoNames)
	if err != nil {
		// Sufficiency check failed — proceed without it
		fmt.Fprintf(cfg.Stdout, "Warning: sufficiency check failed: %v\n", err)
		return &PipelineResult{
			Description:        description,
			Source:             source,
			SourceRef:          sourceRef,
			SufficiencyChecked: false,
		}, nil
	}

	// Step 3: Brainstorm if insufficient
	if !suffOutput.Sufficient && len(suffOutput.Gaps) > 0 && !cfg.NoBrainstorm {
		enriched, err := p.brainstorm(cfg.Stdin, cfg.Stdout, description, suffOutput.Gaps)
		if err != nil {
			return nil, fmt.Errorf("brainstorm: %w", err)
		}
		description = enriched
	} else if !suffOutput.Sufficient && len(suffOutput.Gaps) > 0 && cfg.NoBrainstorm {
		fmt.Fprintf(cfg.Stdout, "Sufficiency check found gaps (brainstorm skipped):\n")
		for _, gap := range suffOutput.Gaps {
			fmt.Fprintf(cfg.Stdout, "  - %s\n", gap)
		}
	}

	return &PipelineResult{
		Description:        description,
		Source:             source,
		SourceRef:          sourceRef,
		SufficiencyChecked: true,
	}, nil
}

// parseInput normalizes the input into a description, source, and sourceRef.
func (p *Pipeline) parseInput(cfg PipelineConfig) (description, source, sourceRef string) {
	if len(cfg.JiraTickets) > 0 {
		source = "jira"
		sourceRef = strings.Join(cfg.JiraTickets, ",")
		description = fmt.Sprintf("Jira tickets: %s", strings.Join(cfg.JiraTickets, ", "))
		if cfg.Description != "" {
			description += "\n\nAdditional context:\n" + cfg.Description
		}
		return
	}

	source = "text"
	sourceRef = ""
	description = cfg.Description
	return
}

// checkSufficiency runs the sufficiency agentic node.
func (p *Pipeline) checkSufficiency(ctx context.Context, description string, repoNames []string) (*SufficiencyOutput, error) {
	repoList := "none specified"
	if len(repoNames) > 0 {
		repoList = strings.Join(repoNames, ", ")
	}

	prompt := fmt.Sprintf(
		`You are a task sufficiency checker for a multi-repo coding orchestrator.

Evaluate whether this task description has enough context to be decomposed into per-repo implementation specs.

Available repos: %s

Task description:
%s

Respond with JSON: {"sufficient": true/false, "gaps": ["specific question about missing context"]}`,
		repoList, description,
	)

	output, err := p.executor.Execute(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("executing sufficiency check: %w", err)
	}

	var result SufficiencyOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Errorf("parsing sufficiency output: %w", err)
	}

	return &result, nil
}

// brainstorm runs an interactive Q&A loop to fill in gaps.
func (p *Pipeline) brainstorm(stdin io.Reader, stdout io.Writer, description string, gaps []string) (string, error) {
	fmt.Fprintf(stdout, "\nThe task needs more context. Please answer the following questions:\n\n")

	scanner := bufio.NewScanner(stdin)
	var answers []string

	for i, gap := range gaps {
		fmt.Fprintf(stdout, "Q%d: %s\n> ", i+1, gap)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", fmt.Errorf("reading input: %w", err)
			}
			// EOF — user cancelled
			break
		}
		answer := strings.TrimSpace(scanner.Text())
		if answer != "" {
			answers = append(answers, fmt.Sprintf("Q: %s\nA: %s", gap, answer))
		}
	}

	if len(answers) == 0 {
		return description, nil
	}

	enriched := description + "\n\nAdditional context from brainstorm:\n" + strings.Join(answers, "\n\n")
	return enriched, nil
}

// ParseJiraTickets splits a comma-separated list of Jira ticket IDs,
// trimming whitespace from each.
func ParseJiraTickets(input string) []string {
	if input == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	var tickets []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			tickets = append(tickets, t)
		}
	}
	return tickets
}

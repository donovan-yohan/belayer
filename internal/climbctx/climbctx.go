package climbctx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LeadClimb is the GOAL.json context for a lead agent.
type LeadClimb struct {
	Role            string `json:"role"`
	ProblemSpec     string `json:"problem_spec"`
	ClimbID         string `json:"climb_id"`
	RepoName        string `json:"repo_name"`
	Description     string `json:"description"`
	Attempt         int    `json:"attempt"`
	SpotterFeedback string `json:"spotter_feedback,omitempty"`
}

// ClimbTopSummary contains the TOP.json data for a completed climb.
type ClimbTopSummary struct {
	ClimbID     string `json:"climb_id"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Summary     string `json:"summary"`
	Notes       string `json:"notes,omitempty"`
}

// SpotterClimb is the GOAL.json context for a spotter agent.
type SpotterClimb struct {
	Role        string            `json:"role"`
	RepoName    string            `json:"repo_name"`
	ProblemSpec string            `json:"problem_spec"`
	ClimbTops   []ClimbTopSummary `json:"climb_tops"`
	WorkDir     string            `json:"work_dir"`
	Profiles    map[string]string `json:"profiles"`
}

// AnchorClimb is the GOAL.json context for an anchor agent.
type AnchorClimb struct {
	Role        string         `json:"role"`
	ProblemSpec string         `json:"problem_spec"`
	RepoDiffs   []RepoDiff     `json:"repo_diffs"`
	Summaries   []ClimbSummary `json:"summaries"`
}

// RepoDiff contains git diff output for a single repo.
type RepoDiff struct {
	RepoName string `json:"repo_name"`
	DiffStat string `json:"diff_stat"`
	Diff     string `json:"diff"`
}

// ClimbSummary contains the completion summary for a single climb.
type ClimbSummary struct {
	ClimbID     string `json:"climb_id"`
	RepoName    string `json:"repo_name"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	Summary     string `json:"summary"`
	Notes       string `json:"notes,omitempty"`
}

// WriteClimbJSON writes the climb context to <dir>/.lead/<climbID>/GOAL.json.
func WriteClimbJSON(dir string, climbID string, climb any) error {
	climbDir := filepath.Join(dir, ".lead", climbID)
	if err := os.MkdirAll(climbDir, 0o755); err != nil {
		return fmt.Errorf("creating .lead/%s directory: %w", climbID, err)
	}

	data, err := json.MarshalIndent(climb, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling GOAL.json: %w", err)
	}

	goalPath := filepath.Join(climbDir, "GOAL.json")
	if err := os.WriteFile(goalPath, data, 0o644); err != nil {
		return fmt.Errorf("writing GOAL.json: %w", err)
	}

	return nil
}

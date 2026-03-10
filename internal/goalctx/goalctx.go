package goalctx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LeadGoal is the GOAL.json context for a lead agent.
type LeadGoal struct {
	Role            string `json:"role"`
	TaskSpec        string `json:"task_spec"`
	GoalID          string `json:"goal_id"`
	RepoName        string `json:"repo_name"`
	Description     string `json:"description"`
	Attempt         int    `json:"attempt"`
	SpotterFeedback string `json:"spotter_feedback,omitempty"`
}

// SpotterGoal is the GOAL.json context for a spotter agent.
type SpotterGoal struct {
	Role        string            `json:"role"`
	GoalID      string            `json:"goal_id"`
	RepoName    string            `json:"repo_name"`
	Description string            `json:"description"`
	WorkDir     string            `json:"work_dir"`
	Profiles    map[string]string `json:"profiles"`
	DoneJSON    string            `json:"done_json"`
}

// AnchorGoal is the GOAL.json context for an anchor agent.
type AnchorGoal struct {
	Role      string        `json:"role"`
	TaskSpec  string        `json:"task_spec"`
	RepoDiffs []RepoDiff    `json:"repo_diffs"`
	Summaries []GoalSummary `json:"summaries"`
}

// RepoDiff contains git diff output for a single repo.
type RepoDiff struct {
	RepoName string `json:"repo_name"`
	DiffStat string `json:"diff_stat"`
	Diff     string `json:"diff"`
}

// GoalSummary contains the completion summary for a single goal.
type GoalSummary struct {
	GoalID      string `json:"goal_id"`
	RepoName    string `json:"repo_name"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	Summary     string `json:"summary"`
	Notes       string `json:"notes,omitempty"`
}

// WriteGoalJSON writes the goal context to <dir>/.lead/<goalID>/GOAL.json.
func WriteGoalJSON(dir string, goalID string, goal any) error {
	goalDir := filepath.Join(dir, ".lead", goalID)
	if err := os.MkdirAll(goalDir, 0o755); err != nil {
		return fmt.Errorf("creating .lead/%s directory: %w", goalID, err)
	}

	data, err := json.MarshalIndent(goal, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling GOAL.json: %w", err)
	}

	goalPath := filepath.Join(goalDir, "GOAL.json")
	if err := os.WriteFile(goalPath, data, 0o644); err != nil {
		return fmt.Errorf("writing GOAL.json: %w", err)
	}

	return nil
}

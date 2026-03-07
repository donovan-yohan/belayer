package lead

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/model"
)

func TestParseLeadEvent(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *model.LeadEvent
		wantErr bool
	}{
		{
			name:  "started event",
			input: `{"type":"started"}`,
			want:  &model.LeadEvent{Type: "started"},
		},
		{
			name:  "goal_started event",
			input: `{"type":"goal_started","goal":0,"attempt":1,"description":"Implement feature"}`,
			want:  &model.LeadEvent{Type: "goal_started", Goal: 0, Attempt: 1, Description: "Implement feature"},
		},
		{
			name:  "goal_executing event",
			input: `{"type":"goal_executing","goal":1,"attempt":2}`,
			want:  &model.LeadEvent{Type: "goal_executing", Goal: 1, Attempt: 2},
		},
		{
			name: "goal_verdict pass",
			input: `{"type":"goal_verdict","goal":0,"attempt":1,"pass":true,"summary":"All good"}`,
			want: func() *model.LeadEvent {
				pass := true
				return &model.LeadEvent{Type: "goal_verdict", Goal: 0, Attempt: 1, Pass: &pass, Summary: "All good"}
			}(),
		},
		{
			name: "goal_verdict fail",
			input: `{"type":"goal_verdict","goal":0,"attempt":1,"pass":false,"summary":"Issues found"}`,
			want: func() *model.LeadEvent {
				pass := false
				return &model.LeadEvent{Type: "goal_verdict", Goal: 0, Attempt: 1, Pass: &pass, Summary: "Issues found"}
			}(),
		},
		{
			name:  "complete event",
			input: `{"type":"complete"}`,
			want:  &model.LeadEvent{Type: "complete"},
		},
		{
			name:  "stuck event",
			input: `{"type":"stuck"}`,
			want:  &model.LeadEvent{Type: "stuck"},
		},
		{
			name:  "error event",
			input: `{"type":"error","error":"spec.md not found"}`,
			want:  &model.LeadEvent{Type: "error", Error: "spec.md not found"},
		},
		{
			name:    "empty line",
			input:   "",
			wantErr: true,
		},
		{
			name:    "non-JSON line",
			input:   "this is not json",
			wantErr: true,
		},
		{
			name:    "JSON without type",
			input:   `{"goal":0}`,
			wantErr: true,
		},
		{
			name:  "with whitespace",
			input: `  {"type":"started"}  `,
			want:  &model.LeadEvent{Type: "started"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLeadEvent(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLeadEvent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if got.Goal != tt.want.Goal {
				t.Errorf("Goal = %d, want %d", got.Goal, tt.want.Goal)
			}
			if got.Attempt != tt.want.Attempt {
				t.Errorf("Attempt = %d, want %d", got.Attempt, tt.want.Attempt)
			}
			if got.Description != tt.want.Description {
				t.Errorf("Description = %q, want %q", got.Description, tt.want.Description)
			}
			if got.Summary != tt.want.Summary {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.want.Summary)
			}
			if got.Error != tt.want.Error {
				t.Errorf("Error = %q, want %q", got.Error, tt.want.Error)
			}
			if (got.Pass == nil) != (tt.want.Pass == nil) {
				t.Errorf("Pass nil mismatch: got %v, want %v", got.Pass, tt.want.Pass)
			} else if got.Pass != nil && tt.want.Pass != nil && *got.Pass != *tt.want.Pass {
				t.Errorf("Pass = %v, want %v", *got.Pass, *tt.want.Pass)
			}
		})
	}
}

func TestSetupLeadDir(t *testing.T) {
	tmpDir := t.TempDir()
	worktreePath := filepath.Join(tmpDir, "worktree")
	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatalf("creating worktree dir: %v", err)
	}

	store := &Store{} // Not used for setup
	runner := NewRunner(store)

	cfg := RunConfig{
		WorktreePath: worktreePath,
		LeadID:       "test-lead",
		TaskID:       "test-task",
		Spec:         "# Test Spec\nImplement the thing.",
		Goals: []model.GoalSpec{
			{Index: 0, Description: "Implement feature A"},
			{Index: 1, Description: "Write tests"},
		},
		MaxAttempts:  3,
		ExecuteModel: "claude-sonnet-4-6",
		ReviewModel:  "claude-sonnet-4-6",
	}

	if err := runner.setupLeadDir(cfg); err != nil {
		t.Fatalf("setupLeadDir: %v", err)
	}

	// Verify .lead/ directory exists
	leadDir := filepath.Join(worktreePath, ".lead")
	if _, err := os.Stat(leadDir); os.IsNotExist(err) {
		t.Fatal(".lead/ directory not created")
	}

	// Verify spec.md
	specContent, err := os.ReadFile(filepath.Join(leadDir, "spec.md"))
	if err != nil {
		t.Fatalf("reading spec.md: %v", err)
	}
	if string(specContent) != cfg.Spec {
		t.Errorf("spec.md content mismatch: got %q, want %q", string(specContent), cfg.Spec)
	}

	// Verify goals.json
	goalsContent, err := os.ReadFile(filepath.Join(leadDir, "goals.json"))
	if err != nil {
		t.Fatalf("reading goals.json: %v", err)
	}
	var goals []model.GoalSpec
	if err := json.Unmarshal(goalsContent, &goals); err != nil {
		t.Fatalf("parsing goals.json: %v", err)
	}
	if len(goals) != 2 {
		t.Errorf("expected 2 goals, got %d", len(goals))
	}
	if goals[0].Description != "Implement feature A" {
		t.Errorf("goal 0 description = %q, want %q", goals[0].Description, "Implement feature A")
	}

	// Verify lead.sh exists and is executable
	scriptPath := filepath.Join(leadDir, "lead.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("stat lead.sh: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("lead.sh is not executable")
	}

	// Verify output/ directory
	outputDir := filepath.Join(leadDir, "output")
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		t.Fatal("output/ directory not created")
	}
}

func TestRunnerWithMockClaude(t *testing.T) {
	// Create a mock claude script that simulates successful execution
	tmpDir := t.TempDir()

	// Create a fake claude command
	mockClaudePath := filepath.Join(tmpDir, "claude")
	mockScript := `#!/usr/bin/env bash
# Mock claude command for testing.
# When called with review prompt (contains "verdict"), write verdict.json.
for arg in "$@"; do
    if echo "$arg" | grep -q "verdict"; then
        echo '{"pass":true,"summary":"mock review passed","issues":[]}' > .lead/verdict.json
        break
    fi
done
echo "Mock claude output"
`
	if err := os.WriteFile(mockClaudePath, []byte(mockScript), 0755); err != nil {
		t.Fatalf("writing mock claude: %v", err)
	}

	// Set up database
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := database.Migrate(); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	conn := database.Conn()
	_, _ = conn.Exec("INSERT INTO instances (id, name, path) VALUES (?, ?, ?)", "test-inst", "test-inst", "/tmp/test")
	_, _ = conn.Exec("INSERT INTO tasks (id, instance_id, description) VALUES (?, ?, ?)", "task-1", "test-inst", "test task")
	_, _ = conn.Exec("INSERT INTO task_repos (id, task_id, repo_name) VALUES (?, ?, ?)", "tr-1", "task-1", "test-repo")
	_, _ = conn.Exec("INSERT INTO leads (id, task_repo_id, status, attempt) VALUES (?, ?, ?, ?)", "lead-1", "tr-1", "pending", 1)

	store := NewStore(conn)
	runner := NewRunner(store)

	// Create worktree directory
	worktreePath := filepath.Join(tmpDir, "worktree")
	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatalf("creating worktree dir: %v", err)
	}

	cfg := RunConfig{
		WorktreePath: worktreePath,
		LeadID:       "lead-1",
		TaskID:       "task-1",
		Spec:         "# Test Spec\nImplement a hello world function.",
		Goals: []model.GoalSpec{
			{Index: 0, Description: "Implement hello world"},
		},
		MaxAttempts:  2,
		ExecuteModel: "claude-sonnet-4-6",
		ReviewModel:  "claude-sonnet-4-6",
	}

	// Prepend mock claude to PATH
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	ctx := context.Background()
	result, err := runner.Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Status != model.LeadStatusComplete {
		t.Errorf("expected status %q, got %q (output: %s)", model.LeadStatusComplete, result.Status, result.Output)
	}

	// Verify lead was updated in DB
	lead, err := store.GetLead("lead-1")
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	if lead.Status != model.LeadStatusComplete {
		t.Errorf("DB lead status = %q, want %q", lead.Status, model.LeadStatusComplete)
	}
	if lead.StartedAt == nil {
		t.Error("expected started_at to be set")
	}
	if lead.FinishedAt == nil {
		t.Error("expected finished_at to be set")
	}

	// Verify lead goals were created
	goals, err := store.GetLeadGoals("lead-1")
	if err != nil {
		t.Fatalf("GetLeadGoals: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("expected 1 goal, got %d", len(goals))
	}
	if goals[0].Status != model.LeadGoalComplete {
		t.Errorf("goal status = %q, want %q", goals[0].Status, model.LeadGoalComplete)
	}

	// Verify events were emitted
	var eventCount int
	err = conn.QueryRow("SELECT COUNT(*) FROM events WHERE lead_id = ?", "lead-1").Scan(&eventCount)
	if err != nil {
		t.Fatalf("counting events: %v", err)
	}
	if eventCount < 2 {
		t.Errorf("expected at least 2 events (started + complete), got %d", eventCount)
	}
}

func TestRunnerWithFailingMockClaude(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a mock claude that always fails verdict
	mockClaudePath := filepath.Join(tmpDir, "claude")
	mockScript := `#!/usr/bin/env bash
for arg in "$@"; do
    if echo "$arg" | grep -q "verdict"; then
        echo '{"pass":false,"summary":"implementation incomplete","issues":["missing tests"]}' > .lead/verdict.json
        break
    fi
done
echo "Mock claude output"
`
	if err := os.WriteFile(mockClaudePath, []byte(mockScript), 0755); err != nil {
		t.Fatalf("writing mock claude: %v", err)
	}

	// Set up database
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := database.Migrate(); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	conn := database.Conn()
	_, _ = conn.Exec("INSERT INTO instances (id, name, path) VALUES (?, ?, ?)", "test-inst", "test-inst", "/tmp/test")
	_, _ = conn.Exec("INSERT INTO tasks (id, instance_id, description) VALUES (?, ?, ?)", "task-1", "test-inst", "test task")
	_, _ = conn.Exec("INSERT INTO task_repos (id, task_id, repo_name) VALUES (?, ?, ?)", "tr-1", "task-1", "test-repo")
	_, _ = conn.Exec("INSERT INTO leads (id, task_repo_id, status, attempt) VALUES (?, ?, ?, ?)", "lead-1", "tr-1", "pending", 1)

	store := NewStore(conn)
	runner := NewRunner(store)

	worktreePath := filepath.Join(tmpDir, "worktree")
	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatalf("creating worktree dir: %v", err)
	}

	cfg := RunConfig{
		WorktreePath: worktreePath,
		LeadID:       "lead-1",
		TaskID:       "task-1",
		Spec:         "# Test Spec\nImplement something.",
		Goals: []model.GoalSpec{
			{Index: 0, Description: "Implement feature"},
		},
		MaxAttempts:  2,
		ExecuteModel: "claude-sonnet-4-6",
		ReviewModel:  "claude-sonnet-4-6",
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	ctx := context.Background()
	result, err := runner.Run(ctx, cfg)
	// Run should not return an error for stuck status (it's a valid outcome)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Status != model.LeadStatusStuck {
		t.Errorf("expected status %q, got %q (output: %s)", model.LeadStatusStuck, result.Status, result.Output)
	}

	// Verify lead was marked stuck in DB
	lead, err := store.GetLead("lead-1")
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	if lead.Status != model.LeadStatusStuck {
		t.Errorf("DB lead status = %q, want %q", lead.Status, model.LeadStatusStuck)
	}

	// Verify goal was marked stuck
	goals, err := store.GetLeadGoals("lead-1")
	if err != nil {
		t.Fatalf("GetLeadGoals: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("expected 1 goal, got %d", len(goals))
	}
	if goals[0].Status != model.LeadGoalStuck {
		t.Errorf("goal status = %q, want %q", goals[0].Status, model.LeadGoalStuck)
	}
}

func TestDefaultRunConfig(t *testing.T) {
	goals := []model.GoalSpec{{Index: 0, Description: "test"}}
	cfg := DefaultRunConfig("/path/to/worktree", "lead-1", "task-1", "spec", goals)

	if cfg.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
	if cfg.ExecuteModel != "claude-sonnet-4-6" {
		t.Errorf("ExecuteModel = %q, want %q", cfg.ExecuteModel, "claude-sonnet-4-6")
	}
	if cfg.ReviewModel != "claude-sonnet-4-6" {
		t.Errorf("ReviewModel = %q, want %q", cfg.ReviewModel, "claude-sonnet-4-6")
	}
	if len(cfg.Goals) != 1 {
		t.Errorf("Goals count = %d, want 1", len(cfg.Goals))
	}
}

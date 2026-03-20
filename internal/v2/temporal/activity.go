package temporal

import (
	"context"
	"encoding/json"

	"github.com/donovan-yohan/belayer/internal/v2/model"
	"github.com/donovan-yohan/belayer/internal/v2/role"
)

// TypeAInput is the input for a Type A (pitch) activity.
type TypeAInput struct {
	Role     role.RoleDef    `json:"role"`
	Input    json.RawMessage `json:"input"`
	TaskID   string          `json:"task_id"`
}

// TypeAOutput is the output of a Type A (pitch) activity.
type TypeAOutput struct {
	Output json.RawMessage `json:"output"`
	Status string          `json:"status"` // "completed" or "failed"
}

// TypeBSpawnInput is the input for spawning a Type B (ascent) interactive session.
type TypeBSpawnInput struct {
	Role        role.RoleDef    `json:"role"`
	Input       json.RawMessage `json:"input"`
	TaskID      string          `json:"task_id"`
	WorkDir     string          `json:"work_dir"`
}

// TypeBSpawnOutput is returned after a Type B session is spawned.
// The workflow then waits for a CLI callback signal.
type TypeBSpawnOutput struct {
	SessionID string `json:"session_id"`
	Spawned   bool   `json:"spawned"`
}

// Activities holds the activity implementations registered with the Temporal worker.
type Activities struct {
	// SessionSpawner launches interactive sessions for Type B roles.
	SessionSpawner SessionSpawner
	// ExecProvider runs Type A roles by shelling out.
	ExecProvider ExecProvider
	// WorkDir is the default working directory for sessions.
	WorkDir string
}

// SessionSpawner is the interface for launching interactive sessions (Type B).
type SessionSpawner interface {
	Spawn(ctx context.Context, roleName, taskID, workDir string, input json.RawMessage) (string, error)
}

// ExecProvider is the interface for running Type A roles via exec.
type ExecProvider interface {
	Execute(ctx context.Context, roleDef role.RoleDef, input json.RawMessage) (json.RawMessage, error)
}

// TypeAPitchActivity runs a Type A role synchronously via the exec provider.
func (a *Activities) TypeAPitchActivity(ctx context.Context, input TypeAInput) (*TypeAOutput, error) {
	output, err := a.ExecProvider.Execute(ctx, input.Role, input.Input)
	if err != nil {
		return &TypeAOutput{Status: "failed"}, err
	}
	return &TypeAOutput{Output: output, Status: "completed"}, nil
}

// TypeBSpawnActivity spawns an interactive session for a Type B role.
// It does NOT wait for completion — the Route workflow waits for a Signal.
func (a *Activities) TypeBSpawnActivity(ctx context.Context, input TypeBSpawnInput) (*TypeBSpawnOutput, error) {
	workDir := input.WorkDir
	if workDir == "" {
		workDir = a.WorkDir
	}
	sessionID, err := a.SessionSpawner.Spawn(ctx, input.Role.Name, input.TaskID, workDir, input.Input)
	if err != nil {
		return &TypeBSpawnOutput{Spawned: false}, err
	}
	return &TypeBSpawnOutput{SessionID: sessionID, Spawned: true}, nil
}

// HandleRoleSignal processes a CLI callback signal and returns the role result.
func HandleRoleSignal(signal model.RoleSignal) model.RoleResult {
	switch signal.Action {
	case model.SignalFinish:
		return model.RoleResult{
			Role:    signal.Role,
			Status:  "completed",
			Output:  signal.Output,
			Message: signal.Message,
		}
	case model.SignalFlare:
		return model.RoleResult{
			Role:    signal.Role,
			Status:  "flared",
			Message: signal.Message,
		}
	case model.SignalFail:
		return model.RoleResult{
			Role:    signal.Role,
			Status:  "failed",
			Message: signal.Message,
		}
	default:
		return model.RoleResult{
			Role:    signal.Role,
			Status:  "failed",
			Message: "unknown signal action: " + string(signal.Action),
		}
	}
}

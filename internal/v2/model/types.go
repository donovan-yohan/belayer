// Package model defines the v2 domain types for belayer's Temporal-backed orchestrator.
// Climbing metaphors: Route (pipeline), Pitch (role execution), Ascent (full run),
// Section (phase), Protection (risk gate), Fall (retry/loop).
package model

import "encoding/json"

// RunStatus tracks the state of a pipeline run.
// Temporal owns execution state — this is belayer's view.
type RunStatus string

const (
	RunStatusActive    RunStatus = "active"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusFlared    RunStatus = "flared" // Needs human help
)

// SignalAction is the action sent via `belayer <role> finish/flare/fail`.
type SignalAction string

const (
	SignalFinish SignalAction = "finish"
	SignalFlare  SignalAction = "flare"
	SignalFail   SignalAction = "fail"
)

// RoleSignal is the payload sent via CLI callback (`belayer <role> finish --task-id <id>`).
// This becomes a Temporal Signal delivered to the Route workflow.
type RoleSignal struct {
	TaskID  string          `json:"task_id"`
	Role    string          `json:"role"`
	Repo    string          `json:"repo,omitempty"` // For multi-repo: which repo this signal is for
	Action  SignalAction    `json:"action"`
	Output  json.RawMessage `json:"output,omitempty"`
	Message string          `json:"message,omitempty"`
}

// RouteInput is the input to the Route workflow (a pipeline run / ascent).
type RouteInput struct {
	Description  string          `json:"description"`
	PipelineFile string          `json:"pipeline_file,omitempty"`
	RouteJSON    json.RawMessage `json:"route_json,omitempty"` // Serialized pipeline.Route — if empty, uses default
	FromRole     string          `json:"from_role,omitempty"`
	ToRole       string          `json:"to_role,omitempty"`
	InputJSON    json.RawMessage `json:"input_json,omitempty"`
}

// RouteOutput is the output of a completed Route workflow.
type RouteOutput struct {
	Status      RunStatus                  `json:"status"`
	RoleOutputs map[string]json.RawMessage `json:"role_outputs"`
	Message     string                     `json:"message,omitempty"`
}

// RoleResult captures the outcome of a single role execution.
type RoleResult struct {
	Role    string          `json:"role"`
	Status  string          `json:"status"` // "completed", "failed", "flared"
	Output  json.RawMessage `json:"output,omitempty"`
	Message string          `json:"message,omitempty"`
}

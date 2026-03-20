package temporal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PipelineEvent is a lifecycle event pushed from the Temporal workflow
// to a session's channel server via HTTP POST.
type PipelineEvent struct {
	Event   string            `json:"event"`
	Content string            `json:"content"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// PushEventInput is the input for the PushEventActivity.
type PushEventInput struct {
	Port  int           `json:"port"`  // Target channel server HTTP port
	Event PipelineEvent `json:"event"` // The event to push
}

// PushEventActivity sends a pipeline event to a session's channel server via HTTP POST.
// This is a best-effort operation — if the channel server is down, the event is lost
// but the workflow continues. Pipeline state is in Temporal, not in channel events.
func (a *Activities) PushEventActivity(ctx context.Context, input PushEventInput) error {
	if input.Port == 0 {
		return nil // No channel configured — skip silently.
	}

	body, err := json.Marshal(input.Event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d", input.Port)
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		// Best effort — log and continue. The pipeline doesn't depend on events.
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		// Best effort — don't fail the workflow because an event couldn't be pushed.
		return nil
	}

	return nil
}

// Common event constructors.

func PipelineStartedEvent(pipelineName string, repos []string) PipelineEvent {
	return PipelineEvent{
		Event:   "pipeline_started",
		Content: fmt.Sprintf("Pipeline '%s' started with %d repo(s)", pipelineName, len(repos)),
		Meta: map[string]string{
			"event":    "pipeline_started",
			"pipeline": pipelineName,
		},
	}
}

func PhaseStartedEvent(phase string) PipelineEvent {
	return PipelineEvent{
		Event:   "phase_started",
		Content: fmt.Sprintf("Entering %s phase", phase),
		Meta: map[string]string{
			"event": "phase_started",
			"phase": phase,
		},
	}
}

func RoleCompletedEvent(roleName, repoName, summary string) PipelineEvent {
	meta := map[string]string{
		"event": "role_completed",
		"role":  roleName,
	}
	if repoName != "" {
		meta["repo"] = repoName
	}
	content := fmt.Sprintf("%s completed", roleName)
	if repoName != "" {
		content = fmt.Sprintf("%s (%s) completed", roleName, repoName)
	}
	if summary != "" {
		content += ": " + summary
	}
	return PipelineEvent{
		Event:   "role_completed",
		Content: content,
		Meta:    meta,
	}
}

func DependencyReadyEvent(repoName, dependsOn string) PipelineEvent {
	return PipelineEvent{
		Event:   "dependency_ready",
		Content: fmt.Sprintf("%s is now ready — %s has completed", repoName, dependsOn),
		Meta: map[string]string{
			"event":      "dependency_ready",
			"repo":       repoName,
			"depends_on": dependsOn,
		},
	}
}

func FlareEvent(roleName, repoName, message string) PipelineEvent {
	meta := map[string]string{
		"event": "flare",
		"role":  roleName,
	}
	if repoName != "" {
		meta["repo"] = repoName
	}
	content := fmt.Sprintf("FLARE from %s", roleName)
	if repoName != "" {
		content = fmt.Sprintf("FLARE from %s (%s)", roleName, repoName)
	}
	if message != "" {
		content += ": " + message
	}
	return PipelineEvent{
		Event:   "flare",
		Content: content,
		Meta:    meta,
	}
}

func PipelineCompletedEvent(status string) PipelineEvent {
	return PipelineEvent{
		Event:   "pipeline_completed",
		Content: fmt.Sprintf("Pipeline %s", status),
		Meta: map[string]string{
			"event":  "pipeline_completed",
			"status": status,
		},
	}
}

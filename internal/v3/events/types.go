package events

import "time"

type Event struct {
	Timestamp  time.Time `json:"ts"`
	Type       string    `json:"event"`
	Node       string    `json:"node,omitempty"`
	Outcome    string    `json:"outcome,omitempty"`
	Target     string    `json:"target,omitempty"`
	Attempt    int       `json:"attempt,omitempty"`
	DurationS  float64   `json:"duration_s,omitempty"`
	WorkflowID string    `json:"workflow_id,omitempty"`
	Pipeline   string    `json:"pipeline,omitempty"`
	Input      string    `json:"input,omitempty"`
	Feedback   string    `json:"feedback,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	Message    string    `json:"message,omitempty"`
}

func PipelineStarted(workflowID, pipeline, input string) Event {
	return Event{Timestamp: time.Now(), Type: "pipeline_started", WorkflowID: workflowID, Pipeline: pipeline, Input: input}
}

func NodeStarted(node string, attempt int) Event {
	return Event{Timestamp: time.Now(), Type: "node_started", Node: node, Attempt: attempt}
}

func NodeCompleted(node, outcome string, durationS float64) Event {
	return Event{Timestamp: time.Now(), Type: "node_completed", Node: node, Outcome: outcome, DurationS: durationS}
}

func NodeRetry(node, target, feedback string) Event {
	return Event{Timestamp: time.Now(), Type: "node_retry", Node: node, Target: target, Feedback: feedback}
}

func PipelineCompleted(outcome string, durationS float64) Event {
	return Event{Timestamp: time.Now(), Type: "pipeline_completed", Outcome: outcome, DurationS: durationS}
}

func PipelineFailed(node, reason string) Event {
	return Event{Timestamp: time.Now(), Type: "pipeline_failed", Node: node, Reason: reason}
}

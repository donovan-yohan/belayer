package events

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewLogger_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "subdir", "events.jsonl")

	l, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer l.Close()

	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("parent dir not created: %v", err)
	}
}

func TestLogger_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	l, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	evts := []Event{
		PipelineStarted("wf-1", "my-pipeline", "some input"),
		NodeStarted("planner", 1),
		NodeCompleted("planner", "pass", 1.5),
		NodeRetry("planner", "executor", "needs more detail"),
		PipelineCompleted("pass", 3.0),
		PipelineFailed("executor", "timed out"),
	}

	for _, e := range evts {
		if err := l.Log(e); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	var read []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		read = append(read, e)
	}
	if sc.Err() != nil {
		t.Fatalf("scan: %v", sc.Err())
	}

	if len(read) != len(evts) {
		t.Fatalf("expected %d events, got %d", len(evts), len(read))
	}

	checks := []struct {
		idx     int
		evtType string
		field   string
		got     func(Event) string
		want    string
	}{
		{0, "pipeline_started", "workflow_id", func(e Event) string { return e.WorkflowID }, "wf-1"},
		{0, "pipeline_started", "pipeline", func(e Event) string { return e.Pipeline }, "my-pipeline"},
		{0, "pipeline_started", "input", func(e Event) string { return e.Input }, "some input"},
		{1, "node_started", "node", func(e Event) string { return e.Node }, "planner"},
		{2, "node_completed", "outcome", func(e Event) string { return e.Outcome }, "pass"},
		{3, "node_retry", "target", func(e Event) string { return e.Target }, "executor"},
		{3, "node_retry", "feedback", func(e Event) string { return e.Feedback }, "needs more detail"},
		{4, "pipeline_completed", "outcome", func(e Event) string { return e.Outcome }, "pass"},
		{5, "pipeline_failed", "reason", func(e Event) string { return e.Reason }, "timed out"},
	}

	for _, c := range checks {
		if read[c.idx].Type != c.evtType {
			t.Errorf("event[%d].Type = %q, want %q", c.idx, read[c.idx].Type, c.evtType)
		}
		if got := c.got(read[c.idx]); got != c.want {
			t.Errorf("event[%d].%s = %q, want %q", c.idx, c.field, got, c.want)
		}
	}
}

func TestLogger_AppendToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	l1, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger first: %v", err)
	}
	if err := l1.Log(NodeStarted("a", 1)); err != nil {
		t.Fatalf("Log first: %v", err)
	}
	l1.Close()

	l2, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger second: %v", err)
	}
	if err := l2.Log(NodeStarted("b", 1)); err != nil {
		t.Fatalf("Log second: %v", err)
	}
	l2.Close()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	var count int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		count++
	}
	if count != 2 {
		t.Errorf("expected 2 lines after append, got %d", count)
	}
}

func TestGateEvents(t *testing.T) {
	evt := GateStarted("review", 1)
	if evt.Type != "gate_started" {
		t.Errorf("Type: got %q, want %q", evt.Type, "gate_started")
	}
	if evt.Node != "review" {
		t.Errorf("Node: got %q, want %q", evt.Node, "review")
	}

	scores := map[string]float64{"correctness": 8.0, "quality": 7.0}
	scored := GateScored("review", 1, scores, 7.5)
	if scored.Type != "gate_scored" {
		t.Errorf("Type: got %q, want %q", scored.Type, "gate_scored")
	}
	if scored.WeightedScore != 7.5 {
		t.Errorf("WeightedScore: got %f, want 7.5", scored.WeightedScore)
	}
	if scored.DimensionScores["correctness"] != 8.0 {
		t.Errorf("correctness score: got %f, want 8.0", scored.DimensionScores["correctness"])
	}

	completed := GateCompleted("review", 1, "PASS", 7.5)
	if completed.Type != "gate_completed" {
		t.Errorf("Type: got %q, want %q", completed.Type, "gate_completed")
	}
	if completed.WeightedScore != 7.5 {
		t.Errorf("WeightedScore: got %f, want 7.5", completed.WeightedScore)
	}
}

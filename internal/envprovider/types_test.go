package envprovider

import (
	"encoding/json"
	"testing"
)

func roundTrip[T any](t *testing.T, v T) T {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestCreateEnvResponse_WithWorktrees(t *testing.T) {
	in := CreateEnvResponse{
		Status: "ok",
		Name:   "dev-1",
		Index:  1,
		Env:    map[string]string{"FOO": "bar"},
		Services: map[string]ServiceStatus{
			"api": {Status: "running", Port: 8080, Uptime: "10m"},
		},
		Worktrees: []WorktreeInfo{
			{Repo: "myrepo", Branch: "main", Path: "/tmp/wt", EnvFile: ".env"},
		},
	}
	out := roundTrip(t, in)

	if out.Status != in.Status || out.Name != in.Name || out.Index != in.Index {
		t.Errorf("basic fields mismatch: got %+v", out)
	}
	if out.Env["FOO"] != "bar" {
		t.Errorf("env mismatch: got %v", out.Env)
	}
	if svc, ok := out.Services["api"]; !ok || svc.Port != 8080 {
		t.Errorf("services mismatch: got %v", out.Services)
	}
	if len(out.Worktrees) != 1 || out.Worktrees[0].Repo != "myrepo" {
		t.Errorf("worktrees mismatch: got %v", out.Worktrees)
	}
}

func TestCreateEnvResponse_WithoutWorktrees(t *testing.T) {
	in := CreateEnvResponse{
		Status: "ok",
		Name:   "dev-2",
		Index:  2,
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// omitempty fields should not appear in JSON
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, key := range []string{"env", "services", "worktrees"} {
		if _, exists := raw[key]; exists {
			t.Errorf("expected %q to be omitted but found it in JSON", key)
		}
	}

	out := roundTrip(t, in)
	if out.Status != "ok" || out.Name != "dev-2" || out.Index != 2 {
		t.Errorf("mismatch: got %+v", out)
	}
}

func TestAddWorktreeResponse(t *testing.T) {
	in := AddWorktreeResponse{
		Status: "ok",
		Repo:   "myrepo",
		Branch: "feature-x",
		Path:   "/tmp/wt2",
	}
	out := roundTrip(t, in)
	if out.Status != in.Status || out.Repo != in.Repo || out.Branch != in.Branch || out.Path != in.Path {
		t.Errorf("mismatch: got %+v", out)
	}
	if out.EnvFile != "" {
		t.Errorf("expected env_file to be empty, got %q", out.EnvFile)
	}

	// With EnvFile set
	in2 := AddWorktreeResponse{Status: "ok", Repo: "r", Branch: "b", Path: "/p", EnvFile: ".env.local"}
	out2 := roundTrip(t, in2)
	if out2.EnvFile != ".env.local" {
		t.Errorf("env_file mismatch: got %q", out2.EnvFile)
	}
}

func TestStatusEnvResponse(t *testing.T) {
	in := StatusEnvResponse{
		Status: "ok",
		Name:   "dev-1",
		Index:  1,
		Services: map[string]ServiceStatus{
			"db": {Status: "running", Port: 5432, Uptime: "1h"},
		},
		Snapshot: &SnapshotInfo{
			Name:       "snap-1",
			RestoredAt: "2026-03-13T00:00:00Z",
			Stale:      false,
		},
		Worktrees: []WorktreeStatusInfo{
			{Repo: "repo1", Branch: "main", Path: "/wt/1", Dirty: true},
		},
	}
	out := roundTrip(t, in)
	if out.Status != in.Status || out.Name != in.Name {
		t.Errorf("basic fields mismatch: got %+v", out)
	}
	if out.Snapshot == nil || out.Snapshot.Name != "snap-1" {
		t.Errorf("snapshot mismatch: got %+v", out.Snapshot)
	}
	if len(out.Worktrees) != 1 || !out.Worktrees[0].Dirty {
		t.Errorf("worktrees mismatch: got %v", out.Worktrees)
	}

	// Without snapshot — should be omitted
	inNoSnap := StatusEnvResponse{Status: "ok", Name: "n", Index: 0, Services: map[string]ServiceStatus{}, Worktrees: []WorktreeStatusInfo{}}
	data, _ := json.Marshal(inNoSnap)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	if _, exists := raw["snapshot"]; exists {
		t.Errorf("expected snapshot to be omitted")
	}
}

func TestErrorResponse(t *testing.T) {
	in := ErrorResponse{
		Status: "error",
		Error:  "not found",
		Code:   "ENV_NOT_FOUND",
	}
	out := roundTrip(t, in)
	if out.Status != in.Status || out.Error != in.Error || out.Code != in.Code {
		t.Errorf("mismatch: got %+v", out)
	}
}

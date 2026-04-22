package daemon

import (
	"net/http"
	"strings"
	"testing"
)

func TestSpawnAgentRejectsWhenMaxConcurrentAgentsReached(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithWorkspace(t, d, "runtime:\n  max_concurrent_agents: 2\n")

	for _, name := range []string{"worker-1", "worker-2"} {
		rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
			Name:    name,
			Role:    "worker",
			Profile: "default",
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("spawn %s: expected 201, got %d: %s", name, rr.Code, rr.Body.String())
		}
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "worker-3",
		Role:    "worker",
		Profile: "default",
	})
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 when cap is reached, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "max_concurrent_agents") {
		t.Fatalf("expected cap error payload, got: %s", body)
	}
	if !strings.Contains(body, "retire one before spawning") {
		t.Fatalf("expected actionable cap message, got: %s", body)
	}
}

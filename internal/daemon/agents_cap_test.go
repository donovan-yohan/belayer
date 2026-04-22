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

func TestSpawnAgentRejectsWhenMaxConcurrentMainsReached(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithWorkspace(t, d, "runtime:\n  max_concurrent_mains: 1\n  max_concurrent_sides: 3\n")

	rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "main-1",
		Role:    "worker",
		Kind:    "main",
		Profile: "default",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn main-1: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "main-2",
		Role:    "worker",
		Kind:    "main",
		Profile: "default",
	})
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 when main cap is reached, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "max_concurrent_mains") {
		t.Fatalf("expected main-cap error payload, got: %s", body)
	}
}

func TestSpawnSideRejectsWhenMaxConcurrentSidesReached(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithWorkspace(t, d, "runtime:\n  max_concurrent_mains: 1\n  max_concurrent_sides: 1\n")

	rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "main-1",
		Role:    "worker",
		Kind:    "main",
		Profile: "default",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn main-1: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "side-1",
		Role:    "worker",
		Kind:    "side",
		Profile: "default",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn side-1: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "side-2",
		Role:    "worker",
		Kind:    "side",
		Profile: "default",
	})
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 when side cap is reached, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "max_concurrent_sides") {
		t.Fatalf("expected side-cap error payload, got: %s", body)
	}
	if !strings.Contains(body, "retire one before summoning") {
		t.Fatalf("expected side-cap guidance, got: %s", body)
	}
}

func TestSpawnSideRejectsWhenSideSummonBudgetReached(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithWorkspace(t, d, "runtime:\n  max_concurrent_sides: 5\n  max_side_summons_per_session: 2\n")

	for _, name := range []string{"side-1", "side-2"} {
		rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
			Name:    name,
			Role:    "worker",
			Kind:    "side",
			Profile: "default",
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("spawn %s: expected 201, got %d: %s", name, rr.Code, rr.Body.String())
		}
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "side-3",
		Role:    "worker",
		Kind:    "side",
		Profile: "default",
	})
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 when side summon budget is reached, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "max_side_summons_per_session") {
		t.Fatalf("expected summon-budget error payload, got: %s", body)
	}
	if !strings.Contains(body, "do not summon more sides") {
		t.Fatalf("expected summon-budget guidance, got: %s", body)
	}
}

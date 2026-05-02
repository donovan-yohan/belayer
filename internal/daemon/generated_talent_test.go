package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

func TestScaffoldGeneratedTalentEndpointCreatesRunnableIdentity(t *testing.T) {
	d := testDaemon(t)
	workspace := t.TempDir()
	sessionID, err := d.store.CreateSession(store.Session{Name: "story-smoke", WorkspaceDir: workspace})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/generated-talents/scaffold", generatedTalentScaffoldRequest{
		Crag:          "last-lantern",
		ID:            "mara-underbough",
		Domain:        "story",
		Role:          "tavernkeep",
		Lifecycle:     "resumable",
		SourceRequest: "turn-0002",
		Reason:        "scene needs a reusable local authority",
		Metadata: map[string]string{
			"voice": "warm and watchful",
		},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("scaffold generated talent: got %d, body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeJSON[map[string]string](t, rr)
	if resp["identity"] != "mara-underbough" {
		t.Fatalf("identity = %q, want mara-underbough", resp["identity"])
	}
	if resp["path"] != filepath.ToSlash(filepath.Join(workspace, ".belayer", "agents", "mara-underbough")) {
		t.Fatalf("path = %q, want slash-normalized identity path", resp["path"])
	}

	identityDir := filepath.Join(workspace, ".belayer", "agents", "mara-underbough")
	for _, rel := range []string{"agent.yaml", "system-prompt.md", "agents.md", "talent.yaml"} {
		if _, err := os.Stat(filepath.Join(identityDir, rel)); err != nil {
			t.Fatalf("expected scaffolded %s: %v", rel, err)
		}
	}
	systemPrompt, err := os.ReadFile(filepath.Join(identityDir, "system-prompt.md"))
	if err != nil {
		t.Fatalf("read system prompt: %v", err)
	}
	for _, want := range []string{"domain: story", "role: tavernkeep", "source request: turn-0002"} {
		if !strings.Contains(string(systemPrompt), want) {
			t.Fatalf("system prompt missing %q:\n%s", want, string(systemPrompt))
		}
	}
}

func TestScaffoldGeneratedTalentEndpointIgnoresPersistenceOnlyFields(t *testing.T) {
	d := testDaemon(t)
	workspace := t.TempDir()
	sessionID, err := d.store.CreateSession(store.Session{Name: "story-smoke", WorkspaceDir: workspace})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/generated-talents/scaffold", map[string]any{
		"crag":               "last-lantern",
		"id":                 "mara-underbough",
		"domain":             "story",
		"role":               "tavernkeep",
		"lifecycle":          "resumable",
		"status":             "promoted",
		"source_request":     "turn-0002",
		"reason":             "scene needs a reusable local authority",
		"promotion_evidence": []string{"artifacts/talent-evaluation-mara.json"},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("scaffold generated talent: got %d, body=%s", rr.Code, rr.Body.String())
	}

	record, err := os.ReadFile(filepath.Join(workspace, ".belayer", "agents", "mara-underbough", "talent.yaml"))
	if err != nil {
		t.Fatalf("read talent.yaml: %v", err)
	}
	if strings.Contains(string(record), "promoted") || strings.Contains(string(record), "promotion_evidence") {
		t.Fatalf("daemon scaffold should ignore persistence-only fields:\n%s", string(record))
	}
	if !strings.Contains(string(record), "status: generated") {
		t.Fatalf("daemon scaffold should retain generated default status:\n%s", string(record))
	}
}

func TestScaffoldGeneratedTalentEndpointRequiresWorkspace(t *testing.T) {
	d := testDaemon(t)
	sessionID, err := d.store.CreateSession(store.Session{Name: "no-workspace"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/generated-talents/scaffold", generatedTalentScaffoldRequest{
		Crag:          "last-lantern",
		ID:            "mara-underbough",
		Domain:        "story",
		Role:          "tavernkeep",
		Lifecycle:     "resumable",
		SourceRequest: "turn-0002",
		Reason:        "scene needs a reusable local authority",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing workspace, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestScaffoldGeneratedTalentEndpointRejectsTraversalID(t *testing.T) {
	d := testDaemon(t)
	sessionID, err := d.store.CreateSession(store.Session{Name: "story-smoke", WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/generated-talents/scaffold", generatedTalentScaffoldRequest{
		Crag:          "last-lantern",
		ID:            "../escape",
		Domain:        "story",
		Role:          "tavernkeep",
		Lifecycle:     "resumable",
		SourceRequest: "turn-0002",
		Reason:        "scene needs a reusable local authority",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal id, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestScaffoldGeneratedTalentEndpointReturnsInternalErrorForFilesystemFailure(t *testing.T) {
	d := testDaemon(t)
	workspaceFile := filepath.Join(t.TempDir(), "workspace-file")
	if err := os.WriteFile(workspaceFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	sessionID, err := d.store.CreateSession(store.Session{Name: "story-smoke", WorkspaceDir: workspaceFile})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/generated-talents/scaffold", generatedTalentScaffoldRequest{
		Crag:          "last-lantern",
		ID:            "mara-underbough",
		Domain:        "story",
		Role:          "tavernkeep",
		Lifecycle:     "resumable",
		SourceRequest: "turn-0002",
		Reason:        "scene needs a reusable local authority",
	})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for filesystem failure, got %d body=%s", rr.Code, rr.Body.String())
	}
}

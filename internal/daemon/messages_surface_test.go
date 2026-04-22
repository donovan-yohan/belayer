package daemon

import (
	"net/http"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

func createAgentRunForSurfaceTest(t *testing.T, d *Daemon, sessionID, name, role, kind string) {
	t.Helper()
	if _, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessionID,
		Name:      name,
		Role:      role,
		Kind:      kind,
		Profile:   "default",
		Status:    "running",
		Transport: "bridge",
	}); err != nil {
		t.Fatalf("CreateAgentRun %s: %v", name, err)
	}
}

func TestSendMessageEnforcesMainAndSideSurfaces(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "mail-surface"}))
	createAgentRunForSurfaceTest(t, d, created.ID, "supervisor", "supervisor", "main")
	createAgentRunForSurfaceTest(t, d, created.ID, "pm", "pm", "side")

	rr := doRequest(t, d, "POST", "/sessions/"+created.ID+"/messages", sendMessageRequest{
		From:      "pm",
		To:        "supervisor",
		Content:   "side cannot send",
		Type:      "instruction",
		Interrupt: false,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected side sender to be rejected, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "side agents have no outbox") {
		t.Fatalf("expected outbox rejection, got: %s", rr.Body.String())
	}

	rr = doRequest(t, d, "POST", "/sessions/"+created.ID+"/messages", sendMessageRequest{
		From:      "supervisor",
		To:        "pm",
		Content:   "side cannot receive passive mail",
		Type:      "instruction",
		Interrupt: false,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected passive message to side to be rejected, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "sides have no inbox") {
		t.Fatalf("expected inbox rejection, got: %s", rr.Body.String())
	}

	rr = doRequest(t, d, "POST", "/sessions/"+created.ID+"/messages", sendMessageRequest{
		From:      "supervisor",
		To:        "pm",
		Content:   "urgent side interrupt",
		Type:      "instruction",
		Interrupt: true,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected interrupt delivery to side to be allowed, got %d: %s", rr.Code, rr.Body.String())
	}

	msgsRR := doRequest(t, d, "GET", "/sessions/"+created.ID+"/messages?for=pm", nil)
	if msgsRR.Code != http.StatusBadRequest {
		t.Fatalf("expected side inbox listing to be rejected, got %d: %s", msgsRR.Code, msgsRR.Body.String())
	}
}

func TestBroadcastMessagePersistsMainRecipientsOnly(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "broadcast-surface"}))
	createAgentRunForSurfaceTest(t, d, created.ID, "supervisor", "supervisor", "main")
	createAgentRunForSurfaceTest(t, d, created.ID, "worker", "worker", "main")
	createAgentRunForSurfaceTest(t, d, created.ID, "pm", "pm", "side")

	rr := doRequest(t, d, "POST", "/sessions/"+created.ID+"/messages/broadcast", broadcastMessageRequest{
		From:    "supervisor",
		Content: "broadcast to main agents only",
		Type:    "instruction",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected broadcast to succeed, got %d: %s", rr.Code, rr.Body.String())
	}

	msgs, err := d.store.ListMessagesInSession(created.ID)
	if err != nil {
		t.Fatalf("ListMessagesInSession: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected one broadcast row for the non-sender main recipient, got %d: %#v", len(msgs), msgs)
	}
	if msgs[0].RecipientID != "worker" || msgs[0].SenderID != "supervisor" {
		t.Fatalf("expected broadcast to stay on the main surface, got %#v", msgs[0])
	}

	unackedRR := doRequest(t, d, "GET", "/sessions/"+created.ID+"/messages?state=unacked", nil)
	if unackedRR.Code != http.StatusOK {
		t.Fatalf("expected unacked listing to succeed, got %d: %s", unackedRR.Code, unackedRR.Body.String())
	}
	unacked := decodeJSON[[]store.Message](t, unackedRR)
	if len(unacked) != 1 || unacked[0].RecipientID != "worker" {
		t.Fatalf("expected unacked listing to include only the main recipient, got %#v", unacked)
	}
}

package broker

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/store"
)

const testWindow = 50 * time.Millisecond

// newTestBroker returns a MemoryBroker with a short debounce window for tests.
func newTestBroker() *MemoryBroker {
	return newMemoryBrokerWithWindow(nil, testWindow)
}

// collector accumulates delivered messages in a thread-safe slice.
type collector struct {
	mu   sync.Mutex
	msgs []Message
}

func (c *collector) handler() Handler {
	return func(msg Message) {
		c.mu.Lock()
		c.msgs = append(c.msgs, msg)
		c.mu.Unlock()
	}
}

func (c *collector) all() []Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Message, len(c.msgs))
	copy(out, c.msgs)
	return out
}

func (c *collector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.msgs)
}

// waitFor polls until cond returns true or timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// --- Tests ---

func TestSendDeliversToSubscriber(t *testing.T) {
	b := newTestBroker()
	col := &collector{}

	if err := b.Subscribe("sess1", "agent1", col.handler()); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	msg := Message{
		SessionID: "sess1",
		SenderID:  "sender",
		Type:      MessageInstruction,
		Content:   "hello",
	}
	if err := b.Send("sess1", "agent1", msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Wait for debounce to flush.
	waitFor(t, 200*time.Millisecond, func() bool { return col.count() >= 1 })

	msgs := col.all()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("expected content %q, got %q", "hello", msgs[0].Content)
	}
}

func TestSendToUnsubscribedAgentReturnsError(t *testing.T) {
	b := newTestBroker()

	msg := Message{
		SessionID: "sess1",
		SenderID:  "sender",
		Type:      MessageInstruction,
		Content:   "hello",
	}
	err := b.Send("sess1", "nobody", msg)
	if err == nil {
		t.Fatal("expected error sending to unsubscribed agent, got nil")
	}
}

func TestBroadcastDeliversToAllExceptSender(t *testing.T) {
	b := newTestBroker()

	col1 := &collector{}
	col2 := &collector{}
	col3 := &collector{}

	_ = b.Subscribe("sess1", "agent1", col1.handler())
	_ = b.Subscribe("sess1", "agent2", col2.handler())
	_ = b.Subscribe("sess1", "agent3", col3.handler())

	msg := Message{
		SessionID: "sess1",
		SenderID:  "agent1", // sender should not receive its own broadcast
		Type:      MessageStateChange,
		Content:   "state-update",
	}
	if err := b.Broadcast("sess1", msg); err != nil {
		t.Fatalf("Broadcast: %v", err)
	}

	// Broadcast is not debounced — give a small window for goroutine scheduling.
	time.Sleep(20 * time.Millisecond)

	if col1.count() != 0 {
		t.Errorf("sender (agent1) should not receive its own broadcast, got %d messages", col1.count())
	}
	if col2.count() != 1 {
		t.Errorf("agent2 expected 1 message, got %d", col2.count())
	}
	if col3.count() != 1 {
		t.Errorf("agent3 expected 1 message, got %d", col3.count())
	}
}

func TestSubscribeUnsubscribeLifecycle(t *testing.T) {
	b := newTestBroker()
	col := &collector{}

	// Subscribe and confirm delivery.
	_ = b.Subscribe("sess1", "agent1", col.handler())
	msg := Message{SessionID: "sess1", SenderID: "s", Type: MessageInstruction, Content: "first", Urgent: true}
	_ = b.Send("sess1", "agent1", msg)
	time.Sleep(20 * time.Millisecond)

	if col.count() != 1 {
		t.Fatalf("expected 1 message after subscribe, got %d", col.count())
	}

	// Unsubscribe.
	if err := b.Unsubscribe("sess1", "agent1"); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	// Sending after unsubscribe should return error.
	err := b.Send("sess1", "agent1", msg)
	if err == nil {
		t.Fatal("expected error after unsubscribe, got nil")
	}

	// Count must remain 1.
	if col.count() != 1 {
		t.Errorf("no new messages expected after unsubscribe, got %d", col.count())
	}
}

func TestMessageCoalescingRapidSends(t *testing.T) {
	b := newTestBroker()
	col := &collector{}

	_ = b.Subscribe("sess1", "agent1", col.handler())

	msg1 := Message{SessionID: "sess1", SenderID: "s", Type: MessageInstruction, Content: "first"}
	msg2 := Message{SessionID: "sess1", SenderID: "s", Type: MessageInstruction, Content: "second"}

	_ = b.Send("sess1", "agent1", msg1)
	_ = b.Send("sess1", "agent1", msg2)

	// Wait longer than the debounce window.
	waitFor(t, 300*time.Millisecond, func() bool { return col.count() >= 1 })

	msgs := col.all()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 coalesced message, got %d", len(msgs))
	}
	expected := "first\n\nsecond"
	if msgs[0].Content != expected {
		t.Errorf("expected coalesced content %q, got %q", expected, msgs[0].Content)
	}
}

func TestUrgentMessageBypassesDebounce(t *testing.T) {
	b := newTestBroker()
	col := &collector{}

	_ = b.Subscribe("sess1", "agent1", col.handler())

	// Send a non-urgent message first (sits in debounce buffer).
	nonUrgent := Message{SessionID: "sess1", SenderID: "s", Type: MessageInstruction, Content: "buffered"}
	_ = b.Send("sess1", "agent1", nonUrgent)

	// Immediately send an urgent message — should arrive right away.
	urgent := Message{SessionID: "sess1", SenderID: "s", Type: MessageInstruction, Content: "urgent!", Urgent: true}
	_ = b.Send("sess1", "agent1", urgent)

	// The urgent message (and the flushed buffer) should arrive before the debounce window expires.
	waitFor(t, 30*time.Millisecond, func() bool { return col.count() >= 2 })

	msgs := col.all()
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 deliveries (flushed buffer + urgent), got %d", len(msgs))
	}
	// Last delivery should be the urgent message.
	last := msgs[len(msgs)-1]
	if last.Content != "urgent!" {
		t.Errorf("expected last message to be urgent, got %q", last.Content)
	}
}

func TestInterruptDeliversMessageDirectly(t *testing.T) {
	b := newTestBroker()
	col := &collector{}

	_ = b.Subscribe("sess1", "agent1", col.handler())

	msg := Message{
		SessionID: "sess1",
		SenderID:  "s",
		Type:      MessageInstruction,
		Content:   "interrupt message",
	}
	if err := b.Interrupt("sess1", "agent1", msg); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	msgs := col.all()
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 message from Interrupt, got %d", len(msgs))
	}
	if msgs[0].Content != "interrupt message" {
		t.Errorf("expected message content %q, got %q", "interrupt message", msgs[0].Content)
	}
}

func TestInterruptOnUnsubscribedAgentReturnsError(t *testing.T) {
	b := newTestBroker()
	msg := Message{SessionID: "sess1", SenderID: "s", Type: MessageInstruction, Content: "x"}
	err := b.Interrupt("sess1", "nobody", msg)
	if err == nil {
		t.Fatal("expected error interrupting unsubscribed agent, got nil")
	}
}

func TestMessageHistoryLoggedToStore(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	// Create a session so FK constraints pass (events table references session_id).
	sessionID := "sess-store-test"
	if _, err := st.CreateSession(store.Session{ID: sessionID, Name: "test"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	b := newMemoryBrokerWithWindow(st, testWindow)
	col := &collector{}
	_ = b.Subscribe(sessionID, "agent1", col.handler())

	msg := Message{
		SessionID: sessionID,
		SenderID:  "s",
		Type:      MessageInstruction,
		Content:   "stored",
		Urgent:    true, // bypass debounce so delivery is synchronous
	}
	if err := b.Send(sessionID, "agent1", msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Give a tiny moment for logEvent (synchronous in this path, but be safe).
	time.Sleep(10 * time.Millisecond)

	events, err := st.QueryEvents(sessionID)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Type == "message_delivered" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected message_delivered event in store, got %d events: %v", len(events), events)
	}
}

func TestSendRejectsEmptyContent(t *testing.T) {
	b := newTestBroker()
	col := &collector{}
	_ = b.Subscribe("sess1", "agent1", col.handler())

	cases := []struct {
		name    string
		content string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"tab only", "\t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := Message{SessionID: "sess1", SenderID: "sender", Type: MessageInstruction, Content: tc.content}
			err := b.Send("sess1", "agent1", msg)
			if err == nil {
				t.Fatalf("expected error for content %q, got nil", tc.content)
			}
		})
	}

	// Valid content must still succeed.
	msg := Message{SessionID: "sess1", SenderID: "sender", Type: MessageInstruction, Content: "hello", Urgent: true}
	if err := b.Send("sess1", "agent1", msg); err != nil {
		t.Fatalf("expected no error for valid content, got: %v", err)
	}
}

func TestBroadcastRejectsEmptyContent(t *testing.T) {
	b := newTestBroker()
	col := &collector{}
	_ = b.Subscribe("sess1", "agent1", col.handler())

	cases := []struct {
		name    string
		content string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := Message{SessionID: "sess1", SenderID: "sender2", Type: MessageStateChange, Content: tc.content}
			err := b.Broadcast("sess1", msg)
			if err == nil {
				t.Fatalf("expected error for content %q, got nil", tc.content)
			}
		})
	}

	// Valid content must still succeed.
	msg := Message{SessionID: "sess1", SenderID: "sender2", Type: MessageStateChange, Content: "state-update"}
	if err := b.Broadcast("sess1", msg); err != nil {
		t.Fatalf("expected no error for valid content, got: %v", err)
	}
}

func TestInterruptRejectsEmptyContent(t *testing.T) {
	b := newTestBroker()
	col := &collector{}
	_ = b.Subscribe("sess1", "agent1", col.handler())

	cases := []struct {
		name    string
		content string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := Message{SessionID: "sess1", SenderID: "sender", Type: MessageInstruction, Content: tc.content}
			err := b.Interrupt("sess1", "agent1", msg)
			if err == nil {
				t.Fatalf("expected error for content %q, got nil", tc.content)
			}
		})
	}

	// Valid content must still succeed.
	msg := Message{SessionID: "sess1", SenderID: "sender", Type: MessageInstruction, Content: "interrupt!"}
	if err := b.Interrupt("sess1", "agent1", msg); err != nil {
		t.Fatalf("expected no error for valid content, got: %v", err)
	}
}

func TestConcurrentSendIsSafe(t *testing.T) {
	b := newTestBroker()
	var delivered int64

	_ = b.Subscribe("sess1", "agent1", func(msg Message) {
		atomic.AddInt64(&delivered, 1)
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg := Message{
				SessionID: "sess1",
				SenderID:  "s",
				Type:      MessageInstruction,
				Content:   "concurrent",
				Urgent:    true,
			}
			_ = b.Send("sess1", "agent1", msg)
		}()
	}
	wg.Wait()

	// All 20 urgent messages bypass debounce, so all 20 should be delivered.
	time.Sleep(20 * time.Millisecond)
	if got := atomic.LoadInt64(&delivered); got != 20 {
		t.Errorf("expected 20 concurrent deliveries, got %d", got)
	}
}

package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
)

// Client connects to the belayer daemon via Unix socket.
type Client struct {
	http       *http.Client
	socketPath string
}

// DefaultSocketPath returns the default daemon socket path.
func DefaultSocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".belayer", "daemon.sock")
}

// NewClient creates a client that talks to the daemon at socketPath.
func NewClient(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
			Timeout: 10 * time.Second,
		},
	}
}

// do executes an HTTP request against the daemon.
func (c *Client) do(method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequest(method, "http://daemon"+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// Health checks daemon health.
func (c *Client) Health() error {
	resp, err := c.do("GET", "/health", nil)
	if err != nil {
		return fmt.Errorf("daemon not reachable at %s: %w", c.socketPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon unhealthy: status %d", resp.StatusCode)
	}
	return nil
}

type sessionResponse struct {
	ID        string    `json:"ID"`
	Name      string    `json:"Name"`
	Status    string    `json:"Status"`
	Template  string    `json:"Template"`
	CreatedAt time.Time `json:"CreatedAt"`
	UpdatedAt time.Time `json:"UpdatedAt"`
}

type eventResponse struct {
	ID        int64     `json:"ID"`
	SessionID string    `json:"SessionID"`
	Timestamp time.Time `json:"Timestamp"`
	Type      string    `json:"Type"`
	Data      string    `json:"Data"`
}

// CreateSession creates a new session via the daemon.
func (c *Client) CreateSession(name, template string) (sessionResponse, error) {
	resp, err := c.do("POST", "/sessions", map[string]string{
		"name":     name,
		"template": template,
	})
	if err != nil {
		return sessionResponse{}, err
	}
	defer resp.Body.Close()
	var sess sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return sessionResponse{}, fmt.Errorf("decode session: %w", err)
	}
	return sess, nil
}

// ListSessions lists all sessions.
func (c *Client) ListSessions() ([]sessionResponse, error) {
	resp, err := c.do("GET", "/sessions", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var sessions []sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("decode sessions: %w", err)
	}
	return sessions, nil
}

// GetSession gets a single session by ID.
func (c *Client) GetSession(id string) (sessionResponse, error) {
	resp, err := c.do("GET", "/sessions/"+id, nil)
	if err != nil {
		return sessionResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return sessionResponse{}, fmt.Errorf("session %s not found", id)
	}
	var sess sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return sessionResponse{}, fmt.Errorf("decode session: %w", err)
	}
	return sess, nil
}

// UpdateSession updates session status.
func (c *Client) UpdateSession(id, status string) (sessionResponse, error) {
	resp, err := c.do("PATCH", "/sessions/"+id, map[string]string{
		"status": status,
	})
	if err != nil {
		return sessionResponse{}, err
	}
	defer resp.Body.Close()
	var sess sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return sessionResponse{}, fmt.Errorf("decode session: %w", err)
	}
	return sess, nil
}

// mustJSON serialises v to a JSON string, panicking on error (only for static values).
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// LogEvent posts an event to a session.
func (c *Client) LogEvent(sessionID, eventType, data string) error {
	body := map[string]any{
		"type": eventType,
		"data": data,
	}
	resp, err := c.do("POST", "/sessions/"+sessionID+"/events", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		return fmt.Errorf("log event: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// GetEvents returns events for a session.
func (c *Client) GetEvents(sessionID string) ([]eventResponse, error) {
	return c.GetEventsAfter(sessionID, 0, 0)
}

// GetEventsAfter returns events for a session after the given event ID. When
// waitFor is positive, the daemon long-polls until new events arrive or the
// wait interval expires.
func (c *Client) GetEventsAfter(sessionID string, afterID int64, waitFor time.Duration) ([]eventResponse, error) {
	path := "/sessions/" + sessionID + "/events"
	query := url.Values{}
	if afterID > 0 {
		query.Set("after", strconv.FormatInt(afterID, 10))
	}
	if waitFor > 0 {
		query.Set("wait", waitFor.String())
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var events []eventResponse
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, fmt.Errorf("decode events: %w", err)
	}
	return events, nil
}

// WatchSessions streams multiplexed session events over the SSE endpoint until
// the provided context is cancelled.
func (c *Client) WatchSessions(ctx context.Context, sessionIDs []string, afterID int64, onEvent func(eventResponse) error) error {
	query := url.Values{}
	query.Set("sessions", strings.Join(sessionIDs, ","))
	if afterID > 0 {
		query.Set("after", strconv.FormatInt(afterID, 10))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "http://daemon/events/stream?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	// Use a streaming-specific HTTP client without a timeout so the SSE
	// connection is not killed by the default 10s client timeout.
	streamClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", c.socketPath)
			},
		},
	}
	resp, err := streamClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("watch sessions: status %d: %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var payload strings.Builder
	var eventType string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			payload.WriteString(strings.TrimPrefix(line, "data: "))
			continue
		}
		if line == "" && payload.Len() > 0 {
			if eventType == "error" {
				payload.Reset()
				eventType = ""
				continue
			}
			var evt eventResponse
			if err := json.Unmarshal([]byte(payload.String()), &evt); err != nil {
				payload.Reset()
				eventType = ""
				continue
			}
			if err := onEvent(evt); err != nil {
				return err
			}
			payload.Reset()
			eventType = ""
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("watch sessions: %w", err)
	}
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

type workbenchResponse struct {
	ID        string                   `json:"id"`
	SessionID string                   `json:"session_id"`
	Status    string                   `json:"status"`
	Endpoints map[string]string        `json:"endpoints"`
	Services  []workbenchServiceStatus `json:"services"`
	Spec      string                   `json:"spec"`
	CreatedAt time.Time                `json:"created_at"`
	UpdatedAt time.Time                `json:"updated_at"`
}

type workbenchServiceStatus struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Health string `json:"health"`
}

// CreateWorkbench provisions a workbench for a session via the daemon.
func (c *Client) CreateWorkbench(sessionID, spec string) (workbenchResponse, error) {
	resp, err := c.do("POST", "/sessions/"+sessionID+"/workbench", map[string]string{
		"session_id": sessionID,
		"spec":       spec,
	})
	if err != nil {
		return workbenchResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return workbenchResponse{}, fmt.Errorf("create workbench: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var wb workbenchResponse
	if err := json.NewDecoder(resp.Body).Decode(&wb); err != nil {
		return workbenchResponse{}, fmt.Errorf("decode workbench: %w", err)
	}
	return wb, nil
}

// GetWorkbenchStatus retrieves the workbench state for a session via the daemon.
func (c *Client) GetWorkbenchStatus(sessionID string) (workbenchResponse, error) {
	resp, err := c.do("GET", "/sessions/"+sessionID+"/workbench", nil)
	if err != nil {
		return workbenchResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return workbenchResponse{}, fmt.Errorf("workbench for session %s not found", sessionID)
	}
	var wb workbenchResponse
	if err := json.NewDecoder(resp.Body).Decode(&wb); err != nil {
		return workbenchResponse{}, fmt.Errorf("decode workbench: %w", err)
	}
	return wb, nil
}

// DeleteWorkbenchBySession deletes workbench records for a session via the daemon API.
func (c *Client) DeleteWorkbenchBySession(sessionID string) error {
	resp, err := c.do("DELETE", "/sessions/"+sessionID+"/workbench", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete workbench: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// RegisterTool registers a tool for a session.
func (c *Client) RegisterTool(sessionID string, spec agent.ToolSpec) error {
	resp, err := c.do("POST", "/sessions/"+url.PathEscape(sessionID)+"/tools", spec)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register tool: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// --- Tool routing client methods ---

// toolResultResponse is the JSON response from a tool execution.
type toolResultResponse struct {
	Output     string `json:"output"`
	Stderr     string `json:"stderr,omitempty"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Target     string `json:"target"`
}

// toolSpecResponse mirrors agent.ToolSpec for client-side deserialization.
type toolSpecResponse struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Input       map[string]string `json:"input"`
	Exec        struct {
		Target  string `json:"target"`
		Command string `json:"command"`
		Timeout int    `json:"timeout,omitempty"`
	} `json:"exec"`
}

// ExecuteTool runs a named tool for a session and returns the result.
func (c *Client) ExecuteTool(sessionID, toolName string, input map[string]string, callingAgent string) (toolResultResponse, error) {
	path := fmt.Sprintf("/sessions/%s/tools/%s", url.PathEscape(sessionID), url.PathEscape(toolName))
	if callingAgent != "" {
		path += "?" + url.Values{"agent": {callingAgent}}.Encode()
	}
	resp, err := c.do("POST", path, input)
	if err != nil {
		return toolResultResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return toolResultResponse{}, fmt.Errorf("tool execute: status %d: %s", resp.StatusCode, string(body))
	}
	var result toolResultResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return toolResultResponse{}, fmt.Errorf("decode tool result: %w", err)
	}
	return result, nil
}

// ListTools returns the registered tools for a session.
func (c *Client) ListTools(sessionID string) ([]toolSpecResponse, error) {
	resp, err := c.do("GET", "/sessions/"+url.PathEscape(sessionID)+"/tools", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list tools: status %d: %s", resp.StatusCode, string(body))
	}
	var tools []toolSpecResponse
	if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
		return nil, fmt.Errorf("decode tools: %w", err)
	}
	return tools, nil
}

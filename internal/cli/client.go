package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

// ToolResult is the response from a tool execution.
type ToolResult struct {
	Output     string `json:"output"`
	Stderr     string `json:"stderr,omitempty"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Target     string `json:"target"`
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

// GetEvents returns events for a session.
func (c *Client) GetEvents(sessionID string) ([]eventResponse, error) {
	resp, err := c.do("GET", "/sessions/"+sessionID+"/events", nil)
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

// RegisterTool registers a ToolSpec for a session.
func (c *Client) RegisterTool(sessionID string, tool agent.ToolSpec) error {
	resp, err := c.do("POST", "/sessions/"+sessionID+"/tools", tool)
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

// ListTools returns the tools registered for a session.
func (c *Client) ListTools(sessionID string) ([]agent.ToolSpec, error) {
	resp, err := c.do("GET", "/sessions/"+sessionID+"/tools", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	var tools []agent.ToolSpec
	if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
		return nil, fmt.Errorf("decode tools: %w", err)
	}
	return tools, nil
}

// ExecuteTool executes a named tool for a session.
// callingAgent is optional — pass empty string if not applicable.
func (c *Client) ExecuteTool(sessionID, toolName string, input map[string]string, callingAgent string) (ToolResult, error) {
	path := "/sessions/" + sessionID + "/tools/" + toolName
	if callingAgent != "" {
		path += "?agent=" + callingAgent
	}

	// Use a longer timeout for tool execution (tools may take up to their configured timeout).
	origTimeout := c.http.Timeout
	c.http.Timeout = 5 * time.Minute
	defer func() { c.http.Timeout = origTimeout }()

	resp, err := c.do("POST", path, input)
	if err != nil {
		return ToolResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return ToolResult{}, fmt.Errorf("not found: %s", string(body))
	}
	if resp.StatusCode == http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		return ToolResult{}, fmt.Errorf("execution error: %s", string(body))
	}

	var result ToolResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ToolResult{}, fmt.Errorf("decode tool result: %w", err)
	}
	return result, nil
}

// mustJSON serialises v to a JSON string, panicking on error (only for static values).
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

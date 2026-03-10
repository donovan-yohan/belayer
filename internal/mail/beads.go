// internal/mail/beads.go
package mail

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// BeadsIssue represents a beads issue returned by bd list --json.
type BeadsIssue struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    int    `json:"priority"`
}

// BeadsStore wraps the bd CLI for mail storage.
type BeadsStore struct {
	dir string // directory containing .beads/
}

// NewBeadsStore initializes a beads database in the given directory.
// If already initialized, this is a no-op.
func NewBeadsStore(dir string, prefix string) (*BeadsStore, error) {
	store := &BeadsStore{dir: dir}

	// Check if already initialized
	cmd := exec.Command("bd", "list", "--json")
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		return store, nil // already initialized
	}

	// Initialize
	cmd = exec.Command("bd", "init", "--prefix", prefix, "--stealth", "--skip-hooks", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("bd init: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return store, nil
}

// Create creates a new beads issue with the given title, description, and labels.
func (b *BeadsStore) Create(title, description string, labels map[string]string) error {
	args := []string{"create", "--title", title, "--description", description}
	for k, v := range labels {
		args = append(args, "--label", fmt.Sprintf("%s:%s", k, v))
	}
	args = append(args, "--json")

	cmd := exec.Command("bd", args...)
	cmd.Dir = b.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd create: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// List returns open issues labeled with to:<address>.
func (b *BeadsStore) List(toAddress string) ([]BeadsIssue, error) {
	label := fmt.Sprintf("to:%s", toAddress)
	cmd := exec.Command("bd", "list", "--label", label, "--status", "open", "--flat", "--json")
	cmd.Dir = b.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("bd list: %s: %w", strings.TrimSpace(string(out)), err)
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "[]" || trimmed == "null" {
		return nil, nil
	}

	var issues []BeadsIssue
	if err := json.Unmarshal([]byte(trimmed), &issues); err != nil {
		return nil, fmt.Errorf("parsing bd list output: %w", err)
	}
	return issues, nil
}

// Close closes a beads issue by ID (marks as read).
func (b *BeadsStore) Close(id string) error {
	cmd := exec.Command("bd", "close", id)
	cmd.Dir = b.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd close %s: %s: %w", id, strings.TrimSpace(string(out)), err)
	}
	return nil
}

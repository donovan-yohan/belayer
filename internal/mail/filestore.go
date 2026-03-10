package mail

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MailMessage represents a mail message returned by FileStore.List.
type MailMessage struct {
	ID          string
	Title       string
	Description string
}

// FileStore implements mail storage using the filesystem.
// Messages are JSON files in per-address directories with unread/ and read/ subdirectories.
type FileStore struct {
	dir string // base mail directory (e.g., <instanceDir>/mail)
}

// NewFileStore creates a FileStore rooted at the given directory.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

// fileMessage is the on-disk JSON format for a mail message.
type fileMessage struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	From        string `json:"from,omitempty"`
	To          string `json:"to"`
	MsgType     string `json:"msg_type"`
}

// Create writes a message as a JSON file in the recipient's unread/ directory.
func (f *FileStore) Create(title, description string, labels map[string]string) error {
	to := labels["to"]
	if to == "" {
		return fmt.Errorf("missing 'to' label")
	}

	unreadDir := filepath.Join(f.dir, to, "unread")
	if err := os.MkdirAll(unreadDir, 0o755); err != nil {
		return fmt.Errorf("creating unread dir: %w", err)
	}

	msg := fileMessage{
		Title:       title,
		Description: description,
		From:        labels["from"],
		To:          to,
		MsgType:     labels["msg-type"],
	}

	data, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	filename := fmt.Sprintf("%d-%s-%s.json", time.Now().UnixNano(), sanitizeFilename(labels["msg-type"]), randomSuffix())
	finalPath := filepath.Join(unreadDir, filename)
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("writing message file: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("committing message file: %w", err)
	}
	return nil
}

// List returns unread messages for the given address.
func (f *FileStore) List(address string) ([]MailMessage, error) {
	unreadDir := filepath.Join(f.dir, address, "unread")
	entries, err := os.ReadDir(unreadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading unread dir: %w", err)
	}

	var messages []MailMessage
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(unreadDir, entry.Name()))
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("mail: failed to read message file %s: %v", entry.Name(), err)
			}
			continue
		}
		var msg fileMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("mail: skipping malformed message file %s: %v", entry.Name(), err)
			continue
		}
		messages = append(messages, MailMessage{
			ID:          entry.Name(),
			Title:       msg.Title,
			Description: msg.Description,
		})
	}
	return messages, nil
}

// Close moves a message from unread/ to read/ for the given address.
// The id is the filename (e.g., "1741234567-done.json").
func (f *FileStore) Close(address, id string) error {
	src := filepath.Join(f.dir, address, "unread", id)
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("message %s not found at address %s", id, address)
		}
		return fmt.Errorf("checking message %s: %w", id, err)
	}

	readDir := filepath.Join(f.dir, address, "read")
	if err := os.MkdirAll(readDir, 0o755); err != nil {
		return fmt.Errorf("creating read dir: %w", err)
	}
	dst := filepath.Join(readDir, id)
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("moving %s to read: %w", id, err)
	}
	return nil
}

// randomSuffix returns a random 4-byte hex string to prevent filename collisions.
func randomSuffix() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// sanitizeFilename replaces characters unsafe for filenames.
func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '-'
		}
		return r
	}, s)
}

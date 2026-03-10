// internal/mail/read.go
package mail

import (
	"fmt"
	"log"
	"strings"
)

// FormatMessages formats a list of beads issues for terminal output.
func FormatMessages(issues []BeadsIssue) string {
	if len(issues) == 0 {
		return "No unread messages.\n"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d unread message(s):\n\n", len(issues)))

	for i, issue := range issues {
		b.WriteString(fmt.Sprintf("--- Message %d [%s] ---\n", i+1, issue.ID))
		b.WriteString(issue.Description)
		b.WriteString("\n\n")
	}

	return b.String()
}

// ReadInbox lists unread messages for the given address.
func ReadInbox(store *BeadsStore, address string) ([]BeadsIssue, error) {
	return store.List(address)
}

// ReadAndClose lists unread messages, prints them, and closes them.
func ReadAndClose(store *BeadsStore, address string) (string, error) {
	issues, err := store.List(address)
	if err != nil {
		return "", err
	}

	output := FormatMessages(issues)

	// Close all read messages
	for _, issue := range issues {
		if closeErr := store.Close(issue.ID); closeErr != nil {
			log.Printf("warning: failed to close message %s: %v", issue.ID, closeErr)
		}
	}

	return output, nil
}

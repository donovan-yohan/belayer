package mail

import (
	"fmt"
	"log"

	"github.com/donovan-yohan/belayer/internal/tmux"
)

// SendOpts configures a mail send operation.
type SendOpts struct {
	To      string
	From    string // auto-populated from BELAYER_MAIL_ADDRESS if empty
	Type    MessageType
	Subject string // optional override; uses DefaultSubject if empty
	Body    string
}

// Send writes a message to beads and delivers a nudge via tmux.
func Send(store *BeadsStore, tm tmux.TmuxManager, opts SendOpts) error {
	if !opts.Type.Valid() {
		return fmt.Errorf("invalid message type: %q", opts.Type)
	}

	// Parse and validate address
	addr, err := ParseAddress(opts.To)
	if err != nil {
		return fmt.Errorf("invalid address: %w", err)
	}

	// Render template
	rendered, err := RenderTemplate(opts.Type, opts.Body)
	if err != nil {
		return fmt.Errorf("rendering template: %w", err)
	}

	// Resolve subject
	subject := opts.Subject
	if subject == "" {
		subject = DefaultSubject(opts.Type)
	}

	// Write to beads
	labels := map[string]string{
		"to":       opts.To,
		"msg-type": string(opts.Type),
	}
	if opts.From != "" {
		labels["from"] = opts.From
	}

	if err := store.Create(subject, rendered, labels); err != nil {
		return fmt.Errorf("writing to beads: %w", err)
	}

	// Deliver via tmux (best-effort — message is durable in beads)
	session, window := addr.TmuxTarget()
	nudgeText := "You have a new message. Run `belayer mail read` to see it."
	if err := NudgeSession(tm, session, window, nudgeText); err != nil {
		log.Printf("mail: delivery failed (message persisted in beads): %v", err)
	}

	return nil
}

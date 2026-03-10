// internal/mail/templates.go
package mail

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed templates/*.md.tmpl
var templateFS embed.FS

var defaultSubjects = map[MessageType]string{
	MessageTypeGoalAssignment: "Goal Assignment",
	MessageTypeDone:           "Goal Complete",
	MessageTypeSpotResult:     "Spotter Result",
	MessageTypeVerdict:        "Anchor Verdict",
	MessageTypeFeedback:       "Feedback",
	MessageTypeInstruction:    "Instruction",
}

// DefaultSubject returns the default subject line for a message type.
func DefaultSubject(mt MessageType) string {
	if s, ok := defaultSubjects[mt]; ok {
		return s
	}
	return string(mt)
}

type templateData struct {
	Body string
}

// RenderTemplate applies the template for the given message type to the body.
func RenderTemplate(mt MessageType, body string) (string, error) {
	if !mt.Valid() {
		return "", fmt.Errorf("unknown message type: %q", mt)
	}

	filename := fmt.Sprintf("templates/%s.md.tmpl", string(mt))
	content, err := templateFS.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("loading template for %s: %w", mt, err)
	}

	tmpl, err := template.New(string(mt)).Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("parsing template for %s: %w", mt, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData{Body: body}); err != nil {
		return "", fmt.Errorf("rendering template for %s: %w", mt, err)
	}

	return buf.String(), nil
}

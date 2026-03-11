package tracker

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/donovan-yohan/belayer/internal/model"
)

type SpecAssemblyOutput struct {
	Spec   string           `json:"spec"`
	Climbs model.ClimbsFile `json:"climbs"`
}

func BuildSpecAssemblyPrompt(issue *model.Issue, repoNames []string) string {
	var b strings.Builder
	b.WriteString("You are a spec assembly agent. Convert the following tracker issue into a belayer problem spec and suggested climbs.\n\n")
	b.WriteString("## Tracker Issue\n\n")
	b.WriteString(fmt.Sprintf("**ID:** %s\n", issue.ID))
	b.WriteString(fmt.Sprintf("**Title:** %s\n", issue.Title))
	b.WriteString(fmt.Sprintf("**Body:**\n%s\n\n", issue.Body))
	if len(issue.Comments) > 0 {
		b.WriteString("**Discussion:**\n")
		for _, c := range issue.Comments {
			b.WriteString(fmt.Sprintf("- @%s: %s\n", c.Author, c.Body))
		}
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("**Available repos:** %s\n\n", strings.Join(repoNames, ", ")))
	b.WriteString("## Output Format\n\n")
	b.WriteString("Respond with a single JSON object (no markdown fences):\n\n")
	b.WriteString(`{"spec": "<markdown problem spec>", "climbs": {"repos": {"<repo>": {"climbs": [{"id": "<id>", "description": "<desc>", "depends_on": []}]}}}}`)
	b.WriteString("\n\nThe spec should be a clear, actionable problem description.\n")
	b.WriteString("Each climb should be a self-contained unit of work in a single repo.\n")
	b.WriteString("Use short kebab-case IDs for climb IDs.\n")
	return b.String()
}

func ParseSpecAssemblyOutput(raw string) (*SpecAssemblyOutput, error) {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		if len(lines) >= 3 {
			cleaned = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	var out SpecAssemblyOutput
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return nil, fmt.Errorf("parsing spec assembly output: %w", err)
	}
	return &out, nil
}

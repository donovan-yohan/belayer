package route

import (
	"fmt"
	"sort"
	"strings"

	"github.com/donovan-yohan/belayer/internal/pipeline"
)

// BuildRoutePrompt generates a structured route description block for injection
// into the LLM prompt. Same pattern as gate.BuildGatePrompt().
func BuildRoutePrompt(node pipeline.NodeConfig) string {
	if node.Routes == nil || len(node.Routes.Options) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\nChoose one of the following routes:\n")

	// Sort option names for deterministic prompt output
	names := make([]string, 0, len(node.Routes.Options))
	for name := range node.Routes.Options {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		opt := node.Routes.Options[name]
		desc := strings.TrimSpace(opt.Description)
		if desc != "" {
			fmt.Fprintf(&sb, "- %s: %s\n", name, desc)
		} else {
			fmt.Fprintf(&sb, "- %s\n", name)
		}
	}
	sb.WriteString("\nYou MUST choose exactly one route. Output your choice as structured JSON.\n")
	return sb.String()
}

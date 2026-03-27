package gate

import (
	"fmt"
	"strings"

	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// BuildGatePrompt constructs the structured prompt for a gate's Claude session.
// This prompt includes dimension definitions, rubrics, and output format instructions.
func BuildGatePrompt(node pipeline.NodeConfig) string {
	var sb strings.Builder

	sb.WriteString("You are evaluating work as a quality gate.\n\n")

	// Dimensions
	sb.WriteString("Score each dimension from 0-10. For each, provide:\n")
	sb.WriteString("- A score (integer 0-10)\n")
	sb.WriteString("- A brief rationale (1-2 sentences)\n")
	sb.WriteString("- Specific issues found (if any)\n\n")
	sb.WriteString("Dimensions:\n\n")

	for _, dim := range node.Dimensions {
		sb.WriteString(fmt.Sprintf("- **%s** (weight: %.2f): %s\n", dim.Name, dim.Weight, dim.Description))
		if dim.Rubric != "" {
			sb.WriteString(fmt.Sprintf("  Rubric: %s\n", dim.Rubric))
		}
	}

	// Output instructions
	sb.WriteString("\nProduce two files:\n\n")
	sb.WriteString("1. `.belayer/.internal/output/gate-result.json` — structured scores per dimension:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"gate\": \"" + node.Name + "\",\n")
	sb.WriteString("  \"attempt\": 0,\n")
	sb.WriteString("  \"dimensions\": {\n")
	for i, dim := range node.Dimensions {
		comma := ","
		if i == len(node.Dimensions)-1 {
			comma = ""
		}
		sb.WriteString(fmt.Sprintf("    \"%s\": {\"score\": 0, \"rationale\": \"\", \"issues\": []}%s\n", dim.Name, comma))
	}
	sb.WriteString("  },\n")
	sb.WriteString("  \"weighted_score\": 0,\n")
	sb.WriteString("  \"outcome\": \"PASS\",\n")
	sb.WriteString("  \"summary\": \"\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")
	sb.WriteString("2. `.belayer/.internal/output/rationale.md` — human-readable review with action items for each dimension.\n\n")
	sb.WriteString("Be rigorous. The only way to improve the score is to genuinely improve the work.\n")

	return sb.String()
}

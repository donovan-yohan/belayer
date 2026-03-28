package pipeline

import (
	"fmt"
	"strings"
)

// Visualize renders a PipelineConfig as a human-readable ASCII representation
// showing intake sources, node graph with routing, and gate annotations.
func Visualize(cfg *PipelineConfig) string {
	var b strings.Builder

	// Header.
	b.WriteString(fmt.Sprintf("Pipeline: %s\n", cfg.Name))
	b.WriteString(strings.Repeat("=", len(cfg.Name)+10))
	b.WriteString("\n\n")

	// Intake sources.
	if len(cfg.Intake) > 0 {
		b.WriteString("INTAKE:\n")
		for _, intake := range cfg.Intake {
			label := intake.Type
			if intake.Type == "interactive" {
				label = "interactive (belayer start)"
			} else if interval, ok := intake.Config["poll_interval"]; ok {
				label = fmt.Sprintf("%s (every %s)", intake.Type, interval)
			}
			b.WriteString(fmt.Sprintf("  [%s] → %s\n", intake.Name, label))
		}
		b.WriteString("\n")
	}

	// Node graph.
	b.WriteString("NODES:\n")
	for i, node := range cfg.Nodes {
		// Node type indicator.
		typeStr := "node"
		if node.IsGate() {
			typeStr = "gate"
		}

		// Output type.
		outputStr := node.Output.Type
		if outputStr == "" {
			outputStr = "?"
		}

		// Build the line.
		b.WriteString(fmt.Sprintf("  %s (%s)", node.Name, typeStr))

		// Show output type and routing.
		if node.OnPass != "" && node.OnPass != "stop" {
			nextName := node.OnPass
			if node.OnPass == "next" {
				if i+1 < len(cfg.Nodes) {
					nextName = cfg.Nodes[i+1].Name
				} else {
					nextName = "done"
				}
			}
			b.WriteString(fmt.Sprintf(" ──[%s]──► %s", outputStr, nextName))
		} else {
			b.WriteString(fmt.Sprintf(" ──[%s]──► done", outputStr))
		}
		b.WriteString("\n")

		// Show retry routing.
		if node.OnRetry != "" && node.OnRetry != "stop" {
			target := node.OnRetry
			if target == "self" {
				target = node.Name
			}
			b.WriteString(fmt.Sprintf("    ↺ on_retry → %s", target))
			if node.MaxRetries > 0 {
				b.WriteString(fmt.Sprintf(" (max %d)", node.MaxRetries))
			}
			b.WriteString("\n")
		}

		// Gate-specific: show threshold.
		if node.IsGate() && node.Thresholds.Pass > 0 {
			b.WriteString(fmt.Sprintf("    threshold: pass=%.1f retry=%.1f\n", node.Thresholds.Pass, node.Thresholds.Retry))
		}
	}

	// Safety.
	if cfg.Safety.MaxConcurrentRuns > 0 {
		b.WriteString(fmt.Sprintf("\nSAFETY: max_concurrent_runs=%d\n", cfg.Safety.MaxConcurrentRuns))
	}

	return b.String()
}

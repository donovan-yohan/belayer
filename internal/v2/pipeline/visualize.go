package pipeline

import (
	"fmt"
	"strings"

	"github.com/donovan-yohan/belayer/internal/v2/role"
)

// StatusMarker represents the visual state of a role in the pipeline.
type StatusMarker string

const (
	MarkerPending  StatusMarker = "○"
	MarkerActive   StatusMarker = "●"
	MarkerComplete StatusMarker = "✓"
	MarkerFailed   StatusMarker = "✗"
	MarkerFlared   StatusMarker = "!"
)

// RoleStatus maps role names to their current status markers.
type RoleStatus map[string]StatusMarker

// Visualize renders an ASCII diagram of the pipeline with optional status markers.
func Visualize(route *Route, status RoleStatus) string {
	if status == nil {
		status = make(RoleStatus)
	}

	var sb strings.Builder
	phaseNames := map[role.Phase]string{
		role.PhaseApproach: "APPROACH",
		role.PhaseAscent:   "ASCENT",
		role.PhaseSend:     "SEND",
	}

	// Header.
	sb.WriteString("Pipeline: " + route.Name + "\n")
	sb.WriteString(strings.Repeat("─", 60) + "\n")

	for i, phase := range route.Phases {
		phaseName := phaseNames[phase.Phase]
		if phaseName == "" {
			phaseName = string(phase.Phase)
		}
		sb.WriteString(fmt.Sprintf("\n  %s\n", phaseName))

		for j, r := range phase.Roles {
			marker := status[r.Name]
			if marker == "" {
				marker = MarkerPending
			}

			contractLabel := "pitch"
			if r.ContractType == role.TypeB {
				contractLabel = "ascent"
			}

			sb.WriteString(fmt.Sprintf("    [%s]%s  (%s)", r.Name, marker, contractLabel))

			// Arrow to next role (within phase).
			if j < len(phase.Roles)-1 {
				sb.WriteString("  →")
			}
			sb.WriteString("\n")
		}

		// Show loops.
		for _, loop := range phase.Loops {
			sb.WriteString(fmt.Sprintf("    ◄── loop: %s → %s (max %d)\n", loop.From, loop.To, loop.MaxIterations))
		}

		// Arrow to next phase.
		if i < len(route.Phases)-1 {
			sb.WriteString("         │\n")
			sb.WriteString("         ▼\n")
		}
	}

	sb.WriteString(strings.Repeat("─", 60) + "\n")
	return sb.String()
}

package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Pane borders
	activeBorder  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63"))
	inactiveBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))

	// Selection
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("63"))
	normalStyle   = lipgloss.NewStyle()

	// Status badges
	statusPending   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).SetString("pending")
	statusRunning   = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true).SetString("running")
	statusComplete  = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).SetString("complete")
	statusFailed    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).SetString("failed")
	statusStuck     = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).SetString("stuck")
	statusDecompose = lipgloss.NewStyle().Foreground(lipgloss.Color("141")).SetString("decomposing")
	statusAligning  = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).SetString("aligning")

	// Header and status bar
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).Padding(0, 1)
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Padding(0, 1)
	titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Padding(0, 1)

	// Labels
	dimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	boldStyle = lipgloss.NewStyle().Bold(true)
)

// StatusBadge returns a styled status string for task status.
func StatusBadge(status string) string {
	switch status {
	case "pending":
		return statusPending.Render()
	case "running":
		return statusRunning.Render()
	case "complete":
		return statusComplete.Render()
	case "failed":
		return statusFailed.Render()
	case "stuck":
		return statusStuck.Render()
	case "decomposing":
		return statusDecompose.Render()
	case "aligning":
		return statusAligning.Render()
	default:
		return status
	}
}

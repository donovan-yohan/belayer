package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/donovan-yohan/belayer/internal/model"
)

func renderHeader(instanceName string, width int) string {
	title := headerStyle.Render("belayer")
	inst := dimStyle.Render(fmt.Sprintf(" instance: %s", instanceName))
	help := dimStyle.Render("tab:pane  j/k:nav  enter:select  q:quit  r:refresh")

	left := title + inst
	right := help
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

func renderTaskList(tasks []TaskSummary, cursor int, focused bool, width, height int) string {
	var b strings.Builder

	title := titleStyle.Render("Tasks")
	b.WriteString(title)
	b.WriteString("\n")

	if len(tasks) == 0 {
		b.WriteString(dimStyle.Render("  No tasks found"))
		b.WriteString("\n")
	}

	// Calculate visible area (subtract title line and border)
	visibleHeight := height - 2
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	// Calculate scroll offset to keep cursor visible
	scrollOffset := 0
	if cursor >= visibleHeight {
		scrollOffset = cursor - visibleHeight + 1
	}

	for i, ts := range tasks {
		if i < scrollOffset {
			continue
		}
		if i-scrollOffset >= visibleHeight {
			break
		}

		badge := StatusBadge(string(ts.Task.Status))
		desc := truncate(ts.Task.Description, width-30)
		leadInfo := fmt.Sprintf("%d/%d leads", ts.LeadsDone, ts.LeadCount)
		if ts.LeadsFailed > 0 {
			leadInfo += fmt.Sprintf(" (%d failed)", ts.LeadsFailed)
		}

		line := fmt.Sprintf("  [%s] %s  %s", badge, desc, dimStyle.Render(leadInfo))

		if i == cursor && focused {
			line = selectedStyle.Render(fmt.Sprintf(" > [%s] %s  %s ", string(ts.Task.Status), desc, leadInfo))
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	style := inactiveBorder.Width(width - 2).Height(height)
	if focused {
		style = activeBorder.Width(width - 2).Height(height)
	}

	return style.Render(b.String())
}

func renderTaskDetail(task *TaskSummary, leads []LeadDetail, cursor int, focused bool, width, height int) string {
	var b strings.Builder

	title := titleStyle.Render("Detail")
	b.WriteString(title)
	b.WriteString("\n")

	if task == nil {
		b.WriteString(dimStyle.Render("  Select a task to view details"))
		b.WriteString("\n")

		style := inactiveBorder.Width(width - 2).Height(height)
		if focused {
			style = activeBorder.Width(width - 2).Height(height)
		}
		return style.Render(b.String())
	}

	// Task info header
	b.WriteString(fmt.Sprintf("  %s  [%s]\n", boldStyle.Render(task.Task.ID), StatusBadge(string(task.Task.Status))))
	desc := task.Task.Description
	if len(desc) > width-6 {
		desc = desc[:width-9] + "..."
	}
	b.WriteString(fmt.Sprintf("  %s\n", desc))
	b.WriteString(fmt.Sprintf("  %s  source: %s\n",
		dimStyle.Render(RelativeTime(task.Task.CreatedAt)),
		dimStyle.Render(task.Task.Source)))
	b.WriteString("\n")

	// Leads
	b.WriteString(boldStyle.Render("  Leads:"))
	b.WriteString("\n")

	if len(leads) == 0 {
		b.WriteString(dimStyle.Render("  No leads yet"))
		b.WriteString("\n")
	}

	for i, ld := range leads {
		badge := StatusBadge(string(ld.Lead.Status))
		goalProgress := ""
		if len(ld.Goals) > 0 {
			done := 0
			for _, g := range ld.Goals {
				if g.Status == model.LeadGoalComplete {
					done++
				}
			}
			goalProgress = fmt.Sprintf(" %d/%d goals", done, len(ld.Goals))
		}

		attempt := ""
		if ld.Lead.Attempt > 0 {
			attempt = fmt.Sprintf(" (attempt %d)", ld.Lead.Attempt+1)
		}

		line := fmt.Sprintf("  %s [%s]%s%s", ld.RepoName, badge, goalProgress, dimStyle.Render(attempt))

		if i == cursor && focused {
			line = selectedStyle.Render(fmt.Sprintf(" > %s [%s]%s%s ",
				ld.RepoName, string(ld.Lead.Status), goalProgress, attempt))
		}

		b.WriteString(line)
		b.WriteString("\n")

		// Show goals for selected lead
		if i == cursor && len(ld.Goals) > 0 {
			for _, g := range ld.Goals {
				goalBadge := StatusBadge(string(g.Status))
				goalDesc := truncate(g.Description, width-16)
				b.WriteString(fmt.Sprintf("    %s %s\n", goalBadge, dimStyle.Render(goalDesc)))
			}
		}
	}

	style := inactiveBorder.Width(width - 2).Height(height)
	if focused {
		style = activeBorder.Width(width - 2).Height(height)
	}

	return style.Render(b.String())
}

func renderEventLog(events []EventEntry, scrollOffset int, focused bool, width, height int) string {
	var b strings.Builder

	title := titleStyle.Render("Events")
	b.WriteString(title)
	b.WriteString("\n")

	if len(events) == 0 {
		b.WriteString(dimStyle.Render("  No events"))
		b.WriteString("\n")
	}

	visibleHeight := height - 2
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	for i, entry := range events {
		if i < scrollOffset {
			continue
		}
		if i-scrollOffset >= visibleHeight {
			break
		}

		ts := dimStyle.Render(RelativeTime(entry.Event.CreatedAt))
		eventType := StatusBadge(eventTypeLabel(string(entry.Event.Type)))
		repo := ""
		if entry.RepoName != "" {
			repo = dimStyle.Render(fmt.Sprintf(" [%s]", entry.RepoName))
		}

		b.WriteString(fmt.Sprintf("  %s %s%s\n", ts, eventType, repo))
	}

	style := inactiveBorder.Width(width - 2).Height(height)
	if focused {
		style = activeBorder.Width(width - 2).Height(height)
	}

	return style.Render(b.String())
}

func renderStatusBar(taskCount, activeCount int, width int) string {
	info := fmt.Sprintf(" %d tasks  |  %d active", taskCount, activeCount)
	return statusBarStyle.Width(width).Render(info)
}

func truncate(s string, maxLen int) string {
	if maxLen < 4 {
		maxLen = 4
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func eventTypeLabel(eventType string) string {
	switch eventType {
	case "task_created":
		return "pending"
	case "task_decomposed":
		return "decomposing"
	case "lead_started":
		return "running"
	case "lead_progress":
		return "running"
	case "lead_stuck":
		return "stuck"
	case "lead_complete":
		return "complete"
	case "lead_failed":
		return "failed"
	case "alignment_started":
		return "aligning"
	case "alignment_passed":
		return "complete"
	case "alignment_failed":
		return "failed"
	case "prs_created":
		return "complete"
	default:
		return eventType
	}
}

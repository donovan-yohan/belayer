package daemon

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/climbpath"
	"github.com/donovan-yohan/belayer/internal/store"
)

const gitProbeTimeout = 2 * time.Second

func (d *Daemon) WriteHandoffArtifact(sessionID string) (string, error) {
	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(sess.WorkspaceDir) == "" {
		return "", fmt.Errorf("session %s has no workspace_dir", sessionID)
	}

	runDir := climbpath.SessionDir(sess.WorkspaceDir, sessionID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir handoff dir: %w", err)
	}
	path := filepath.Join(runDir, "handoff.md")

	runs, err := d.store.ListAgentRuns(sessionID)
	if err != nil {
		return "", err
	}
	artifacts, err := d.store.ListArtifacts(sessionID)
	if err != nil {
		return "", err
	}
	events, err := d.store.QueryEvents(sessionID)
	if err != nil {
		return "", err
	}
	states := d.agentSurfaceStates(sessionID)
	exitConditions, exitSource := d.resolveExitConditions(sessionID)
	persistenceStrategy, persistenceSource := d.resolvePersistenceStrategy(sessionID)

	var b bytes.Buffer
	fmt.Fprintf(&b, "# Handoff\n\nGenerated: %s UTC\n\n", time.Now().UTC().Format(time.RFC3339))

	b.WriteString("## Session\n\n")
	fmt.Fprintf(&b, "- Session ID: `%s`\n", sess.ID)
	fmt.Fprintf(&b, "- Task: %s\n", safeMarkdownText(sess.Name))
	fmt.Fprintf(&b, "- Status: `%s`\n", sess.Status)
	fmt.Fprintf(&b, "- Workspace: `%s`\n", sess.WorkspaceDir)
	fmt.Fprintf(&b, "- Exit conditions source: `%s`\n", exitSource)
	if len(exitConditions) == 0 {
		b.WriteString("- Exit conditions: none\n")
	} else {
		for _, cond := range exitConditions {
			fmt.Fprintf(&b, "- Exit condition: %s\n", safeMarkdownText(cond))
		}
	}
	fmt.Fprintf(&b, "- Persistence strategy source: `%s`\n", persistenceSource)
	if len(persistenceStrategy) == 0 {
		b.WriteString("- Persistence strategy: none\n")
	} else {
		for _, step := range persistenceStrategy {
			fmt.Fprintf(&b, "- Persistence step: %s\n", safeMarkdownText(step))
		}
	}

	b.WriteString("\n## Roster\n\n")
	if len(runs) == 0 {
		b.WriteString("- No agents registered.\n")
	} else {
		for _, run := range runs {
			state := states[run.Name]
			fmt.Fprintf(
				&b,
				"- `%s`: lifecycle=`%s` outcome=`%s` last_event=`%s` branch=`%s` worktree=`%s` artifacts=%s\n",
				run.Name,
				state.Lifecycle,
				state.Outcome,
				formatTime(state.LastEventAt),
				emptyDash(run.Branch),
				emptyDash(run.WorktreePath),
				formatArtifactIDsForProducer(artifacts, run.Name),
			)
		}
	}

	b.WriteString("\n## Unacked Mail\n\n")
	unacked := d.collectUnackedMessages(sessionID, runs)
	if len(unacked) == 0 {
		b.WriteString("- None.\n")
	} else {
		for _, msg := range unacked {
			age := time.Since(msg.CreatedAt).Round(time.Second)
			fmt.Fprintf(
				&b,
				"- `%s -> %s` age=%s preview=%q\n",
				msg.SenderID,
				msg.RecipientID,
				age,
				truncateForPreview(msg.Content, 140),
			)
		}
	}

	b.WriteString("\n## Git State\n\n")
	for _, entry := range collectGitTargets(sess.WorkspaceDir, runs) {
		state := inspectGitState(entry)
		fmt.Fprintf(&b, "- `%s`: %s\n", entry, state)
	}

	b.WriteString("\n## Artifacts\n\n")
	if len(artifacts) == 0 {
		b.WriteString("- None.\n")
	} else {
		for _, artifact := range artifacts {
			fmt.Fprintf(
				&b,
				"- `%s` `%s` producer=`%s` summary=%q id=`%s`\n",
				artifact.Kind,
				artifact.Path,
				emptyDash(artifact.Producer),
				artifact.Summary,
				artifact.ID,
			)
		}
	}

	b.WriteString("\n## Recent Events\n\n")
	start := 0
	if len(events) > 20 {
		start = len(events) - 20
	}
	for _, evt := range events[start:] {
		fmt.Fprintf(
			&b,
			"- `%s` `%s` %s\n",
			evt.Timestamp.UTC().Format(time.RFC3339),
			evt.Type,
			truncateForPreview(evt.Data, 160),
		)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("write handoff: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(b.Bytes()); err != nil {
		return "", fmt.Errorf("write handoff: %w", err)
	}
	return path, nil
}

func (d *Daemon) registerHandoffArtifact(sessionID, absolutePath string) error {
	artifacts, err := d.store.ListArtifacts(sessionID)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if artifact.Kind == "handoff" {
			return nil
		}
	}
	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		return err
	}
	relPath := absolutePath
	if sess.WorkspaceDir != "" {
		if rel, relErr := filepath.Rel(sess.WorkspaceDir, absolutePath); relErr == nil {
			relPath = filepath.ToSlash(rel)
		}
	}
	_, err = d.store.CreateArtifact(store.Artifact{
		SessionID: sessionID,
		Kind:      "handoff",
		Path:      relPath,
		Producer:  "daemon",
		Summary:   "Deterministic incomplete-run handoff snapshot",
	})
	return err
}

func (d *Daemon) collectUnackedMessages(sessionID string, runs []store.AgentRun) []store.Message {
	recipients := map[string]struct{}{"supervisor": {}}
	for _, run := range runs {
		if agentRunIsMain(run) {
			recipients[run.Name] = struct{}{}
		}
	}
	var messages []store.Message
	for recipient := range recipients {
		pending, err := d.store.UnackedMessages(sessionID, recipient, "")
		if err != nil {
			continue
		}
		messages = append(messages, pending...)
	}
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].CreatedAt.Before(messages[j].CreatedAt)
	})
	return messages
}

func collectGitTargets(workspace string, runs []store.AgentRun) []string {
	seen := map[string]struct{}{}
	var targets []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		targets = append(targets, path)
	}
	add(workspace)
	for _, run := range runs {
		if run.WorktreePath != "" {
			add(run.WorktreePath)
			continue
		}
		add(run.Workdir)
	}
	sort.Strings(targets)
	return targets
}

func inspectGitState(path string) string {
	if path == "" {
		return "no path"
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), gitProbeTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--git-dir")
		if err := cmd.Run(); err != nil {
			return "not a git worktree"
		}
	}

	branch := gitOutput(path, "rev-parse", "--abbrev-ref", "HEAD")
	commit := gitOutput(path, "rev-parse", "--short", "HEAD")
	statusOut := gitOutput(path, "status", "--porcelain")
	dirty := "clean"
	if strings.TrimSpace(statusOut) != "" {
		dirty = "dirty"
	}
	ahead, behind := "0", "0"
	if counts := strings.Fields(gitOutput(path, "rev-list", "--left-right", "--count", "@{upstream}...HEAD")); len(counts) == 2 {
		behind, ahead = counts[0], counts[1]
	}
	return fmt.Sprintf("branch=%s ahead=%s behind=%s %s last_commit=%s", emptyDash(branch), ahead, behind, dirty, emptyDash(commit))
}

func gitOutput(path string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "-"
	}
	return strings.TrimSpace(string(out))
}

func formatArtifactIDsForProducer(artifacts []store.Artifact, producer string) string {
	if producer == "" {
		return "-"
	}
	var ids []string
	for _, artifact := range artifacts {
		if artifact.Producer == producer {
			ids = append(ids, artifact.ID)
		}
	}
	if len(ids) == 0 {
		return "-"
	}
	return strings.Join(ids, ",")
}

func truncateForPreview(s string, limit int) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " "))
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit-3] + "..."
}

func safeMarkdownText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return strings.ReplaceAll(s, "\n", " ")
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

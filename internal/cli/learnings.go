package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	agenticpkg "github.com/donovan-yohan/belayer/internal/agentic"
	"github.com/donovan-yohan/belayer/internal/crag"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/spf13/cobra"
)

func newLearningsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "learnings",
		Short: "Manage persistent learnings",
	}
	cmd.AddCommand(
		newLearningsListCmd(),
		newLearningsShowCmd(),
		newLearningsAddCmd(),
		newLearningsCompactCmd(),
	)
	return cmd
}

func newLearningsListCmd() *cobra.Command {
	var cragName string
	var category string
	var activeOnly bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List learnings for the current crag",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedName, err := resolveCragName(cragName)
			if err != nil {
				return err
			}

			_, cragDir, err := crag.Load(resolvedName)
			if err != nil {
				return fmt.Errorf("loading crag %q: %w", resolvedName, err)
			}

			dbPath := filepath.Join(cragDir, "belayer.db")
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			s := store.New(database.Conn())
			learnings, err := s.ListLearnings(resolvedName, activeOnly, category)
			if err != nil {
				return fmt.Errorf("listing learnings: %w", err)
			}

			if len(learnings) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No learnings found.")
				return nil
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%-26s  %-12s  %-10s  %-8s  %s\n", "ID", "CATEGORY", "SEVERITY", "RESOLVED", "DESCRIPTION")
			fmt.Fprintf(w, "%s\n", strings.Repeat("-", 90))
			for _, l := range learnings {
				resolved := "no"
				if l.Resolved {
					resolved = "yes"
				}
				desc := l.Description
				if len(desc) > 50 {
					desc = desc[:47] + "..."
				}
				fmt.Fprintf(w, "%-26s  %-12s  %-10s  %-8s  %s\n",
					l.ID, l.Category, l.Severity, resolved, desc)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	cmd.Flags().StringVar(&category, "category", "", "Filter by category")
	cmd.Flags().BoolVar(&activeOnly, "active", false, "Show only unresolved learnings")
	return cmd
}

func newLearningsShowCmd() *cobra.Command {
	var cragName string

	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show detail view of a single learning",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			resolvedName, err := resolveCragName(cragName)
			if err != nil {
				return err
			}

			_, cragDir, err := crag.Load(resolvedName)
			if err != nil {
				return fmt.Errorf("loading crag %q: %w", resolvedName, err)
			}

			dbPath := filepath.Join(cragDir, "belayer.db")
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			s := store.New(database.Conn())
			l, err := s.GetLearning(id)
			if err != nil {
				return fmt.Errorf("getting learning %q: %w", id, err)
			}

			if err := s.IncrementLearningAccess(id); err != nil {
				// Non-fatal: access count is best-effort
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: incrementing access count: %v\n", err)
			}

			resolved := "no"
			if l.Resolved {
				resolved = "yes"
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "ID:             %s\n", l.ID)
			fmt.Fprintf(w, "Crag:           %s\n", l.CragID)
			fmt.Fprintf(w, "Problem:        %s\n", l.ProblemID)
			fmt.Fprintf(w, "Category:       %s\n", l.Category)
			fmt.Fprintf(w, "Severity:       %s\n", l.Severity)
			fmt.Fprintf(w, "Resolved:       %s\n", resolved)
			fmt.Fprintf(w, "Access Count:   %d\n", l.AccessCount)
			fmt.Fprintf(w, "Created At:     %s\n", l.CreatedAt.Format(time.RFC3339))
			fmt.Fprintf(w, "\nDescription:\n  %s\n", l.Description)
			if l.Recommendation != "" {
				fmt.Fprintf(w, "\nRecommendation:\n  %s\n", l.Recommendation)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	return cmd
}

func newLearningsAddCmd() *cobra.Command {
	var cragName string
	var category string
	var description string
	var recommendation string
	var severity string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a manual learning",
		RunE: func(cmd *cobra.Command, args []string) error {
			if category == "" {
				return fmt.Errorf("--category is required")
			}
			if description == "" {
				return fmt.Errorf("--desc is required")
			}
			switch severity {
			case "":
				severity = string(model.LearningSeverityMedium)
			case string(model.LearningSeverityHigh), string(model.LearningSeverityMedium), string(model.LearningSeverityLow):
				// valid
			default:
				return fmt.Errorf("--severity must be one of: high, medium, low")
			}

			resolvedName, err := resolveCragName(cragName)
			if err != nil {
				return err
			}

			_, cragDir, err := crag.Load(resolvedName)
			if err != nil {
				return fmt.Errorf("loading crag %q: %w", resolvedName, err)
			}

			dbPath := filepath.Join(cragDir, "belayer.db")
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			s := store.New(database.Conn())

			l := model.Learning{
				CragID:         resolvedName,
				Category:       model.LearningCategory(category),
				Description:    description,
				Recommendation: recommendation,
				Severity:       model.LearningSeverity(severity),
			}

			if err := s.InsertLearning(l); err != nil {
				return fmt.Errorf("inserting learning: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Created learning")
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	cmd.Flags().StringVar(&category, "category", "", "Category for the learning (required)")
	cmd.Flags().StringVar(&description, "desc", "", "Description of the learning (required)")
	cmd.Flags().StringVar(&recommendation, "recommendation", "", "Recommendation based on the learning")
	cmd.Flags().StringVar(&severity, "severity", "", "Severity: high, medium, or low")
	return cmd
}

func newLearningsCompactCmd() *cobra.Command {
	var cragName string
	var modelName string

	cmd := &cobra.Command{
		Use:   "compact",
		Short: "Compact learnings (consolidate and deduplicate)",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedName, err := resolveCragName(cragName)
			if err != nil {
				return err
			}

			_, cragDir, err := crag.Load(resolvedName)
			if err != nil {
				return fmt.Errorf("loading crag %q: %w", resolvedName, err)
			}

			dbPath := filepath.Join(cragDir, "belayer.db")
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			s := store.New(database.Conn())
			learnings, err := s.ListLearnings(resolvedName, true, "")
			if err != nil {
				return fmt.Errorf("listing learnings: %w", err)
			}

			if len(learnings) < 2 {
				fmt.Fprintln(cmd.OutOrStdout(), "Not enough learnings to compact (need at least 2).")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Compacting %d active learnings...\n", len(learnings))

			result, err := agenticpkg.RunCompaction(cmd.Context(), modelName, learnings)
			if err != nil {
				return fmt.Errorf("running compaction: %w", err)
			}

			// Mark old learnings as resolved
			for _, id := range result.ResolvedIDs {
				if err := s.ResolveLearning(id); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: resolving learning %s: %v\n", id, err)
				}
			}

			// Insert compacted learnings
			for _, cl := range result.CompactedLearnings {
				l := model.Learning{
					CragID:         resolvedName,
					Category:       cl.Category,
					Description:    cl.Description,
					Recommendation: cl.Recommendation,
					Severity:       cl.Severity,
				}
				if err := s.InsertLearning(l); err != nil {
					return fmt.Errorf("inserting compacted learning: %w", err)
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Compacted: %d resolved, %d new learnings created.\n%s\n",
				len(result.ResolvedIDs), len(result.CompactedLearnings), result.Summary)
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	cmd.Flags().StringVar(&modelName, "model", "sonnet", "Claude model to use for compaction")
	return cmd
}

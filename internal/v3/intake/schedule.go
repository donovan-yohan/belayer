package intake

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// ReconcileSchedules creates, updates, or pauses Temporal schedules for automated intakes.
// Called on worker startup. Skips interactive intakes (no schedule needed).
//
// NOTE: Phase 1 stub — logs intended schedules but does not yet create them.
// Full Temporal Schedule API integration is deferred.
func ReconcileSchedules(ctx context.Context, _ client.ScheduleClient, pipelineName string, intakes []pipeline.IntakeConfig) error {
	for _, cfg := range intakes {
		if cfg.Type == "interactive" {
			continue
		}
		scheduleID := fmt.Sprintf("intake/%s/%s", pipelineName, cfg.Name)
		interval := parsePollInterval(cfg.Config["poll_interval"])
		fmt.Printf("  Schedule: %s (every %s)\n", scheduleID, interval)
	}
	return nil
}

// parsePollInterval parses a duration string from intake config.
// Supports Go duration strings like "5m", "1h", "30s".
// Defaults to 5 minutes if empty or unparseable.
func parsePollInterval(s string) time.Duration {
	if s == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		fmt.Printf("Warning: invalid poll_interval %q, defaulting to 5m\n", s)
		return 5 * time.Minute
	}
	return d
}

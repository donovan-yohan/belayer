package gate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// ScopedPath inserts an attempt suffix before the file extension, producing
// attempt-scoped output paths (e.g., "gate-result.json" → "gate-result-attempt-0.json").
// This prevents stale gate outputs from prior attempts from being reused.
func ScopedPath(basePath string, attempt int) string {
	ext := filepath.Ext(basePath)
	base := strings.TrimSuffix(basePath, ext)
	return fmt.Sprintf("%s-attempt-%d%s", base, attempt, ext)
}

// DimensionResult holds the score and rationale for a single gate dimension.
type DimensionResult struct {
	Score     float64  `json:"score"`
	Rationale string   `json:"rationale"`
	Issues    []string `json:"issues,omitempty"`
}

// GateResult is the structured output from a gate session (gate-result.json).
type GateResult struct {
	Gate          string                     `json:"gate"`
	Attempt       int                        `json:"attempt"`
	Timestamp     string                     `json:"timestamp,omitempty"`
	Dimensions    map[string]DimensionResult `json:"dimensions"`
	WeightedScore float64                    `json:"weighted_score"`
	Outcome       string                     `json:"outcome"`
	Summary       string                     `json:"summary,omitempty"`
}

// ParseGateResult unmarshals gate-result.json bytes into a GateResult.
func ParseGateResult(data []byte) (*GateResult, error) {
	var result GateResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse gate result: %w", err)
	}
	return &result, nil
}

// ValidateGateResult checks that all expected dimensions are present.
func ValidateGateResult(result *GateResult, expectedDimensions []string) error {
	for _, name := range expectedDimensions {
		if _, ok := result.Dimensions[name]; !ok {
			return fmt.Errorf("gate result missing dimension %q", name)
		}
	}
	return nil
}

// ComputeWeightedScore computes the weighted average score across dimensions.
// Scores are clamped to [0, 10].
func ComputeWeightedScore(result *GateResult, dimensions []pipeline.DimensionConfig) float64 {
	var total float64
	for _, dim := range dimensions {
		dr, ok := result.Dimensions[dim.Name]
		if !ok {
			continue
		}
		total += clampScore(dr.Score) * dim.Weight
	}
	return total
}

// ApplyThresholds determines the outcome based on score and thresholds.
// score >= pass → PASS, score >= retry → RETRY, else FAIL.
func ApplyThresholds(score float64, thresholds pipeline.ThresholdConfig) model.NodeOutcome {
	if score >= thresholds.Pass {
		return model.OutcomePass
	}
	if score >= thresholds.Retry {
		return model.OutcomeRetry
	}
	return model.OutcomeFail
}

// clampScore restricts a score to the valid [0, 10] range.
func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 10 {
		return 10
	}
	return score
}

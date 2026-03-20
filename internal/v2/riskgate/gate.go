// Package riskgate implements risk evaluation at pipeline role transitions.
// Below threshold: auto-advance. Above threshold: pause for human review (protection placement).
package riskgate

import (
	"encoding/json"
	"time"
)

// RiskScore is the evaluation result for a role's output.
type RiskScore struct {
	Score   float64  `json:"score"`   // 0.0 (safe) to 1.0 (risky)
	Factors []string `json:"factors"` // What contributed to the score
}

// GateDecision is the outcome of a risk gate evaluation.
type GateDecision string

const (
	GateAutoPass    GateDecision = "auto_pass"    // Score below threshold
	GateHumanReview GateDecision = "human_review" // Score at or above threshold
)

// GateConfig holds risk gate settings for a role transition.
type GateConfig struct {
	Threshold float64       `yaml:"threshold" json:"threshold"` // Score >= threshold → human review
	Timeout   time.Duration `yaml:"timeout" json:"timeout"`     // Auto-flare after this
}

// DefaultGateConfig returns a conservative default.
func DefaultGateConfig() GateConfig {
	return GateConfig{
		Threshold: 0.7,
		Timeout:   24 * time.Hour,
	}
}

// Evaluate checks a role's output against the risk threshold.
func Evaluate(config GateConfig, output json.RawMessage) (GateDecision, RiskScore) {
	score := computeRiskScore(output)
	if score.Score >= config.Threshold {
		return GateHumanReview, score
	}
	return GateAutoPass, score
}

// computeRiskScore is a simple heuristic risk scorer for MVP.
// For now: base score on output size as a proxy for complexity.
// A real implementation would use LLM-based risk assessment.
func computeRiskScore(output json.RawMessage) RiskScore {
	if output == nil {
		return RiskScore{Score: 0.5, Factors: []string{"no output"}}
	}

	var factors []string
	score := 0.0

	// Size heuristic: larger outputs suggest more complex changes.
	size := len(output)
	if size > 10000 {
		score += 0.3
		factors = append(factors, "large output (>10KB)")
	} else if size > 5000 {
		score += 0.15
		factors = append(factors, "medium output (>5KB)")
	}

	// Try to parse as object and check for known risk indicators.
	var obj map[string]json.RawMessage
	if json.Unmarshal(output, &obj) == nil {
		if _, hasFilesChanged := obj["files_changed"]; hasFilesChanged {
			var files []string
			if json.Unmarshal(obj["files_changed"], &files) == nil && len(files) > 10 {
				score += 0.3
				factors = append(factors, "many files changed (>10)")
			}
		}
		if _, hasWarnings := obj["warnings"]; hasWarnings {
			score += 0.2
			factors = append(factors, "warnings present")
		}
	}

	// Clamp to [0, 1].
	if score > 1.0 {
		score = 1.0
	}

	if len(factors) == 0 {
		factors = []string{"no risk factors detected"}
	}

	return RiskScore{Score: score, Factors: factors}
}

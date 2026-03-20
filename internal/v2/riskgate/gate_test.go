package riskgate

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvaluate_BelowThreshold(t *testing.T) {
	config := GateConfig{Threshold: 0.7}
	output := json.RawMessage(`{"result":"ok"}`)

	decision, score := Evaluate(config, output)
	assert.Equal(t, GateAutoPass, decision)
	assert.Less(t, score.Score, 0.7)
}

func TestEvaluate_AboveThreshold_ManyFiles(t *testing.T) {
	config := GateConfig{Threshold: 0.3}
	files := make([]string, 15)
	for i := range files {
		files[i] = "file" + string(rune('a'+i)) + ".go"
	}
	data, _ := json.Marshal(map[string]interface{}{
		"files_changed": files,
	})

	decision, score := Evaluate(config, json.RawMessage(data))
	assert.Equal(t, GateHumanReview, decision)
	assert.GreaterOrEqual(t, score.Score, 0.3)
	assert.Contains(t, score.Factors, "many files changed (>10)")
}

func TestEvaluate_ExactThreshold(t *testing.T) {
	// At exact threshold → human review (conservative).
	config := GateConfig{Threshold: 0.0}
	output := json.RawMessage(`{}`)

	decision, _ := Evaluate(config, output)
	assert.Equal(t, GateHumanReview, decision)
}

func TestEvaluate_NilOutput(t *testing.T) {
	config := GateConfig{Threshold: 0.7}
	decision, score := Evaluate(config, nil)
	assert.Equal(t, GateAutoPass, decision)
	assert.Equal(t, 0.5, score.Score)
	assert.Contains(t, score.Factors, "no output")
}

func TestEvaluate_WithWarnings(t *testing.T) {
	config := GateConfig{Threshold: 0.15}
	data := json.RawMessage(`{"warnings":["some issue"]}`)

	decision, score := Evaluate(config, data)
	assert.Equal(t, GateHumanReview, decision)
	assert.Contains(t, score.Factors, "warnings present")
}

func TestDefaultGateConfig(t *testing.T) {
	cfg := DefaultGateConfig()
	assert.Equal(t, 0.7, cfg.Threshold)
	assert.Greater(t, cfg.Timeout.Seconds(), 0.0)
}

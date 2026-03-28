package pipeline

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ParsePipeline parses and validates a PipelineConfig from YAML bytes.
func ParsePipeline(data []byte) (*PipelineConfig, error) {
	var cfg PipelineConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse pipeline: %w", err)
	}
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validate pipeline: %w", err)
	}
	return &cfg, nil
}

// ParsePipelineNoValidate parses a PipelineConfig from YAML bytes without validation.
// Use this for migration tools or when the caller will validate separately.
func ParsePipelineNoValidate(data []byte) (*PipelineConfig, error) {
	var cfg PipelineConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse pipeline: %w", err)
	}
	return &cfg, nil
}

// ParsePipelineFile reads and parses a PipelineConfig from a YAML file.
func ParsePipelineFile(path string) (*PipelineConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pipeline file: %w", err)
	}
	return ParsePipeline(data)
}

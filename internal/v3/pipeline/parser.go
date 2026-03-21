package pipeline

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ParsePipeline parses a PipelineConfig from YAML bytes.
func ParsePipeline(data []byte) (*PipelineConfig, error) {
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

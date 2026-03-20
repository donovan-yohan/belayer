package pipeline

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/donovan-yohan/belayer/internal/v2/role"
)

// ParseRoute parses a Route from YAML bytes.
func ParseRoute(data []byte) (*Route, error) {
	var route Route
	if err := yaml.Unmarshal(data, &route); err != nil {
		return nil, fmt.Errorf("pipeline parse: %w", err)
	}
	if route.Safety == (role.SafetyConfig{}) {
		route.Safety = role.DefaultSafetyConfig()
	}
	return &route, nil
}

// ParseRouteFile reads and parses a Route from a YAML file.
func ParseRouteFile(path string) (*Route, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pipeline file: %w", err)
	}
	return ParseRoute(data)
}

package route

import (
	"encoding/json"
	"fmt"
	"os"
)

// RouteResult is the structured output from a routing decision.
type RouteResult struct {
	Route      string          `json:"route"`
	Confidence float64         `json:"confidence"`
	Reasoning  string          `json:"reasoning"`
	Rejected   []RejectedRoute `json:"rejected"`
}

// RejectedRoute describes a route that was not chosen.
type RejectedRoute struct {
	Route  string `json:"route"`
	Reason string `json:"reason"`
}

// Parse reads and validates a route-result.json file against valid route names.
func Parse(path string, validRoutes []string) (*RouteResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read route result: %w", err)
	}
	return ParseBytes(data, validRoutes)
}

// ParseBytes parses route result JSON bytes and validates against valid route names.
func ParseBytes(data []byte, validRoutes []string) (*RouteResult, error) {
	var result RouteResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse route result: %w", err)
	}
	if err := result.Validate(validRoutes); err != nil {
		return nil, err
	}
	return &result, nil
}

// Validate checks that the chosen route is in the valid set.
func (r *RouteResult) Validate(validRoutes []string) error {
	if r.Route == "" {
		return fmt.Errorf("route result: route field is empty")
	}
	valid := make(map[string]bool, len(validRoutes))
	for _, v := range validRoutes {
		valid[v] = true
	}
	if !valid[r.Route] {
		return fmt.Errorf("route result: chosen route %q is not in valid set %v", r.Route, validRoutes)
	}
	return nil
}

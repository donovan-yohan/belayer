// Package eval implements the eval framework for testing pipeline roles
// against recorded fixtures. `belayer eval <role>` runs a role's provider
// against fixture inputs and compares outputs.
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

// Fixture is a recorded input/output pair for a role.
type Fixture struct {
	Role      string          `json:"role"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
	Timestamp time.Time       `json:"timestamp"`
	RunID     string          `json:"run_id"`
}

// Result is the outcome of evaluating one fixture.
type Result struct {
	FixturePath string `json:"fixture_path"`
	Pass        bool   `json:"pass"`
	Expected    string `json:"expected,omitempty"`
	Actual      string `json:"actual,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Summary is the overall eval result for a role.
type Summary struct {
	Role    string   `json:"role"`
	Total   int      `json:"total"`
	Passed  int      `json:"passed"`
	Failed  int      `json:"failed"`
	Results []Result `json:"results"`
}

// RecordFixture saves a fixture to disk at the standard path.
func RecordFixture(baseDir, roleName, runID string, input, output json.RawMessage) (string, error) {
	dir := filepath.Join(baseDir, "fixtures", roleName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create fixture dir: %w", err)
	}

	fixture := Fixture{
		Role:      roleName,
		Input:     input,
		Output:    output,
		Timestamp: time.Now(),
		RunID:     runID,
	}

	data, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal fixture: %w", err)
	}

	filename := fmt.Sprintf("%s.json", time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write fixture: %w", err)
	}

	return path, nil
}

// LoadFixtures reads all fixtures for a role from the standard path.
func LoadFixtures(baseDir, roleName string) ([]Fixture, error) {
	dir := filepath.Join(baseDir, "fixtures", roleName)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read fixture dir: %w", err)
	}

	var fixtures []Fixture
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var f Fixture
		if err := json.Unmarshal(data, &f); err != nil {
			continue
		}
		fixtures = append(fixtures, f)
	}
	return fixtures, nil
}

// CompareOutputs does a deep JSON comparison of expected and actual outputs.
func CompareOutputs(expected, actual json.RawMessage) bool {
	var exp, act interface{}
	if err := json.Unmarshal(expected, &exp); err != nil {
		return false
	}
	if err := json.Unmarshal(actual, &act); err != nil {
		return false
	}
	return reflect.DeepEqual(exp, act)
}

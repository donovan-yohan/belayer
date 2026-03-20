package eval

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordAndLoadFixture(t *testing.T) {
	dir := t.TempDir()

	input := json.RawMessage(`{"spec":"build auth"}`)
	output := json.RawMessage(`{"files_changed":["auth.go"]}`)

	path, err := RecordFixture(dir, "lead", "run-123", input, output)
	require.NoError(t, err)
	assert.Contains(t, path, "fixtures/lead/")
	assert.Contains(t, path, ".json")

	fixtures, err := LoadFixtures(dir, "lead")
	require.NoError(t, err)
	assert.Len(t, fixtures, 1)
	assert.Equal(t, "lead", fixtures[0].Role)
	assert.Equal(t, "run-123", fixtures[0].RunID)
	assert.JSONEq(t, `{"spec":"build auth"}`, string(fixtures[0].Input))
	assert.JSONEq(t, `{"files_changed":["auth.go"]}`, string(fixtures[0].Output))
}

func TestLoadFixtures_NoFixtures(t *testing.T) {
	dir := t.TempDir()
	fixtures, err := LoadFixtures(dir, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, fixtures)
}

func TestCompareOutputs_Equal(t *testing.T) {
	a := json.RawMessage(`{"a":1,"b":2}`)
	b := json.RawMessage(`{"b":2,"a":1}`) // Same but different key order.
	assert.True(t, CompareOutputs(a, b))
}

func TestCompareOutputs_NotEqual(t *testing.T) {
	a := json.RawMessage(`{"a":1}`)
	b := json.RawMessage(`{"a":2}`)
	assert.False(t, CompareOutputs(a, b))
}

func TestCompareOutputs_InvalidJSON(t *testing.T) {
	assert.False(t, CompareOutputs(json.RawMessage(`not json`), json.RawMessage(`{}`)))
	assert.False(t, CompareOutputs(json.RawMessage(`{}`), json.RawMessage(`not json`)))
}

func TestFixture_JSONRoundTrip(t *testing.T) {
	f := Fixture{
		Role:   "setter",
		Input:  json.RawMessage(`{"desc":"test"}`),
		Output: json.RawMessage(`{"spec":"test spec"}`),
		RunID:  "run-abc",
	}

	data, err := json.Marshal(f)
	require.NoError(t, err)

	var got Fixture
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, f.Role, got.Role)
	assert.Equal(t, f.RunID, got.RunID)
	assert.JSONEq(t, string(f.Input), string(got.Input))
}

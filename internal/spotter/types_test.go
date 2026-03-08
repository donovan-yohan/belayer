package spotter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSpotJSON(t *testing.T) {
	raw := `{"pass": false, "project_type": "frontend", "issues": [{"check": "visual_quality", "description": "Text not wrapping", "severity": "error"}]}`
	var spot SpotJSON
	err := json.Unmarshal([]byte(raw), &spot)
	require.NoError(t, err)
	assert.False(t, spot.Pass)
	assert.Equal(t, "frontend", spot.ProjectType)
	assert.Len(t, spot.Issues, 1)
	assert.Equal(t, "visual_quality", spot.Issues[0].Check)
	assert.Equal(t, "Text not wrapping", spot.Issues[0].Description)
	assert.Equal(t, "error", spot.Issues[0].Severity)
}

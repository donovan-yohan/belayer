package spotter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSpotJSON_Pass(t *testing.T) {
	raw := `{"pass": true, "project_type": "backend", "issues": []}`
	var spot SpotJSON
	err := json.Unmarshal([]byte(raw), &spot)
	require.NoError(t, err)
	assert.True(t, spot.Pass)
	assert.Equal(t, "backend", spot.ProjectType)
	assert.Empty(t, spot.Issues)
}

func TestParseSpotJSON_Fail(t *testing.T) {
	raw := `{"pass": false, "project_type": "frontend", "issues": [{"check": "visual_quality", "description": "Text not wrapping", "severity": "error"}]}`
	var spot SpotJSON
	err := json.Unmarshal([]byte(raw), &spot)
	require.NoError(t, err)
	assert.False(t, spot.Pass)
	assert.Equal(t, "frontend", spot.ProjectType)
	assert.Len(t, spot.Issues, 1)
	assert.Equal(t, "visual_quality", spot.Issues[0].Check)
	assert.Equal(t, "error", spot.Issues[0].Severity)
}

func TestParseSpotJSON_WithScreenshots(t *testing.T) {
	raw := `{"pass": true, "project_type": "frontend", "issues": [], "screenshots": ["home.png", "nav.png"]}`
	var spot SpotJSON
	err := json.Unmarshal([]byte(raw), &spot)
	require.NoError(t, err)
	assert.Len(t, spot.Screenshots, 2)
}

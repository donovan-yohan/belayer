package pipeline

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVisualize_DefaultPipeline(t *testing.T) {
	route, err := ParseRoute([]byte(DefaultPipelineYAML))
	require.NoError(t, err)

	output := Visualize(route, nil)

	// Should contain phase headers.
	assert.Contains(t, output, "APPROACH")
	assert.Contains(t, output, "ASCENT")
	assert.Contains(t, output, "SEND")

	// Should contain role names.
	assert.Contains(t, output, "[setter]")
	assert.Contains(t, output, "[decomposer]")
	assert.Contains(t, output, "[lead]")
	assert.Contains(t, output, "[spotter]")
	assert.Contains(t, output, "[pr-creator]")

	// Should contain loop.
	assert.Contains(t, output, "loop: spotter")

	// Should contain contract types.
	assert.Contains(t, output, "(ascent)")
	assert.Contains(t, output, "(pitch)")
}

func TestVisualize_WithStatus(t *testing.T) {
	route, err := ParseRoute([]byte(DefaultPipelineYAML))
	require.NoError(t, err)

	status := RoleStatus{
		"setter":     MarkerComplete,
		"decomposer": MarkerComplete,
		"lead":       MarkerActive,
	}

	output := Visualize(route, status)
	assert.Contains(t, output, "[setter]✓")
	assert.Contains(t, output, "[decomposer]✓")
	assert.Contains(t, output, "[lead]●")
	assert.Contains(t, output, "[spotter]○") // Pending (default)
}

func TestVisualize_PipelineName(t *testing.T) {
	route, err := ParseRoute([]byte(DefaultPipelineYAML))
	require.NoError(t, err)

	output := Visualize(route, nil)
	assert.True(t, strings.HasPrefix(output, "Pipeline: solo"))
}

func TestVisualize_EmptyRoute(t *testing.T) {
	route := &Route{Name: "empty"}
	output := Visualize(route, nil)
	assert.Contains(t, output, "Pipeline: empty")
}

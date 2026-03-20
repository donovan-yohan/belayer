package pipeline

import (
	"testing"

	"github.com/donovan-yohan/belayer/internal/v2/role"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRoute_DefaultPipeline(t *testing.T) {
	route, err := ParseRoute([]byte(DefaultPipelineYAML))
	require.NoError(t, err)
	assert.Equal(t, "solo", route.Name)
	assert.Len(t, route.Phases, 3)

	// Verify phases.
	assert.Equal(t, role.PhaseApproach, route.Phases[0].Phase)
	assert.Equal(t, role.PhaseAscent, route.Phases[1].Phase)
	assert.Equal(t, role.PhaseSend, route.Phases[2].Phase)

	// Verify roles.
	assert.Len(t, route.Phases[0].Roles, 1)
	assert.Equal(t, "setter", route.Phases[0].Roles[0].Name)
	assert.Equal(t, role.TypeB, route.Phases[0].Roles[0].ContractType)

	assert.Len(t, route.Phases[1].Roles, 3)
	assert.Equal(t, "decomposer", route.Phases[1].Roles[0].Name)
	assert.Equal(t, role.TypeA, route.Phases[1].Roles[0].ContractType)

	// Verify loops.
	assert.Len(t, route.Phases[1].Loops, 1)
	assert.Equal(t, "spotter", route.Phases[1].Loops[0].From)
	assert.Equal(t, "decomposer", route.Phases[1].Loops[0].To)
	assert.Equal(t, 3, route.Phases[1].Loops[0].MaxIterations)
}

func TestParseRoute_InvalidYAML(t *testing.T) {
	_, err := ParseRoute([]byte("{{invalid yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline parse")
}

func TestParseRoute_EmptyYAML(t *testing.T) {
	route, err := ParseRoute([]byte("name: empty\n"))
	require.NoError(t, err)
	assert.Equal(t, "empty", route.Name)
	assert.Len(t, route.Phases, 0)
	// Safety defaults should be applied.
	assert.Equal(t, 2, route.Safety.MaxChildDepth)
}

func TestParseRoute_ExecProvider(t *testing.T) {
	yaml := `
name: custom
phases:
  - phase: ascent
    roles:
      - name: lead
        phase: ascent
        contract_type: ascent
        provider:
          type: exec
          command: my-cursor-lead
          args: ["--model", "gpt-4"]
          config:
            timeout: "30m"
`
	route, err := ParseRoute([]byte(yaml))
	require.NoError(t, err)
	lead := route.Phases[0].Roles[0]
	assert.Equal(t, "exec", lead.Provider.Type)
	assert.Equal(t, "my-cursor-lead", lead.Provider.Command)
	assert.Equal(t, []string{"--model", "gpt-4"}, lead.Provider.Args)
	assert.Equal(t, "30m", lead.Provider.Config["timeout"])
}

func TestParseRoute_EmbeddedTemplates(t *testing.T) {
	// Verify embedded template files are accessible.
	data, err := templateFS.ReadFile("templates/solo.yaml")
	require.NoError(t, err)
	route, err := ParseRoute(data)
	require.NoError(t, err)
	assert.Equal(t, "solo", route.Name)

	data, err = templateFS.ReadFile("templates/team.yaml")
	require.NoError(t, err)
	route, err = ParseRoute(data)
	require.NoError(t, err)
	assert.Equal(t, "team", route.Name)
}

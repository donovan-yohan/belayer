package pipeline

import (
	"testing"

	"github.com/donovan-yohan/belayer/internal/v2/role"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_ValidRoute(t *testing.T) {
	route := testRoute()
	errs := Validate(route)
	assert.Empty(t, errs)
}

func TestValidate_DefaultPipeline(t *testing.T) {
	route, err := ParseRoute([]byte(DefaultPipelineYAML))
	require.NoError(t, err)
	errs := Validate(route)
	assert.Empty(t, errs)
}

func TestValidate_EmptyPhases(t *testing.T) {
	route := &Route{Name: "empty"}
	errs := Validate(route)
	assert.Contains(t, errs, "pipeline must have at least one phase")
}

func TestValidate_EmptyRolesInPhase(t *testing.T) {
	route := &Route{
		Name:   "test",
		Phases: []PhaseConfig{{Phase: role.PhaseAscent}},
		Safety: role.DefaultSafetyConfig(),
	}
	errs := Validate(route)
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "has no roles")
}

func TestValidate_DuplicateRoleName(t *testing.T) {
	route := &Route{
		Name: "test",
		Phases: []PhaseConfig{
			{
				Phase: role.PhaseAscent,
				Roles: []role.RoleDef{
					{Name: "lead", ContractType: role.TypeB},
					{Name: "lead", ContractType: role.TypeA}, // Duplicate
				},
			},
		},
		Safety: role.DefaultSafetyConfig(),
	}
	errs := Validate(route)
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "duplicate role name")
}

func TestValidate_LoopTargetNotInPhase(t *testing.T) {
	route := &Route{
		Name: "test",
		Phases: []PhaseConfig{
			{
				Phase: role.PhaseAscent,
				Roles: []role.RoleDef{
					{Name: "lead", ContractType: role.TypeB},
				},
				Loops: []role.LoopConfig{
					{From: "lead", To: "nonexistent", MaxIterations: 3},
				},
			},
		},
		Safety: role.DefaultSafetyConfig(),
	}
	errs := Validate(route)
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "loop to \"nonexistent\"")
}

func TestValidate_CircularLoop(t *testing.T) {
	route := &Route{
		Name: "test",
		Phases: []PhaseConfig{
			{
				Phase: role.PhaseAscent,
				Roles: []role.RoleDef{
					{Name: "a", ContractType: role.TypeA},
					{Name: "b", ContractType: role.TypeA},
				},
				Loops: []role.LoopConfig{
					{From: "a", To: "b", MaxIterations: 3},
					{From: "b", To: "a", MaxIterations: 3},
				},
			},
		},
		Safety: role.DefaultSafetyConfig(),
	}
	errs := Validate(route)
	found := false
	for _, e := range errs {
		if contains := "circular loop"; len(e) > 0 && e[:len(contains)] == contains || len(e) > len(contains) {
			found = true
		}
	}
	// At least one error should mention circular.
	hasCircular := false
	for _, e := range errs {
		if len(e) > 8 && e[:8] == "circular" {
			hasCircular = true
		}
	}
	_ = found
	assert.True(t, hasCircular, "expected circular loop error, got: %v", errs)
}

func TestValidate_EmbeddedTemplates(t *testing.T) {
	templates := []string{"templates/solo.yaml", "templates/team.yaml"}
	for _, tmpl := range templates {
		t.Run(tmpl, func(t *testing.T) {
			data, err := templateFS.ReadFile(tmpl)
			require.NoError(t, err)
			route, err := ParseRoute(data)
			require.NoError(t, err)
			errs := Validate(route)
			assert.Empty(t, errs, "template %s has validation errors: %v", tmpl, errs)
		})
	}
}

func TestValidateOrError_Valid(t *testing.T) {
	route := testRoute()
	assert.NoError(t, ValidateOrError(route))
}

func TestValidateOrError_Invalid(t *testing.T) {
	route := &Route{Name: "empty"}
	err := ValidateOrError(route)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline validation failed")
}

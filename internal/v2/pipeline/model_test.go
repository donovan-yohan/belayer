package pipeline

import (
	"testing"

	"github.com/donovan-yohan/belayer/internal/v2/role"
	"github.com/stretchr/testify/assert"
)

func testRoute() *Route {
	return &Route{
		Name: "test-route",
		Phases: []PhaseConfig{
			{
				Phase: role.PhaseApproach,
				Roles: []role.RoleDef{
					{Name: "setter", Phase: role.PhaseApproach, ContractType: role.TypeB, Provider: role.ProviderConfig{Type: "builtin"}},
				},
			},
			{
				Phase: role.PhaseAscent,
				Roles: []role.RoleDef{
					{Name: "decomposer", Phase: role.PhaseAscent, ContractType: role.TypeA, Provider: role.ProviderConfig{Type: "builtin"}},
					{Name: "lead", Phase: role.PhaseAscent, ContractType: role.TypeB, Provider: role.ProviderConfig{Type: "builtin"}},
					{Name: "spotter", Phase: role.PhaseAscent, ContractType: role.TypeA, Provider: role.ProviderConfig{Type: "builtin"}},
				},
				Loops: []role.LoopConfig{
					{From: "spotter", To: "decomposer", MaxIterations: 3, Condition: "output.pass == false"},
				},
			},
			{
				Phase: role.PhaseSend,
				Roles: []role.RoleDef{
					{Name: "pr-creator", Phase: role.PhaseSend, ContractType: role.TypeA, Provider: role.ProviderConfig{Type: "builtin"}},
				},
			},
		},
		Safety: role.DefaultSafetyConfig(),
	}
}

func TestRoute_AllRoles(t *testing.T) {
	r := testRoute()
	roles := r.AllRoles()
	assert.Len(t, roles, 5)
	assert.Equal(t, "setter", roles[0].Name)
	assert.Equal(t, "decomposer", roles[1].Name)
	assert.Equal(t, "lead", roles[2].Name)
	assert.Equal(t, "spotter", roles[3].Name)
	assert.Equal(t, "pr-creator", roles[4].Name)
}

func TestRoute_FindRole(t *testing.T) {
	r := testRoute()

	found := r.FindRole("lead")
	assert.NotNil(t, found)
	assert.Equal(t, "lead", found.Name)
	assert.Equal(t, role.TypeB, found.ContractType)

	notFound := r.FindRole("nonexistent")
	assert.Nil(t, notFound)
}

func TestRoute_AllLoops(t *testing.T) {
	r := testRoute()
	loops := r.AllLoops()
	assert.Len(t, loops, 1)
	assert.Equal(t, "spotter", loops[0].From)
	assert.Equal(t, "decomposer", loops[0].To)
	assert.Equal(t, 3, loops[0].MaxIterations)
}

func TestRoute_EmptyRoute(t *testing.T) {
	r := &Route{Name: "empty"}
	assert.Len(t, r.AllRoles(), 0)
	assert.Len(t, r.AllLoops(), 0)
	assert.Nil(t, r.FindRole("anything"))
}

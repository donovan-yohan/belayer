package role

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestContractType_Values(t *testing.T) {
	assert.Equal(t, ContractType("pitch"), TypeA)
	assert.Equal(t, ContractType("ascent"), TypeB)
}

func TestPhase_Values(t *testing.T) {
	assert.Equal(t, Phase("approach"), PhaseApproach)
	assert.Equal(t, Phase("ascent"), PhaseAscent)
	assert.Equal(t, Phase("send"), PhaseSend)
}

func TestDefaultSafetyConfig(t *testing.T) {
	cfg := DefaultSafetyConfig()
	assert.Equal(t, 2, cfg.MaxChildDepth)
	assert.Equal(t, 50, cfg.GlobalChildBudget)
	assert.True(t, cfg.ChildDedupe)
	assert.Equal(t, 24*time.Hour, cfg.GateTimeout)
	assert.Equal(t, 60*time.Second, cfg.HeartbeatInterval)
	assert.Equal(t, 3, cfg.MaxLoopIterations)
}

func TestRoleDef_BuiltinProvider(t *testing.T) {
	r := RoleDef{
		Name:         "decomposer",
		Phase:        PhaseAscent,
		ContractType: TypeA,
		Provider:     ProviderConfig{Type: "builtin"},
	}
	assert.Equal(t, "decomposer", r.Name)
	assert.Equal(t, PhaseAscent, r.Phase)
	assert.Equal(t, TypeA, r.ContractType)
	assert.Equal(t, "builtin", r.Provider.Type)
	assert.Empty(t, r.Provider.Command)
}

func TestRoleDef_ExecProvider(t *testing.T) {
	r := RoleDef{
		Name:         "custom-reviewer",
		Phase:        PhaseSend,
		ContractType: TypeA,
		Provider: ProviderConfig{
			Type:    "exec",
			Command: "my-reviewer",
			Args:    []string{"--strict"},
			Config:  map[string]string{"model": "gpt-4"},
		},
	}
	assert.Equal(t, "exec", r.Provider.Type)
	assert.Equal(t, "my-reviewer", r.Provider.Command)
	assert.Equal(t, []string{"--strict"}, r.Provider.Args)
	assert.Equal(t, "gpt-4", r.Provider.Config["model"])
}

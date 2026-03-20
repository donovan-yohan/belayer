package pipeline

import "embed"

//go:embed templates/*.yaml
var templateFS embed.FS

// DefaultPipelineYAML is the embedded default pipeline (solo template).
const DefaultPipelineYAML = `name: solo
phases:
  - phase: approach
    roles:
      - name: setter
        phase: approach
        contract_type: ascent
        provider:
          type: builtin
  - phase: ascent
    roles:
      - name: decomposer
        phase: ascent
        contract_type: pitch
        provider:
          type: builtin
      - name: lead
        phase: ascent
        contract_type: ascent
        provider:
          type: builtin
      - name: spotter
        phase: ascent
        contract_type: pitch
        provider:
          type: builtin
    loops:
      - from: spotter
        to: decomposer
        max_iterations: 3
  - phase: send
    roles:
      - name: pr-creator
        phase: send
        contract_type: pitch
        provider:
          type: builtin
safety:
  max_child_depth: 2
  global_child_budget: 50
  child_dedupe: true
  max_loop_iterations: 3
`

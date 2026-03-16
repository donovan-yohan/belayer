package belayer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractTestContract_Found(t *testing.T) {
	spec := `# My Spec

## Overview
Some overview text.

## Test Contract
- All endpoints must return 200 on success
- Auth tokens must be validated

## Implementation Notes
Some notes here.
`
	got := extractTestContract(spec)
	assert.Equal(t, "- All endpoints must return 200 on success\n- Auth tokens must be validated", got)
}

func TestExtractTestContract_AtEnd(t *testing.T) {
	spec := `# My Spec

## Overview
Some overview.

## Test Contract
- Tests must cover happy and error paths
- Coverage must be >= 80%`
	got := extractTestContract(spec)
	assert.Equal(t, "- Tests must cover happy and error paths\n- Coverage must be >= 80%", got)
}

func TestExtractTestContract_NotFound(t *testing.T) {
	spec := `# My Spec

## Overview
Some overview text.

## Implementation Notes
Some notes here.
`
	got := extractTestContract(spec)
	assert.Equal(t, "", got)
}

func TestExtractTestContract_Empty(t *testing.T) {
	spec := `# My Spec

## Overview
Some overview.

## Test Contract
## Implementation Notes
Some notes here.
`
	got := extractTestContract(spec)
	assert.Equal(t, "", got)
}

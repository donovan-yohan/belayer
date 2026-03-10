package mail

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSend_RendersTemplateAndSetsLabels(t *testing.T) {
	rendered, err := RenderTemplate(MessageTypeFeedback, "Fix the bug")
	require.NoError(t, err)
	assert.Contains(t, rendered, "FEEDBACK FROM SPOTTER")
	assert.Contains(t, rendered, "Fix the bug")
	assert.Contains(t, rendered, "belayer message setter --type done")
}

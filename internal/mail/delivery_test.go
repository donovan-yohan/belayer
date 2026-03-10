package mail

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeMessage(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"strips ESC", "hello\x1bworld", "helloworld"},
		{"strips CR", "hello\rworld", "helloworld"},
		{"strips BS", "hello\x08world", "helloworld"},
		{"strips DEL", "hello\x7fworld", "helloworld"},
		{"tab to space", "hello\tworld", "hello world"},
		{"preserves newlines", "hello\nworld", "hello\nworld"},
		{"preserves unicode", "hello 🌍 world", "hello 🌍 world"},
		{"preserves quotes", `hello "world" 'foo'`, `hello "world" 'foo'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeMessage(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestChunkMessage(t *testing.T) {
	// Short message: single chunk
	chunks := ChunkMessage("short", 512)
	assert.Len(t, chunks, 1)
	assert.Equal(t, "short", chunks[0])

	// Long message: multiple chunks
	long := make([]byte, 1025)
	for i := range long {
		long[i] = 'x'
	}
	chunks = ChunkMessage(string(long), 512)
	assert.Len(t, chunks, 3) // 512 + 512 + 1
	assert.Len(t, chunks[0], 512)
	assert.Len(t, chunks[1], 512)
	assert.Len(t, chunks[2], 1)
}

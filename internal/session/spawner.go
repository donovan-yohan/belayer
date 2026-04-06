package session

import "context"

// SpawnOpts holds the parameters needed to spawn a session for a pipeline node.
type SpawnOpts struct {
	NodeName      string
	TaskID        string
	Attempt       int
	WorkDir       string
	Description   string
	Command       string
	InputPrompt   string
	CaptureStdout bool   // When true, stdout is teed to a buffer and returned in SpawnResult.
	LogFile       string // When set, stdout and stderr are teed to this file for observability.
}

// SpawnResult carries the outcome of a spawned process.
type SpawnResult struct {
	Error  error  // nil on clean exit (exit code 0)
	Stdout []byte // nil if CaptureStdout was false
	Stderr []byte // always populated
}

// Spawner launches processes for pipeline nodes.
// Returns a channel that receives a SpawnResult when the process exits,
// or nil channel if exit monitoring is not supported.
type Spawner interface {
	Spawn(ctx context.Context, opts SpawnOpts) (<-chan SpawnResult, error)
}

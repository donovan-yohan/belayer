package session

import "context"

// SpawnOpts holds the parameters needed to spawn a session for a pipeline node.
type SpawnOpts struct {
	NodeName    string
	TaskID      string
	Attempt     int
	WorkDir     string
	Description string
	Command     string
	InputPrompt string
}

// Spawner launches processes for pipeline nodes.
// Returns a channel that receives an error if the spawned process exits non-zero,
// or nil channel if exit monitoring is not supported.
type Spawner interface {
	Spawn(ctx context.Context, opts SpawnOpts) (<-chan error, error)
}

package session

import (
	"context"
	"fmt"
)

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

// WindowName returns a display name: "{NodeName}-{TaskID[:8]}".
func (o SpawnOpts) WindowName() string {
	id := o.TaskID
	if len(id) > 8 {
		id = id[:8]
	}
	return fmt.Sprintf("%s-%s", o.NodeName, id)
}

// Spawner launches sessions for pipeline nodes.
// Returns a channel that receives an error if the spawned process exits non-zero
// before writing a completion file. Returns nil channel if exit monitoring is not supported.
type Spawner interface {
	Spawn(ctx context.Context, opts SpawnOpts) (<-chan error, error)
}

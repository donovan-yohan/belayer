package runtime

import "context"

// Provider provisions the dev stack that agents work against.
// Called before sandbox creation — runtime endpoints become allowed
// TCP destinations in the sandbox policy.
type Provider interface {
	// Up starts the dev stack and returns discovered endpoints.
	Up(ctx context.Context) ([]Endpoint, error)

	// Health checks whether the dev stack is ready.
	Health(ctx context.Context) error

	// Down stops the dev stack.
	Down(ctx context.Context) error
}

// Endpoint describes a runtime service available to agents.
type Endpoint struct {
	Name string
	Host string
	Port int
}

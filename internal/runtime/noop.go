package runtime

import "context"

// Noop is a Provider for projects with no dev stack (pure library, CLI tool, etc.).
// All methods are no-ops.
type Noop struct{}

// Up returns nil endpoints and nil error.
func (n *Noop) Up(_ context.Context) ([]Endpoint, error) {
	return nil, nil
}

// Health returns nil.
func (n *Noop) Health(_ context.Context) error {
	return nil
}

// Down returns nil.
func (n *Noop) Down(_ context.Context) error {
	return nil
}

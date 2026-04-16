package runtime

import (
	"context"
	"testing"
)

// Compile-time check: Noop must satisfy Provider.
var _ Provider = (*Noop)(nil)

func TestNoopUp(t *testing.T) {
	n := &Noop{}
	endpoints, err := n.Up(context.Background())
	if err != nil {
		t.Fatalf("Up() returned unexpected error: %v", err)
	}
	if len(endpoints) != 0 {
		t.Fatalf("Up() returned %d endpoints, want 0", len(endpoints))
	}
}

func TestNoopHealth(t *testing.T) {
	n := &Noop{}
	if err := n.Health(context.Background()); err != nil {
		t.Fatalf("Health() returned unexpected error: %v", err)
	}
}

func TestNoopDown(t *testing.T) {
	n := &Noop{}
	if err := n.Down(context.Background()); err != nil {
		t.Fatalf("Down() returned unexpected error: %v", err)
	}
}

package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Compile-time check: Command must satisfy Provider.
var _ Provider = (*Command)(nil)

func TestCommandZeroConfigActsAsNoop(t *testing.T) {
	c := NewCommand(Config{})
	endpoints, err := c.Up(context.Background())
	if err != nil {
		t.Fatalf("Up() returned unexpected error: %v", err)
	}
	if len(endpoints) != 0 {
		t.Fatalf("Up() returned %d endpoints, want 0", len(endpoints))
	}
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health() returned unexpected error: %v", err)
	}
	if err := c.Down(context.Background()); err != nil {
		t.Fatalf("Down() returned unexpected error: %v", err)
	}
}

func TestCommandUpRunsUpCommand(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	c := NewCommand(Config{
		Up: fmt.Sprintf("touch %s", marker),
	})
	if _, err := c.Up(context.Background()); err != nil {
		t.Fatalf("Up() returned unexpected error: %v", err)
	}
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Fatal("Up() did not create marker file")
	}
}

func TestCommandUpReturnsEndpoints(t *testing.T) {
	want := []Endpoint{
		{Name: "api", Host: "localhost", Port: 8080},
	}
	c := NewCommand(Config{
		Up:        "true",
		Endpoints: want,
	})
	got, err := c.Up(context.Background())
	if err != nil {
		t.Fatalf("Up() returned unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("Up() returned %d endpoints, want %d", len(got), len(want))
	}
	if got[0] != want[0] {
		t.Fatalf("Up() returned endpoint %+v, want %+v", got[0], want[0])
	}
}

func TestCommandUpFailsIfUpCommandFails(t *testing.T) {
	c := NewCommand(Config{Up: "exit 1"})
	_, err := c.Up(context.Background())
	if err == nil {
		t.Fatal("Up() expected non-nil error, got nil")
	}
}

func TestCommandUpPollsHealthUntilReady(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	c := NewCommand(Config{
		Up:     fmt.Sprintf("(sleep 0.1 && touch %s) &", marker),
		Health: fmt.Sprintf("test -f %s", marker),
	}).WithHealthInterval(20 * time.Millisecond).WithHealthTimeout(2 * time.Second)

	_, err := c.Up(context.Background())
	if err != nil {
		t.Fatalf("Up() returned unexpected error: %v", err)
	}
}

func TestCommandUpTimesOutIfHealthNeverPasses(t *testing.T) {
	c := NewCommand(Config{
		Up:     "true",
		Health: "false",
	}).WithHealthInterval(20 * time.Millisecond).WithHealthTimeout(100 * time.Millisecond)

	_, err := c.Up(context.Background())
	if err == nil {
		t.Fatal("Up() expected non-nil error, got nil")
	}
	if !strings.Contains(err.Error(), "100ms") {
		t.Fatalf("error message does not mention timeout: %v", err)
	}
}

func TestCommandUpRespectsContextCancellation(t *testing.T) {
	c := NewCommand(Config{
		Up:     "true",
		Health: "false",
	}).WithHealthInterval(20 * time.Millisecond).WithHealthTimeout(10 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	_, err := c.Up(ctx)
	if err == nil {
		t.Fatal("Up() expected non-nil error after cancellation, got nil")
	}
}

func TestCommandHealthRunsHealthCommand(t *testing.T) {
	cOK := NewCommand(Config{Health: "true"})
	if err := cOK.Health(context.Background()); err != nil {
		t.Fatalf("Health() with 'true' returned unexpected error: %v", err)
	}

	cFail := NewCommand(Config{Health: "false"})
	if err := cFail.Health(context.Background()); err == nil {
		t.Fatal("Health() with 'false' expected non-nil error, got nil")
	}
}

func TestCommandDownRunsDownCommand(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	c := NewCommand(Config{
		Down: fmt.Sprintf("touch %s", marker),
	})
	if err := c.Down(context.Background()); err != nil {
		t.Fatalf("Down() returned unexpected error: %v", err)
	}
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Fatal("Down() did not create marker file")
	}
}

func TestCommandDownFailsIfDownCommandFails(t *testing.T) {
	c := NewCommand(Config{Down: "exit 1"})
	if err := c.Down(context.Background()); err == nil {
		t.Fatal("Down() expected non-nil error, got nil")
	}
}

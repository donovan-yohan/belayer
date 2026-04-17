// Package sandbox defines the Driver interface for agent execution boundaries.
// Belayer holds one driver per session. The noop driver (direct exec) is the
// default. Isolation drivers (e.g. clamshell/Docker) implement the same interface.
package sandbox

import (
	"context"
	"io"
)

// Driver manages the agent execution boundary. Belayer holds one driver per session.
type Driver interface {
	// Create prepares an execution environment for the session.
	// Called once per session, before any agents spawn.
	Create(ctx context.Context, cfg Config) (Handle, error)

	// Exec runs a command inside the sandbox. Used for each agent spawn.
	// The caller manages stdin/stdout/stderr wiring via ExecOpts and waits on
	// the returned Process.
	Exec(ctx context.Context, h Handle, cmd []string, opts ExecOpts) (Process, error)

	// Stop tears down the sandbox. Called when the session ends.
	Stop(ctx context.Context, h Handle) error
}

// Process is a running process started by a Driver. Wait must block until the
// process has exited and any stdio pump goroutines have finished, so callers
// can safely close non-*os.File writers (e.g. io.MultiWriter) passed via
// ExecOpts once Wait returns.
type Process interface {
	// Pid returns the underlying OS process id, or 0 if unavailable.
	Pid() int
	// Wait blocks until the process exits and returns the exit error
	// (nil on success, typically *exec.ExitError on non-zero exit).
	Wait() error
	// Kill forcibly terminates the process.
	Kill() error
}

// Config holds the parameters used to create a sandbox environment.
type Config struct {
	Name      string           // sandbox identifier (typically session ID)
	Workspace string           // host path to mount at /workspace
	Policy    string           // path to policy YAML (driver-specific)
	Mounts    []Mount          // additional mounts
	Endpoints []TCPEndpoint    // runtime services to allow through the sandbox
	Providers []ProviderConfig // credential-broker providers to attach (clamshell only)
}

// ExecOpts configures how a command is executed inside the sandbox.
type ExecOpts struct {
	Env    []string  // environment variables
	Dir    string    // working directory inside sandbox
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Mount describes a host path to expose inside the sandbox.
type Mount struct {
	HostPath    string
	SandboxPath string
	ReadOnly    bool
}

// TCPEndpoint describes a runtime service reachable through the sandbox boundary.
type TCPEndpoint struct {
	Name string
	Host string
	Port int
}

// Handle is an opaque reference to a running sandbox environment.
type Handle struct {
	ID   string            // opaque identifier
	Meta map[string]string // driver-specific metadata
}

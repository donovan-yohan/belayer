package sandbox

import "fmt"

// Registry maps driver names to Driver implementations. Drivers register
// themselves via the package-level Register function from their init(), so
// every non-tagged-out driver file contributes exactly one entry to Default.
//
// The daemon resolves a per-session driver by asking Get(mode), where mode
// comes from .belayer/config.yaml's sandbox.mode. A missing registration
// surfaces as a clear error; this is how the clamshell stub turns
// sandbox.mode: clamshell into a useful message when the real driver wasn't
// compiled in.
type Registry struct {
	drivers map[string]Driver
}

// Default is the process-wide registry populated by driver init() functions.
// cmd/belayer hands this to daemon.Config.SandboxDrivers.
var Default = &Registry{drivers: map[string]Driver{}}

// Register installs d under name in the Default registry. Intended to be
// called from init() functions of driver files.
func Register(name string, d Driver) {
	Default.Register(name, d)
}

// Register installs d under name in this registry.
func (r *Registry) Register(name string, d Driver) {
	if r.drivers == nil {
		r.drivers = map[string]Driver{}
	}
	r.drivers[name] = d
}

// Get returns the driver registered for name, or an error naming it when
// nothing is registered. A driver registration with a stub (e.g. the
// clamshell_stub on default builds) is NOT a Get error — Get only fails on
// missing names. The stub reports unavailability from Create/Exec/Stop.
func (r *Registry) Get(name string) (Driver, error) {
	if r == nil || r.drivers == nil {
		return nil, fmt.Errorf("sandbox driver %q is not registered", name)
	}
	d, ok := r.drivers[name]
	if !ok {
		return nil, fmt.Errorf("sandbox driver %q is not registered", name)
	}
	return d, nil
}

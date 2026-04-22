package sandbox

import "fmt"

// Registry maps driver names to Driver implementations. Drivers register
// themselves via the package-level Register function from their init(), so
// every non-tagged-out driver file contributes exactly one entry to Default.
//
// The daemon resolves a per-session driver by asking Get(mode), where mode
// comes from .belayer/config.yaml's sandbox.mode. A missing registration
// surfaces as a clear error. A driver that is not compiled into the current
// binary can opt to register a stub that reports unavailability from
// Create/Exec/Stop instead of being absent from the registry entirely.
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

// Register installs d under name in this registry. Panics on empty name or
// nil driver: Register is only called from driver init() functions, so a bad
// registration is a programmer error that should surface immediately rather
// than as a nil-deref from Get at session-create time.
func (r *Registry) Register(name string, d Driver) {
	if name == "" {
		panic("sandbox.Registry.Register: empty driver name")
	}
	if d == nil {
		panic(fmt.Sprintf("sandbox.Registry.Register: nil driver for %q", name))
	}
	if r.drivers == nil {
		r.drivers = map[string]Driver{}
	}
	r.drivers[name] = d
}

// Get returns the driver registered for name, or an error naming it when
// nothing is registered. A driver registration with a stub is NOT a Get
// error — Get only fails on missing names. Stubs report unavailability from
// Create/Exec/Stop.
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

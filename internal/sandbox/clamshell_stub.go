//go:build !clamshell

package sandbox

import (
	"context"
	"fmt"
)

// The default build registers the clamshell name against a stub driver whose
// operations fail with a clear "not built with -tags clamshell" message. The
// name stays present in the registry so Get("clamshell") succeeds — callers
// find out the real driver is unavailable at Create time, not via a
// "not registered" error that implies a config typo.

func init() {
	Register("clamshell", &unavailableDriver{
		name:   "clamshell",
		reason: "this binary was built without -tags clamshell",
	})
}

// unavailableDriver is a Driver that reports unavailability from every method.
// It exists only in non-tagged builds; a -tags clamshell build replaces it
// with the real driver via clamshell.go's init().
type unavailableDriver struct {
	name   string
	reason string
}

func (u *unavailableDriver) err() error {
	return fmt.Errorf("sandbox driver %q is unavailable: %s", u.name, u.reason)
}

func (u *unavailableDriver) Create(context.Context, Config) (Handle, error) {
	return Handle{}, u.err()
}

func (u *unavailableDriver) Exec(context.Context, Handle, []string, ExecOpts) (Process, error) {
	return nil, u.err()
}

func (u *unavailableDriver) Stop(context.Context, Handle) error {
	return u.err()
}

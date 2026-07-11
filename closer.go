package app

import "context"

// closerComponent runs a cleanup function on stop.
type closerComponent struct {
	name    string
	closeFn func() error
}

// Closer wraps a cleanup function (for example a connection pool's Close) as a
// Component. Start is a no-op; Stop calls the function. It is the named,
// ordered equivalent of an OnStop hook.
func Closer(name string, closeFn func() error) Component {
	return &closerComponent{name: name, closeFn: closeFn}
}

// Name returns the component name.
func (c *closerComponent) Name() string { return c.name }

// Start does nothing; a Closer has no active phase.
func (c *closerComponent) Start(context.Context) error { return nil }

// Stop runs the cleanup function.
func (c *closerComponent) Stop(context.Context) error {
	if c.closeFn == nil {
		return nil
	}
	return c.closeFn()
}

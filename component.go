package app

import "context"

// Component is a unit of the application lifecycle. Start is called once, in
// registration order, when the App runs; Stop is called once, in reverse
// order, during shutdown.
//
// Start must not block: it launches the component's work (for example a server
// in a goroutine) and returns nil once startup has succeeded, or an error if
// startup failed synchronously. A component whose background work fails later
// reports it with Fatal, which triggers a graceful shutdown.
//
// Stop must return promptly, honouring the deadline of the context it receives
// (bounded by the App's shutdown timeout).
type Component interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// State is the lifecycle state of a component as tracked by the App.
type State string

const (
	// StatePending is a registered component that has not started yet.
	StatePending State = "pending"
	// StateStarting is a component whose Start is in progress.
	StateStarting State = "starting"
	// StateRunning is a component that started successfully.
	StateRunning State = "running"
	// StateStopping is a component whose Stop is in progress.
	StateStopping State = "stopping"
	// StateStopped is a component that stopped cleanly.
	StateStopped State = "stopped"
	// StateFailed is a component whose Start or Stop returned an error.
	StateFailed State = "failed"
)

// ComponentStatus is a snapshot of one component's lifecycle state.
type ComponentStatus struct {
	Name  string
	State State
	Err   error
}

// Status is a read-only snapshot of the App's lifecycle. It is the data bridge
// to external tools (for example a health registry): the App itself serves no
// HTTP and knows nothing about "health".
type Status struct {
	Name       string
	Components []ComponentStatus
}

// Healthy reports whether no component is in the failed state.
func (s Status) Healthy() bool {
	for _, c := range s.Components {
		if c.State == StateFailed {
			return false
		}
	}
	return true
}

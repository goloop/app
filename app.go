package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// defaultShutdownTimeout bounds the whole shutdown sequence unless overridden.
const defaultShutdownTimeout = 15 * time.Second

// Hook is a start or stop callback registered with OnStart/OnStop.
type Hook func(context.Context) error

// Option configures an App at construction time.
type Option func(*App)

// WithLogger sets the lifecycle logger. A *slog.Logger satisfies Logger
// directly. A nil logger is ignored.
func WithLogger(l Logger) Option {
	return func(a *App) {
		if l != nil {
			a.log = l
		}
	}
}

// WithShutdownTimeout sets the deadline for the whole shutdown sequence.
// A non-positive value is ignored.
func WithShutdownTimeout(d time.Duration) Option {
	return func(a *App) {
		if d > 0 {
			a.shutdownTimeout = d
		}
	}
}

// WithSignals overrides the signals that trigger shutdown (default: SIGINT and
// SIGTERM). An empty list is ignored.
func WithSignals(sigs ...os.Signal) Option {
	return func(a *App) {
		if len(sigs) > 0 {
			a.signals = sigs
		}
	}
}

// WithForceQuit overrides what happens on a second shutdown signal. The default
// calls os.Exit(130); a custom function lets an app run its own last-resort
// cleanup (and makes the behavior testable). A nil function is ignored.
func WithForceQuit(fn func()) Option {
	return func(a *App) {
		if fn != nil {
			a.forceQuit = fn
		}
	}
}

// App runs a set of explicitly registered components with a controlled
// lifecycle: ordered start, signal-aware wait, and graceful shutdown in
// reverse order. It holds no hidden global state.
type App struct {
	name            string
	log             Logger
	shutdownTimeout time.Duration
	signals         []os.Signal
	forceQuit       func()

	components []Component
	onStart    []Hook
	onStop     []Hook

	running atomic.Bool

	mu     sync.Mutex
	states map[string]ComponentStatus
	order  []string
}

// New creates an App. An empty name defaults to "app".
func New(name string, opts ...Option) *App {
	if name == "" {
		name = "app"
	}
	a := &App{
		name:            name,
		log:             nopLogger{},
		shutdownTimeout: defaultShutdownTimeout,
		signals:         []os.Signal{os.Interrupt, syscall.SIGTERM},
		forceQuit:       func() { os.Exit(130) },
		states:          make(map[string]ComponentStatus),
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Name returns the application name.
func (a *App) Name() string { return a.name }

// Use registers a component. Components start in registration order and stop
// in reverse. A nil component is ignored. Component names should be unique: the
// status map is keyed by name, so a duplicate name overwrites the earlier
// component's reported state. Call Use before Run; it is not safe to call once
// Run has started.
func (a *App) Use(c Component) {
	if c == nil {
		return
	}
	if a.registerTooLate("Use") {
		return
	}
	a.components = append(a.components, c)
	a.setState(c.Name(), StatePending, nil)
}

// OnStart registers a hook run before components start, in registration order.
// Call it before Run; it is not safe to call once Run has started.
func (a *App) OnStart(h Hook) {
	if h == nil || a.registerTooLate("OnStart") {
		return
	}
	a.onStart = append(a.onStart, h)
}

// OnStop registers a cleanup hook run during shutdown, after components stop,
// in reverse registration order. Call it before Run; it is not safe to call
// once Run has started.
func (a *App) OnStop(h Hook) {
	if h == nil || a.registerTooLate("OnStop") {
		return
	}
	a.onStop = append(a.onStop, h)
}

// registerTooLate reports whether registration is happening after Run started.
// It ignores the late call (rather than racing the slices Run reads) and logs a
// warning, so a misuse is loud but never a data race.
func (a *App) registerTooLate(method string) bool {
	if a.running.Load() {
		a.log.Error("registration after Run is ignored",
			"app", a.name, "method", method)
		return true
	}
	return false
}

// fatalCtxKey identifies the fatal-error channel carried in the run context.
type fatalCtxKey struct{}

// Fatal reports an unrecoverable error from a running component, triggering a
// graceful shutdown. It is safe to call from a Start-spawned goroutine and is
// a no-op outside of Run or for a nil error.
func Fatal(ctx context.Context, err error) {
	if err == nil {
		return
	}
	if ch, ok := ctx.Value(fatalCtxKey{}).(chan error); ok {
		select {
		case ch <- err:
		default:
		}
	}
}

// Run starts the components, waits for a shutdown signal, parent cancellation
// or a fatal component error, then shuts everything down gracefully. It returns
// the aggregated error (nil on a clean shutdown from a signal or parent
// cancellation).
//
// An App is single-use: once Run has been called it cannot be run again, and a
// second call returns ErrAlreadyRunning. Register every component and hook
// before calling Run; the registration methods are not safe to call once Run
// has started.
func (a *App) Run(ctx context.Context) error {
	if !a.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}

	sigCtx, stopSig := signal.NotifyContext(ctx, a.signals...)
	defer stopSig()

	base, cancel := context.WithCancel(sigCtx)
	defer cancel()

	fatal := make(chan error, len(a.components)+1)
	runCtx := context.WithValue(base, fatalCtxKey{}, fatal)

	// Start hooks run before any component; a failure aborts the run and then
	// runs every registered stop hook in reverse, so a hook can clean up after
	// a sibling's failure.
	for _, h := range a.onStart {
		if err := h(runCtx); err != nil {
			cancel() // let any resources tied to runCtx unwind before shutdown
			return errors.Join(fmt.Errorf("%s: start hook: %w", a.name, err), a.shutdown(nil))
		}
	}

	// Start components in registration order. A synchronous failure stops the
	// components already started, in reverse.
	started := make([]Component, 0, len(a.components))
	for _, c := range a.components {
		a.setState(c.Name(), StateStarting, nil)
		a.log.Info("starting component", "app", a.name, "component", c.Name())
		if err := c.Start(runCtx); err != nil {
			a.setState(c.Name(), StateFailed, err)
			a.log.Error("component start failed", "app", a.name, "component", c.Name(), "error", err)
			cancel() // cancel runCtx so already-started workers unwind promptly
			return errors.Join(
				fmt.Errorf("%s: start %s: %w", a.name, c.Name(), err),
				a.shutdown(started),
			)
		}
		a.setState(c.Name(), StateRunning, nil)
		started = append(started, c)

		// A component started earlier may already have failed fatally (a worker
		// whose goroutine called Fatal). Stop before starting its dependents.
		select {
		case err := <-fatal:
			cancel()
			a.log.Error("fatal component error during startup", "app", a.name, "error", err)
			return errors.Join(err, a.shutdown(started))
		default:
		}
	}

	a.log.Info("running", "app", a.name, "components", len(started))

	var cause error
	select {
	case <-sigCtx.Done():
		if ctx.Err() != nil {
			a.log.Info("parent context canceled, shutting down", "app", a.name)
		} else {
			a.log.Info("shutdown signal received", "app", a.name)
		}
	case err := <-fatal:
		cause = err
		a.log.Error("fatal component error, shutting down", "app", a.name, "error", err)
		cancel()
	}

	return errors.Join(cause, a.shutdown(started))
}

// shutdown stops the started components in reverse order, then runs the stop
// hooks in reverse order, all bounded by the shutdown timeout. A second signal
// during shutdown forces an immediate exit.
func (a *App) shutdown(started []Component) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout)
	defer cancel()

	stopForce := a.watchForceQuit()
	defer stopForce()

	var errs []error
	for i := len(started) - 1; i >= 0; i-- {
		c := started[i]
		a.setState(c.Name(), StateStopping, nil)
		a.log.Info("stopping component", "app", a.name, "component", c.Name())
		err := stopBounded(stopCtx, c.Stop)
		if err != nil {
			a.setState(c.Name(), StateFailed, err)
			a.log.Error("component stop failed", "app", a.name, "component", c.Name(), "error", err)
			errs = append(errs, fmt.Errorf("%s: stop %s: %w", a.name, c.Name(), err))
			if stopCtx.Err() != nil {
				// The shutdown deadline is spent; the remaining components would
				// all time out. Mark them and stop waiting so Run returns.
				for j := i - 1; j >= 0; j-- {
					a.setState(started[j].Name(), StateFailed, stopCtx.Err())
				}
				break
			}
		} else {
			a.setState(c.Name(), StateStopped, nil)
		}
	}

	for i := len(a.onStop) - 1; i >= 0; i-- {
		if stopCtx.Err() != nil {
			// The deadline is spent: the hook would only time out. Skip it
			// loudly rather than silently running it against a dead context.
			a.log.Error("stop hook skipped: shutdown deadline exceeded",
				"app", a.name)
			errs = append(errs, fmt.Errorf(
				"%s: stop hook skipped: %w", a.name, stopCtx.Err()))
			continue
		}
		if err := stopBounded(stopCtx, a.onStop[i]); err != nil {
			errs = append(errs, fmt.Errorf("%s: stop hook: %w", a.name, err))
		}
	}

	return errors.Join(errs...)
}

// stopBounded runs stop and returns its error, but never blocks past the
// context deadline: if stop ignores ctx and hangs, stopBounded returns ctx.Err()
// once the deadline passes. The stuck goroutine leaks, but shutdown stays
// bounded and the process can exit, honoring the shutdown-timeout contract.
func stopBounded(ctx context.Context, stop func(context.Context) error) error {
	// The deadline is already spent: do not spawn a goroutine that would
	// outlive Run if stop ignores the canceled context. Report it as timed out.
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- stop(ctx) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// watchForceQuit exits the process on a second shutdown signal, so an impatient
// operator is never stuck behind a slow graceful shutdown. The returned func
// stops watching.
func (a *App) watchForceQuit() func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, a.signals...)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			a.log.Error("forced shutdown on second signal", "app", a.name)
			a.forceQuit()
		case <-done:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// setState records a component's lifecycle state for Status.
func (a *App) setState(name string, s State, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.states[name]; !ok {
		a.order = append(a.order, name)
	}
	a.states[name] = ComponentStatus{Name: name, State: s, Err: err}
}

// Status returns a read-only snapshot of the lifecycle state. It is safe to
// call concurrently while the App runs.
func (a *App) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	comps := make([]ComponentStatus, 0, len(a.order))
	for _, n := range a.order {
		comps = append(comps, a.states[n])
	}
	return Status{Name: a.name, Components: comps}
}

# app - reference

`app` is a lifecycle and composition kernel: it owns the start/stop sequence of
a service so your `main` does not repeat it. This document is the full English
reference. For Ukrainian see [DOC.UK.md](DOC.UK.md).

## Contents

- [Model](#model)
- [Construction and options](#construction-and-options)
- [Registering work](#registering-work)
- [Running and shutdown](#running-and-shutdown)
- [Fatal errors](#fatal-errors)
- [Ready-made components](#ready-made-components)
- [Status and health](#status-and-health)
- [Concurrency](#concurrency)
- [Scope](#scope)

## Model

An `App` holds an ordered list of components and two lists of hooks. A component
is anything with a `Name`, a non-blocking `Start` and a `Stop`. The App does
four things and nothing more:

1. install a signal-aware context;
2. run start hooks, then start components in registration order;
3. wait for a signal, parent cancellation, or a fatal component error;
4. stop components in reverse order, then run stop hooks, bounded by a timeout.

There is no global state: everything is a field of the `*App` you create.

## Construction and options

```go
a := app.New("api", opts...)
```

An empty name defaults to `"app"`. Options:

| Option | Effect | Default |
|--------|--------|---------|
| `WithLogger(Logger)` | lifecycle logging | no-op |
| `WithShutdownTimeout(d)` | deadline for the whole shutdown sequence | 15s |
| `WithSignals(sigs...)` | signals that trigger shutdown | `SIGINT`, `SIGTERM` |
| `WithForceQuit(fn)` | action on a second shutdown signal | `os.Exit(130)` |

`Logger` is a two-method subset of `*slog.Logger`, so `slog.Default()` satisfies
it directly:

```go
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}
```

## Registering work

```go
a.Use(component)                 // ordered start, reverse stop
a.OnStart(func(ctx) error {...}) // before components, in order
a.OnStop(func(ctx) error {...})  // during shutdown, in reverse order
```

Start hooks run before any component starts. If a start hook fails, `Run`
aborts, runs the stop hooks (so earlier hooks can clean up) and returns the
error. Stop hooks always run during shutdown, after components have stopped.

## Running and shutdown

```go
err := a.Run(ctx)
```

`Run`:

1. flips a one-shot running flag (`ErrAlreadyRunning` on any later call - an
   App runs once, so register everything before `Run`);
2. wraps `ctx` with `signal.NotifyContext` for the configured signals;
3. runs start hooks;
4. starts components in order; a synchronous `Start` error stops the
   components already started, in reverse, and returns the joined error;
5. waits on one of: the signal/parent context, or a fatal component error;
6. cancels the run context (so workers observe cancellation), stops components
   in reverse order with a fresh timeout context, runs stop hooks, and returns
   the aggregated error (via `errors.Join`). Each `Stop` is bounded by the
   timeout even if it ignores its context, so a stuck component cannot hang the
   shutdown. `HTTPServer` forces connections closed if graceful `Shutdown`
   exceeds the deadline.

A clean, signal-triggered shutdown returns `nil`. A **second** signal during
shutdown forces an immediate process exit (code 130), so a slow graceful stop
never traps an impatient operator.

## Fatal errors

A component whose background work fails after a successful start reports it:

```go
app.Fatal(ctx, fmt.Errorf("consumer: %w", err))
```

`Fatal` sends the error to `Run`, which begins a graceful shutdown and returns
that error joined with any shutdown errors. It is safe to call from a
`Start`-spawned goroutine and is a no-op outside `Run` or for a nil error. The
built-in `HTTPServer` and `Worker` components use it.

## Ready-made components

### HTTPServer

```go
a.Use(app.HTTPServer("http", &http.Server{Addr: ":8080", Handler: mux}))
```

`Start` binds the listener synchronously (so "address already in use" surfaces
immediately as a start error) and serves in a goroutine; a later serve error
triggers a graceful shutdown via `Fatal`. `Stop` calls `srv.Shutdown` with the
shutdown context. An empty `Addr` defaults to `:http`.

### Worker

```go
a.Use(app.Worker("ticker", func(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// periodic work
		}
	}
}))
```

`Start` launches the blocking function with the run context, which is cancelled
when shutdown begins; the function should return when it observes cancellation.
Returning a non-nil error before shutdown triggers a graceful shutdown. `Stop`
waits for the function to return within the shutdown deadline.

### Closer

```go
a.Use(app.Closer("db", pool.Close))
```

`Start` is a no-op; `Stop` calls the function. It is the named, ordered
equivalent of an `OnStop` hook - use it when a resource should be closed at a
specific position in the shutdown order.

## Status and health

The App serves no HTTP and has no concept of "health". It exposes a snapshot:

```go
type Status struct {
	Name       string
	Components []ComponentStatus
}
func (s Status) Healthy() bool // false if any component is StateFailed
```

`Status()` is safe to call concurrently while the App runs. External tooling
turns it into a health check and mounts handlers itself:

```go
registry.Check("app", func(ctx context.Context) error {
	if !a.Status().Healthy() {
		return errors.New("a component failed")
	}
	return nil
})
```

This keeps `app` and any observability module decoupled: neither imports the
other.

## Concurrency

`Run` may be called once at a time; a second concurrent call returns
`ErrAlreadyRunning`. `Status` is guarded by a mutex and safe to read from other
goroutines. Component `Start`/`Stop` are called from the `Run` goroutine only.

## Scope

`app` does: lifecycle, ordered start/stop, graceful shutdown, error
aggregation, lifecycle logging, a status snapshot.

`app` does not: dependency injection, configuration parsing, routing, a health
registry or HTTP handlers, supervision/restart. Those belong to other packages
or to your wiring code.

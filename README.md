[![Go Reference](https://img.shields.io/badge/godoc-reference-blue)](https://pkg.go.dev/github.com/goloop/app) [![License](https://img.shields.io/badge/license-MIT-brightgreen)](https://github.com/goloop/app/blob/master/LICENSE) [![Stay with Ukraine](https://img.shields.io/static/v1?label=Stay%20with&message=Ukraine%20♥&color=ffD700&labelColor=0057B8&style=flat)](https://u24.gov.ua/)

# app

`app` is a small lifecycle and composition kernel for Go services. It runs a set
of explicitly registered components with a controlled lifecycle: ordered start,
a signal-aware wait, and graceful shutdown in reverse order with a bounded
timeout.

It is deliberately **not** a framework: no hidden global state, no
dependency-injection container, no routing, no configuration parsing. Wiring
stays explicit and visible in your `main`. Zero dependencies, standard library
only.

## Install

```bash
go get github.com/goloop/app
```

## Quick start

```go
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	mux := http.NewServeMux()
	// ... register handlers ...

	a := app.New("api",
		app.WithLogger(slog.Default()),
		app.WithShutdownTimeout(10*time.Second),
	)

	a.Use(app.HTTPServer("http", &http.Server{Addr: ":8080", Handler: mux}))
	a.OnStop(func(context.Context) error {
		pool.Close()
		return nil
	})

	return a.Run(context.Background())
}
```

`Run` starts the components in registration order, waits for `SIGINT`/`SIGTERM`
(or parent cancellation, or a fatal component error), then stops the components
in reverse order and runs the stop hooks. It returns nil on a clean,
signal-triggered shutdown. A second signal during shutdown forces an immediate
exit.

## Components

A component has a name, a non-blocking `Start` and a `Stop`:

```go
type Component interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
```

`Start` launches the work (typically a goroutine) and returns once startup has
succeeded, or an error if it failed synchronously. A background failure is
reported with `app.Fatal(ctx, err)`, which triggers a graceful shutdown.

Ready-made components:

| Constructor | Start | Stop |
|-------------|-------|------|
| `HTTPServer(name, *http.Server)` | bind + `Serve` in a goroutine | `srv.Shutdown` |
| `Worker(name, func(ctx) error)` | run the blocking function | wait for it to return |
| `Closer(name, func() error)` | no-op | call the cleanup function |

## Health without coupling

The App serves no HTTP and knows nothing about "health". It exposes `Status()`,
a read-only snapshot of component lifecycle state, as plain data:

```go
st := a.Status()
if !st.Healthy() { /* a component failed */ }
```

An external health registry (for example `goloop/observe`) turns that snapshot
into a check and mounts its own handlers, so `app` stays decoupled.

## Documentation

- English reference: [DOC.md](DOC.md)
- Ukrainian reference: [DOC.UK.md](DOC.UK.md)

## License

MIT - see [LICENSE](LICENSE).

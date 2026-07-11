// Package app is a small lifecycle and composition kernel for Go services.
//
// It runs a set of explicitly registered components with a controlled
// lifecycle: ordered start, a signal-aware wait, and graceful shutdown in
// reverse order with a bounded timeout. It is not a framework: there is no
// hidden global state, no dependency-injection container, no routing and no
// configuration parsing. Wiring stays explicit and visible in your main.
//
// # Components
//
// A Component has a Name, a non-blocking Start and a Stop:
//
//	type Component interface {
//	    Name() string
//	    Start(ctx context.Context) error
//	    Stop(ctx context.Context) error
//	}
//
// Start launches the component's work (typically a goroutine) and returns nil
// once startup succeeds, or an error if it fails synchronously. Background
// failures are reported with Fatal, which triggers a graceful shutdown.
//
// Ready-made components: HTTPServer wraps an *http.Server; Worker wraps a
// blocking, long-running function; Closer wraps a cleanup function.
//
// # Running
//
//	func run() error {
//	    a := app.New("api",
//	        app.WithShutdownTimeout(10*time.Second),
//	    )
//	    a.Use(app.HTTPServer("http", &http.Server{Addr: ":8080", Handler: h}))
//	    a.OnStop(func(context.Context) error { pool.Close(); return nil })
//	    return a.Run(context.Background())
//	}
//
// Run creates a signal-aware context, starts the components in registration
// order, waits for a signal, parent cancellation or a fatal component error,
// then stops the components in reverse order and runs the stop hooks. It
// returns the aggregated error (nil on a clean, signal-triggered shutdown). A
// second signal during shutdown forces an immediate exit.
//
// # Status and health
//
// The App serves no HTTP and knows nothing about "health". It exposes Status,
// a read-only snapshot of component lifecycle state, as plain data. An external
// health registry can turn that snapshot into a check and mount its own
// handlers; the App stays decoupled.
//
// See DOC.md (English) and DOC.UK.md (Ukrainian) for the full reference.
package app

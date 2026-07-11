package app_test

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/goloop/app"
)

// ExampleApp shows a typical service main: register an HTTP server and a
// cleanup hook, then run until a signal arrives.
func ExampleApp() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "hello")
	})

	a := app.New("api",
		app.WithShutdownTimeout(5*time.Second),
	)
	a.Use(app.HTTPServer("http", &http.Server{Addr: ":8080", Handler: mux}))
	a.OnStop(func(context.Context) error {
		// Close database pools, flush buffers, etc.
		return nil
	})

	// In a real program: log.Fatal(a.Run(context.Background()))
	_ = a.Run
	fmt.Println("configured", a.Name())
	// Output: configured api
}

// ExampleWorker registers a background worker that stops when the run context
// is cancelled during shutdown.
func ExampleWorker() {
	a := app.New("worker")
	a.Use(app.Worker("ticker", func(ctx context.Context) error {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
				// do periodic work
			}
		}
	}))
	fmt.Println(a.Name())
	// Output: worker
}

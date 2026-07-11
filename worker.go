package app

import (
	"context"
	"fmt"
	"sync"
)

// workerComponent runs a long-running function as a Component. The App drives it
// from a single goroutine, but Component is exported and may be used directly,
// so the done channel is guarded.
type workerComponent struct {
	name string
	run  func(context.Context) error

	mu   sync.Mutex
	done chan struct{}
}

// Worker wraps a blocking, long-running function as a Component. Start launches
// run in a goroutine with the run context, which is cancelled when shutdown
// begins; run should return when it observes that cancellation. If run returns
// a non-nil error before shutdown, it triggers a graceful shutdown via Fatal.
// Stop waits for run to return within the shutdown context deadline.
func Worker(name string, run func(context.Context) error) Component {
	return &workerComponent{name: name, run: run}
}

// Name returns the component name.
func (w *workerComponent) Name() string { return w.name }

// Start launches the worker in the background.
func (w *workerComponent) Start(ctx context.Context) error {
	if w.run == nil {
		return fmt.Errorf("worker %q: nil run function", w.name)
	}
	w.mu.Lock()
	// Starting twice would overwrite done, so Stop would wait only on the newer
	// goroutine while the first outlived shutdown. Refuse the second start.
	if w.done != nil {
		w.mu.Unlock()
		return fmt.Errorf("worker %q: already started", w.name)
	}
	done := make(chan struct{})
	w.done = done
	w.mu.Unlock()

	go func() {
		defer close(done)
		if err := w.run(ctx); err != nil && ctx.Err() == nil {
			Fatal(ctx, fmt.Errorf("worker %q: %w", w.name, err))
		}
	}()
	return nil
}

// Stop waits for the worker to finish or the shutdown deadline to elapse.
func (w *workerComponent) Stop(ctx context.Context) error {
	w.mu.Lock()
	done := w.done
	w.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

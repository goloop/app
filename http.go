package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
)

// httpComponent adapts an *http.Server to the Component lifecycle. The App
// drives it from a single goroutine, but Component is exported and may be used
// directly, so the lifecycle flags are guarded.
type httpComponent struct {
	name string
	srv  *http.Server

	mu      sync.Mutex
	ln      net.Listener
	started bool
	stopped bool
}

// HTTPServer wraps an *http.Server as a Component. Start binds the listener
// synchronously (so a bind error surfaces immediately) and serves in a
// goroutine; a later serve error triggers a graceful shutdown via Fatal. Stop
// calls srv.Shutdown with the shutdown context.
func HTTPServer(name string, srv *http.Server) Component {
	return &httpComponent{name: name, srv: srv}
}

// Name returns the component name.
func (h *httpComponent) Name() string { return h.name }

// Start binds the address and serves in the background.
func (h *httpComponent) Start(ctx context.Context) error {
	if h.srv == nil {
		return fmt.Errorf("http server %q: nil *http.Server", h.name)
	}

	h.mu.Lock()
	// Starting twice would launch a second Serve and orphan the first listener.
	if h.started {
		h.mu.Unlock()
		return fmt.Errorf("http server %q: already started", h.name)
	}
	// An *http.Server cannot serve again once shut down; refuse rather than
	// appear to start while silently serving nothing.
	if h.stopped {
		h.mu.Unlock()
		return fmt.Errorf("http server %q: already stopped", h.name)
	}
	addr := h.srv.Addr
	if addr == "" {
		addr = ":http"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		h.mu.Unlock()
		return err
	}
	h.ln = ln
	h.started = true
	h.mu.Unlock()

	go func() {
		if err := h.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			Fatal(ctx, fmt.Errorf("http server %q: %w", h.name, err))
		}
	}()
	return nil
}

// Stop gracefully shuts the server down within the context deadline. If the
// deadline passes with connections still open, it forces them closed so the
// process can exit instead of leaking handler goroutines past shutdown.
func (h *httpComponent) Stop(ctx context.Context) error {
	h.mu.Lock()
	h.stopped = true
	h.mu.Unlock()
	if h.srv == nil {
		return nil
	}
	if err := h.srv.Shutdown(ctx); err != nil {
		_ = h.srv.Close()
		return err
	}
	return nil
}

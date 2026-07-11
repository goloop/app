package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

// httpComponent adapts an *http.Server to the Component lifecycle.
type httpComponent struct {
	name    string
	srv     *http.Server
	ln      net.Listener
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
	// An *http.Server cannot serve again once shut down; refuse rather than
	// appear to start while silently serving nothing.
	if h.stopped {
		return fmt.Errorf("http server %q: already stopped", h.name)
	}
	addr := h.srv.Addr
	if addr == "" {
		addr = ":http"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	h.ln = ln

	go func() {
		if err := h.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			Fatal(ctx, fmt.Errorf("http server %q: %w", h.name, err))
		}
	}()
	return nil
}

// Stop gracefully shuts the server down within the context deadline.
func (h *httpComponent) Stop(ctx context.Context) error {
	h.stopped = true
	if h.srv == nil {
		return nil
	}
	return h.srv.Shutdown(ctx)
}

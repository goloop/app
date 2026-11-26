package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPServerUsesTLSConfig(t *testing.T) {
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	defer fixture.Close()
	server := &http.Server{
		Addr:      "127.0.0.1:0",
		TLSConfig: fixture.TLS.Clone(),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.TLS == nil {
				t.Error("request arrived in clear text")
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	service := HTTPServer("tls", server).(*httpComponent)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Stop(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+service.ln.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := fixture.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestWorkerReportsDrainError(t *testing.T) {
	want := errors.New("could not drain")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := Worker("queue", func(ctx context.Context) error {
		<-ctx.Done()
		return want
	})
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := service.Stop(stopCtx); !errors.Is(err, want) {
		t.Fatalf("Stop returned %v, want %v", err, want)
	}
}

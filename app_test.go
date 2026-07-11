package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

// recorder collects start/stop events across components in order.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(e string) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

// fakeComp is a controllable Component for tests.
type fakeComp struct {
	name     string
	rec      *recorder
	startErr error
	stopErr  error
}

func (f *fakeComp) Name() string { return f.name }

func (f *fakeComp) Start(context.Context) error {
	f.rec.add("start:" + f.name)
	return f.startErr
}

func (f *fakeComp) Stop(context.Context) error {
	f.rec.add("stop:" + f.name)
	return f.stopErr
}

func cancelAfter(d time.Duration) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(d)
		cancel()
	}()
	return ctx
}

func TestRunStartStopOrder(t *testing.T) {
	rec := &recorder{}
	a := New("t")
	a.Use(&fakeComp{name: "a", rec: rec})
	a.Use(&fakeComp{name: "b", rec: rec})
	a.Use(&fakeComp{name: "c", rec: rec})

	if err := a.Run(cancelAfter(20 * time.Millisecond)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := rec.snapshot()
	want := []string{
		"start:a", "start:b", "start:c",
		"stop:c", "stop:b", "stop:a",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("order:\n got %v\nwant %v", got, want)
	}
}

func TestStopHookOrder(t *testing.T) {
	rec := &recorder{}
	a := New("t")
	a.OnStop(func(context.Context) error { rec.add("hook1"); return nil })
	a.OnStop(func(context.Context) error { rec.add("hook2"); return nil })

	if err := a.Run(cancelAfter(10 * time.Millisecond)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := rec.snapshot()
	want := []string{"hook2", "hook1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("hooks: got %v want %v", got, want)
	}
}

func TestStartFailureStopsStarted(t *testing.T) {
	rec := &recorder{}
	boom := errors.New("boom")
	a := New("t")
	a.Use(&fakeComp{name: "a", rec: rec})
	a.Use(&fakeComp{name: "b", rec: rec, startErr: boom})
	a.Use(&fakeComp{name: "c", rec: rec})

	err := a.Run(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
	got := rec.snapshot()
	// a starts, b fails to start, c never starts; a is stopped.
	want := []string{"start:a", "start:b", "stop:a"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestFatalTriggersShutdown(t *testing.T) {
	rec := &recorder{}
	boom := errors.New("worker boom")
	a := New("t")
	a.Use(&fakeComp{name: "a", rec: rec})
	a.Use(Worker("bad", func(context.Context) error { return boom }))

	err := a.Run(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("expected worker boom, got %v", err)
	}
	// a must have been stopped during the fatal shutdown.
	found := false
	for _, e := range rec.snapshot() {
		if e == "stop:a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("component a was not stopped on fatal: %v", rec.snapshot())
	}
}

func TestContextCancelIsCleanShutdown(t *testing.T) {
	a := New("t")
	a.Use(&fakeComp{name: "a", rec: &recorder{}})
	if err := a.Run(cancelAfter(10 * time.Millisecond)); err != nil {
		t.Fatalf("expected nil on cancel, got %v", err)
	}
}

func TestStopErrorAggregated(t *testing.T) {
	rec := &recorder{}
	se := errors.New("stop failed")
	a := New("t")
	a.Use(&fakeComp{name: "a", rec: rec, stopErr: se})
	err := a.Run(cancelAfter(10 * time.Millisecond))
	if !errors.Is(err, se) {
		t.Fatalf("expected stop error, got %v", err)
	}
}

func TestAlreadyRunning(t *testing.T) {
	a := New("t")
	a.Use(Worker("block", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- a.Run(ctx) }()

	// Give the first Run a moment to flip the running flag.
	time.Sleep(20 * time.Millisecond)
	if err := a.Run(context.Background()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("first Run: %v", err)
	}
}

func TestStatusSnapshot(t *testing.T) {
	rec := &recorder{}
	a := New("t")
	a.Use(&fakeComp{name: "a", rec: rec})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- a.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)

	st := a.Status()
	if st.Name != "t" || len(st.Components) != 1 {
		t.Fatalf("unexpected status: %+v", st)
	}
	if st.Components[0].State != StateRunning {
		t.Fatalf("expected running, got %s", st.Components[0].State)
	}
	if !st.Healthy() {
		t.Fatalf("expected healthy")
	}
	cancel()
	<-errCh
}

func TestHTTPServerComponent(t *testing.T) {
	srv := &http.Server{
		Addr: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ok")
		}),
	}
	c := HTTPServer("api", srv).(*httpComponent)

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	addr := c.ln.Addr().String()

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestNilComponentsReturnStartError(t *testing.T) {
	if err := HTTPServer("h", nil).Start(context.Background()); err == nil {
		t.Fatal("expected error for nil *http.Server")
	}
	if err := Worker("w", nil).Start(context.Background()); err == nil {
		t.Fatal("expected error for nil run function")
	}
}

func TestStartFailureCancelsStartedWorker(t *testing.T) {
	// A worker that blocks until its run context is cancelled, followed by a
	// component that fails to start. The abort path must cancel the run context
	// so the worker unwinds well before the shutdown timeout.
	boom := errors.New("boom")
	a := New("t", WithShutdownTimeout(5*time.Second))
	a.Use(Worker("blocker", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}))
	a.Use(&fakeComp{name: "bad", rec: &recorder{}, startErr: boom})

	start := time.Now()
	err := a.Run(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("shutdown took %v, run context was not cancelled on abort", elapsed)
	}
}

func TestHTTPServerBindError(t *testing.T) {
	// Occupy a port, then a second server on the same address must fail to
	// start synchronously.
	first := &http.Server{Addr: "127.0.0.1:0"}
	c1 := HTTPServer("first", first).(*httpComponent)
	if err := c1.Start(context.Background()); err != nil {
		t.Fatalf("first start: %v", err)
	}
	defer c1.Stop(context.Background())

	addr := c1.ln.Addr().String()
	second := &http.Server{Addr: addr}
	c2 := HTTPServer("second", second)
	if err := c2.Start(context.Background()); err == nil {
		t.Fatalf("expected bind error on %s", addr)
	}
}

package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type component struct {
	start func(context.Context) error
	stop  func(context.Context) error
	done  chan error
}

func (c *component) Start(ctx context.Context) error { return c.start(ctx) }
func (c *component) Wait() error                     { return <-c.done }
func (c *component) Stop(ctx context.Context) error  { return c.stop(ctx) }

func TestOrderingAndCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var events []string
	add := func(event string) { mu.Lock(); defer mu.Unlock(); events = append(events, event) }
	ready := make(chan struct{})
	a := &component{done: make(chan error, 1)}
	b := &component{done: make(chan error, 1)}
	a.start = func(context.Context) error { add("startA"); return nil }
	b.start = func(context.Context) error { add("startB"); close(ready); return nil }
	a.stop = func(ctx context.Context) error { add("stopA"); a.done <- nil; return ctx.Err() }
	b.stop = func(ctx context.Context) error { add("stopB"); b.done <- nil; return ctx.Err() }
	app := New(Options{Cleanup: func() error { add("cleanup"); return nil }}, a, b)
	result := make(chan error, 1)
	go func() { result <- app.Run(ctx) }()
	<-ready
	if err := app.Run(context.Background()); !errors.Is(err, ErrAlreadyRun) {
		t.Fatalf("concurrent run = %v", err)
	}
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"startA", "startB", "stopB", "stopA", "cleanup"}) {
		t.Fatalf("events = %v", events)
	}
	if err := app.Run(context.Background()); !errors.Is(err, ErrAlreadyRun) {
		t.Fatalf("repeated run = %v", err)
	}
}

func TestStartupRollbackAndErrors(t *testing.T) {
	startErr, stopErr, cleanupErr := errors.New("start"), errors.New("stop"), errors.New("cleanup")
	var events []string
	a := &component{done: make(chan error, 1)}
	b := &component{done: make(chan error, 1)}
	a.start = func(context.Context) error { events = append(events, "startA"); return nil }
	b.start = func(context.Context) error { events = append(events, "startB"); return startErr }
	a.stop = func(context.Context) error { events = append(events, "stopA"); a.done <- nil; return nil }
	b.stop = func(context.Context) error { events = append(events, "stopB"); return stopErr }
	app := New(Options{Cleanup: func() error { events = append(events, "cleanup"); return cleanupErr }}, a, b)
	err := app.Run(context.Background())
	for _, cause := range []error{startErr, stopErr, cleanupErr} {
		if !errors.Is(err, cause) {
			t.Fatalf("lost %v in %v", cause, err)
		}
	}
	if !reflect.DeepEqual(events, []string{"startA", "startB", "stopB", "stopA", "cleanup"}) {
		t.Fatalf("rollback events = %v", events)
	}
}

func TestRuntimeExitCancelsStartup(t *testing.T) {
	runtimeErr := errors.New("execution failed")
	a := &component{done: make(chan error, 1)}
	b := &component{done: make(chan error, 1)}
	a.start = func(context.Context) error { a.done <- runtimeErr; return nil }
	a.stop = func(context.Context) error { return nil }
	b.start = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	b.stop = func(context.Context) error { return nil }
	err := New(Options{}, a, b).Run(context.Background())
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("runtime error = %v", err)
	}
}

func TestShutdownDeadlineAndCleanup(t *testing.T) {
	runtimeErr := errors.New("runtime")
	c := &component{done: make(chan error, 1)}
	c.start = func(context.Context) error { c.done <- runtimeErr; return nil }
	c.stop = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	cleaned := false
	err := New(Options{ShutdownTimeout: 20 * time.Millisecond, Cleanup: func() error { cleaned = true; return nil }}, c).Run(context.Background())
	if !cleaned || !errors.Is(err, runtimeErr) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline = %v, cleaned=%v", err, cleaned)
	}
}

func TestRuntimeErrorAfterStartup(t *testing.T) {
	runtimeErr := errors.New("execution")
	ready := make(chan struct{})
	c := &component{done: make(chan error, 1)}
	c.start = func(context.Context) error { close(ready); return nil }
	c.stop = func(context.Context) error { return nil }
	result := make(chan error, 1)
	go func() { result <- New(Options{}, c).Run(context.Background()) }()
	<-ready
	c.done <- runtimeErr
	if err := <-result; !errors.Is(err, runtimeErr) {
		t.Fatalf("execution failure = %v", err)
	}
}

func TestWaitDeadlineAndCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	c := &component{done: make(chan error, 1)}
	c.start = func(context.Context) error { close(ready); return nil }
	c.stop = func(context.Context) error { return nil }
	// Release the deliberately unresponsive Wait goroutine after the test.
	defer func() { c.done <- nil }()
	cleaned := false
	result := make(chan error, 1)
	go func() {
		result <- New(Options{ShutdownTimeout: 20 * time.Millisecond, Cleanup: func() error { cleaned = true; return nil }}, c).Run(ctx)
	}()
	<-ready
	cancel()
	if err := <-result; !errors.Is(err, context.DeadlineExceeded) || !cleaned {
		t.Fatalf("waiting deadline = %v, cleaned=%v", err, cleaned)
	}
}

func TestCancelledBeforeStartAndInvalidOptions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		timeout time.Duration
		wantErr bool
	}{{"cancelled", ctx, 0, false}, {"nil context", nil, 0, true}, {"negative timeout", ctx, -1, true}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			err := New(Options{ShutdownTimeout: tc.timeout, Cleanup: func() error { calls++; return nil }}).Run(tc.ctx)
			if (err != nil) != tc.wantErr || calls != 1 {
				t.Fatalf("error=%v cleanup calls=%d", err, calls)
			}
		})
	}
}

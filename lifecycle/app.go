// Package lifecycle coordinates application components and resource cleanup.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// Component represents a server or background worker. Start returns when ready;
// Wait blocks until execution ends and returns nil for normal shutdown. Stop
// must respect its context and release partially started resources even if
// Start failed. Stop must also allow Wait to finish.
type Component interface {
	Start(context.Context) error
	Wait() error
	Stop(context.Context) error
}

type Options struct {
	// ShutdownTimeout bounds the shared shutdown phase; zero defaults to 10s.
	ShutdownTimeout time.Duration
	// Cleanup is typically the callback returned by a generated initializer.
	// It runs after shutdown, including on startup failure or cancellation.
	Cleanup func() error
}

var ErrAlreadyRun = errors.New("lifecycle: application has already run")

// App owns the lifecycle of its components and is run once.
type App struct {
	options    Options
	components []Component
	ran        atomic.Bool
}

func New(options Options, components ...Component) *App {
	if options.ShutdownTimeout == 0 {
		options.ShutdownTimeout = 10 * time.Second
	}
	return &App{options: options, components: append([]Component(nil), components...)}
}

type result struct {
	index int
	err   error
}

// Run starts components in order. Cancellation or completion of any component
// stops the application. Shutdown uses a fresh context with a shared timeout,
// stops components in reverse order, waits for their exit, then runs Cleanup.
// Ordinary caller cancellation is a successful shutdown. Component, shutdown
// and cleanup errors are preserved with errors.Join.
func (a *App) Run(ctx context.Context) (err error) {
	if !a.ran.CompareAndSwap(false, true) {
		return ErrAlreadyRun
	}
	defer func() {
		if a.options.Cleanup != nil {
			if cleanupErr := a.options.Cleanup(); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("lifecycle: cleanup: %w", cleanupErr))
			}
		}
	}()
	if ctx == nil || a.options.ShutdownTimeout < 0 {
		return fmt.Errorf("lifecycle: context must not be nil and shutdown timeout must not be negative")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := make(chan result, len(a.components))
	attempted, running := 0, 0
	for index, component := range a.components {
		if runCtx.Err() != nil {
			break
		}
		if component == nil {
			err = fmt.Errorf("lifecycle: component %d is nil", index)
			break
		}
		attempted++
		if startErr := component.Start(runCtx); startErr != nil {
			if startErr != runCtx.Err() {
				err = fmt.Errorf("lifecycle: start component %d: %w", index, startErr)
			}
			break
		}
		running++
		go func(index int, component Component) {
			events <- result{index: index, err: component.Wait()}
			cancel()
		}(index, component)
	}
	if err == nil && runCtx.Err() == nil {
		<-runCtx.Done()
	}
	cancel()
	shutdown, stop := context.WithTimeout(context.Background(), a.options.ShutdownTimeout)
	defer stop()
	for index := attempted - 1; index >= 0; index-- {
		if stopErr := a.components[index].Stop(shutdown); stopErr != nil {
			err = errors.Join(err, fmt.Errorf("lifecycle: stop component %d: %w", index, stopErr))
		}
	}
	for running > 0 {
		// Prefer already available execution errors even when the deadline expired.
		select {
		case event := <-events:
			if event.err != nil {
				err = errors.Join(err, fmt.Errorf("lifecycle: run component %d: %w", event.index, event.err))
			}
			running--
			continue
		default:
		}
		select {
		case event := <-events:
			if event.err != nil {
				err = errors.Join(err, fmt.Errorf("lifecycle: run component %d: %w", event.index, event.err))
			}
			running--
		case <-shutdown.Done():
			return errors.Join(err, fmt.Errorf("lifecycle: waiting for components to exit: %w", shutdown.Err()))
		}
	}
	return err
}

// RunSignals runs until a component exits, SIGINT or SIGTERM is received.
func (a *App) RunSignals() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return a.Run(ctx)
}

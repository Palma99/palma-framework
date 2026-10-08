// Package httpserver connects a standard HTTP server to the application lifecycle.
// Any router that implements http.Handler can be used as the server's handler.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	pfw "github.com/palma99/palma-framework"
	"github.com/palma99/palma-framework/logging"
)

type Server struct {
	server      *http.Server
	mu          sync.Mutex
	listener    net.Listener
	started     bool
	stopped     bool
	done        chan struct{}
	serveErr    error
	stopOnce    sync.Once
	stopErr     error
	logger      logging.Logger
	environment pfw.Environment
}

func New(server *http.Server) *Server { return NewWithLogger(server, logging.NewDefault()) }

// NewWithLogger is the default DI provider for initializers without an environment.
func NewWithLogger(server *http.Server, logger logging.Logger) *Server {
	return &Server{server: server, logger: logger, done: make(chan struct{})}
}

// NewForEnvironment is the default DI provider for environment-aware initializers.
func NewForEnvironment(server *http.Server, logger logging.Logger, env pfw.Environment) *Server {
	s := NewWithLogger(server, logger)
	s.environment = env
	return s
}

// Logger returns the logger injected into this lifecycle component.
func (s *Server) Logger() logging.Logger { return s.logger }

// HTTPServer returns the underlying application's HTTP server.
func (s *Server) HTTPServer() *http.Server { return s.server }

// Start binds the listener before returning, so bind failures are startup errors.
func (s *Server) Start(ctx context.Context) error {
	if err := s.start(ctx); err != nil {
		return err
	}
	fields := []any{"address", s.Addr().String()}
	if s.environment != "" {
		fields = append(fields, "environment", s.environment)
	}
	s.logger.Info(ctx, "server started", fields...)
	return nil
}

func (s *Server) start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil || s.logger == nil || s.started || s.stopped {
		return fmt.Errorf("httpserver: server is nil or has already started/stopped")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	address := s.server.Addr
	if address == "" {
		address = ":http"
	}
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", address)
	if err != nil {
		return err
	}
	s.listener, s.started = listener, true
	go func() {
		err := s.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		s.mu.Lock()
		s.serveErr = err
		s.mu.Unlock()
		close(s.done)
	}()

	return nil
}

func (s *Server) Wait() error {
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if !started {
		return fmt.Errorf("httpserver: server has not started")
	}
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serveErr
}

// Stop drains requests and forcibly closes connections if its deadline expires.
func (s *Server) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopped = true
		started := s.started
		s.mu.Unlock()
		if started {
			s.stopErr = s.server.Shutdown(ctx)
			if s.stopErr != nil {
				s.stopErr = errors.Join(s.stopErr, s.server.Close())
			}
		}
	})
	return s.stopErr
}

// Addr returns the bound address after Start, useful when binding to port zero.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

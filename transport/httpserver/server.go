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
)

type Server struct {
	server   *http.Server
	mu       sync.Mutex
	listener net.Listener
	started  bool
	stopped  bool
	done     chan struct{}
	serveErr error
	stopOnce sync.Once
	stopErr  error
}

func New(server *http.Server) *Server { return &Server{server: server, done: make(chan struct{})} }

// Start binds the listener before returning, so bind failures are startup errors.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil || s.started || s.stopped {
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

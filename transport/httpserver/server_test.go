package httpserver

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/palma99/palma-framework/lifecycle"
)

func TestLifecycleHTTP(t *testing.T) {
	requests := make(chan struct{}, 1)
	router := http.NewServeMux()
	router.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { requests <- struct{}{}; io.WriteString(w, "ok") })
	server := New(&http.Server{Addr: "127.0.0.1:0", Handler: router, ReadHeaderTimeout: time.Second})
	// Use the actual lifecycle, including cancellation and final resource cleanup.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := &readyServer{Server: server, ready: make(chan struct{})}
	cleaned := false
	result := make(chan error, 1)
	go func() {
		result <- lifecycle.New(lifecycle.Options{Cleanup: func() error { cleaned = true; return nil }}, ready).Run(ctx)
	}()
	<-ready.ready
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	client.CloseIdleConnections()
	if err != nil || string(body) != "ok" {
		t.Fatalf("response = %q %v", body, err)
	}
	<-requests
	cancel()
	if err := <-result; err != nil || !cleaned {
		t.Fatalf("shutdown = %v, cleanup=%v", err, cleaned)
	}
	if err := server.Wait(); err != nil {
		t.Fatal(err)
	}
}

type readyServer struct {
	*Server
	ready chan struct{}
}

func (s *readyServer) Start(ctx context.Context) error {
	err := s.Server.Start(ctx)
	close(s.ready)
	return err
}

func TestBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := New(&http.Server{Addr: listener.Addr().String()})
	if err := server.Start(context.Background()); err == nil {
		t.Fatal("expected occupied port error")
	}
	if err := server.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownTimeoutClosesConnections(t *testing.T) {
	entered := make(chan struct{})
	released := make(chan struct{})
	server := New(&http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(released)
	})})
	if err := server.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.server.Close() })
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client := &http.Client{Timeout: time.Second}
		defer client.CloseIdleConnections()
		if response, err := client.Get("http://" + server.Addr().String()); err == nil {
			response.Body.Close()
		}
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := server.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop = %v", err)
	}
	<-released
	<-clientDone
	if err := server.Wait(); err != nil {
		t.Fatal(err)
	}
}

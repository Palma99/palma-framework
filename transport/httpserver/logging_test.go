package httpserver_test

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/palma99/palma-framework/logging"
	"github.com/palma99/palma-framework/transport/httpserver"
)

func TestReadinessUsesInjectedLoggerAfterBinding(t *testing.T) {
	var output bytes.Buffer
	logger := logging.New(logging.Options{Output: &output})
	httpServer := &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })}
	server := httpserver.NewForEnvironment(httpServer, logger, "uat")
	if server.Logger() != logger || server.HTTPServer() != httpServer || output.Len() != 0 {
		t.Fatal("constructor changed dependencies or logged readiness")
	}
	if err := server.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Stop(ctx); err != nil {
			t.Error(err)
		}
		if err := server.Wait(); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(output.String(), `msg="server started" address=`+server.Addr().String()+" environment=uat") {
		t.Fatalf("readiness: %s", output.String())
	}
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("response: %d", response.StatusCode)
	}
}

func TestFailedBindDoesNotLogReadiness(t *testing.T) {
	var output bytes.Buffer
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	server := httpserver.NewWithLogger(&http.Server{Addr: occupied.Addr().String()}, logging.New(logging.Options{Output: &output}))
	if err := server.Start(context.Background()); err == nil {
		t.Fatal("occupied port accepted")
	}
	if output.Len() != 0 {
		t.Fatalf("failed start logged readiness: %s", output.String())
	}
}

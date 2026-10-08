package http_test

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pfwhttp "github.com/palma99/palma-framework/http"
)

type hijackWriter struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (w hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

func TestHandlerAdapterDoesNotWriteAfterHijack(t *testing.T) {
	conn, peer := net.Pipe()
	defer peer.Close()
	w := hijackWriter{httptest.NewRecorder(), conn}
	adapter := pfwhttp.NewHandlerAdapter(nil, nil)
	adapter.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		return errors.New("late error")
	}).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Body.Len() != 0 {
		t.Fatalf("wrote after hijack: %s", w.Body)
	}
}

func TestHandlerAdapterMapsErrorsAndPreservesCommittedResponses(t *testing.T) {
	missing := errors.New("private missing")
	adapter := pfwhttp.NewHandlerAdapter(pfwhttp.NewMapper(pfwhttp.Is(missing, pfwhttp.NewError(404, "not_found", "item not found"))), nil)
	for _, tc := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request) error
		status  int
		body    string
	}{
		{"application", func(http.ResponseWriter, *http.Request) error { return missing }, 404, "not_found"},
		{"typed", func(http.ResponseWriter, *http.Request) error {
			return pfwhttp.NewError(403, "forbidden", "access denied")
		}, 403, "forbidden"},
		{"encoding", func(w http.ResponseWriter, r *http.Request) error { return pfwhttp.OK(make(chan int)).Write(w) }, 500, "internal_error"},
		{"committed", func(w http.ResponseWriter, r *http.Request) error {
			_, _ = w.Write([]byte("already sent"))
			return missing
		}, 200, "already sent"},
		{"flushed", func(w http.ResponseWriter, r *http.Request) error {
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Fatal(err)
			}
			return missing
		}, 200, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			adapter.Wrap(tc.handler).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) {
				t.Fatalf("response: %d %s", w.Code, w.Body)
			}
			if (tc.name == "committed" || tc.name == "flushed") && strings.Contains(w.Body.String(), "not_found") {
				t.Fatal("overwrote committed response")
			}
		})
	}
}

func TestHandlerAdapterComposesWithNativeMiddlewareAndPathValues(t *testing.T) {
	adapter := pfwhttp.NewHandlerAdapter(nil, nil)
	called := false
	endpoint := adapter.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return pfwhttp.OK(r.PathValue("id")).Write(w)
	})
	protect := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(401)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/items/{id}", protect(endpoint))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/items/42", nil)
	mux.ServeHTTP(w, r)
	if w.Code != 401 || called {
		t.Fatalf("unauthorized: %d called=%v", w.Code, called)
	}
	r.Header.Set("Authorization", "test")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !called || strings.TrimSpace(w.Body.String()) != "\"42\"" {
		t.Fatalf("authorized: %d %s", w.Code, w.Body)
	}
}

package bootstrap_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/palma99/palma-framework/examples/httpapi/internal/bootstrap"
	"github.com/palma99/palma-framework/examples/httpapi/internal/config"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/domain"
)

func server(t *testing.T) *http.Server {
	t.Helper()
	s, err := bootstrap.Initialize(config.Config{HTTPAddress: "127.0.0.1:0", SeedUserName: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Handler.(*echo.Echo); !ok {
		t.Fatalf("expected Echo, got %T", s.Handler)
	}
	return s
}

func request(s *http.Server, method, path, body, mediaType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if mediaType != "" {
		r.Header.Set("Content-Type", mediaType)
	}
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, r)
	return w
}

func TestCreateGetAndList(t *testing.T) {
	s := server(t)
	created := request(s, http.MethodPost, "/users", `{"name":"  Grace  "}`, "application/json; charset=utf-8")
	var user struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	if created.Code != http.StatusCreated || user.ID == "" || user.ID == "1" || user.Name != "Grace" {
		t.Fatalf("create = %d %s", created.Code, created.Body)
	}
	if created.Header().Get("Location") != "/users/"+user.ID {
		t.Fatal("missing resource location")
	}
	found := request(s, http.MethodGet, created.Header().Get("Location"), "", "")
	if found.Code != http.StatusOK || found.Body.String() != created.Body.String() {
		t.Fatalf("get = %d %s", found.Code, found.Body)
	}
	listed := request(s, http.MethodGet, "/users", "", "")
	var users []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &users); err != nil {
		t.Fatal(err)
	}
	if listed.Code != http.StatusOK || len(users) != 2 || users[0].Name != "Ada" || users[1].Name != "Grace" {
		t.Fatalf("list = %s", listed.Body)
	}
	if !strings.HasPrefix(listed.Header().Get("Content-Type"), "application/json") {
		t.Fatal("expected JSON response")
	}
}

func TestErrorResponses(t *testing.T) {
	s := server(t)
	for _, tc := range []struct {
		name, method, path, body, mediaType, code string
		status                                    int
	}{
		{"missing user", http.MethodGet, "/users/unknown", "", "", "not_found", 404},
		{"malformed JSON", http.MethodPost, "/users", `{"name":`, "application/json", "invalid_request", 400},
		{"wrong field type", http.MethodPost, "/users", `{"name":42}`, "application/json", "invalid_request", 400},
		{"empty name", http.MethodPost, "/users", `{"name":"  "}`, "application/json", "validation_failed", 422},
		{"missing name", http.MethodPost, "/users", `{}`, "application/json", "validation_failed", 422},
		{"long name", http.MethodPost, "/users", `{"name":"` + strings.Repeat("a", 101) + `"}`, "application/json", "validation_failed", 422},
		{"wrong media type", http.MethodPost, "/users", `name=Ada`, "application/x-www-form-urlencoded", "unsupported_media_type", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(s, tc.method, tc.path, tc.body, tc.mediaType)
			var body struct {
				Code   string            `json:"code"`
				Fields map[string]string `json:"fields"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || body.Code != tc.code {
				t.Fatalf("response = %d %s", w.Code, w.Body)
			}
			if tc.code == "validation_failed" && body.Fields["name"] == "" {
				t.Fatal("missing validation detail")
			}
		})
	}
	listed := request(s, http.MethodGet, "/users", "", "")
	var users []json.RawMessage
	if err := json.Unmarshal(listed.Body.Bytes(), &users); err != nil || len(users) != 1 {
		t.Fatal("invalid requests modified storage")
	}
}

func TestInternalErrorDoesNotExposeCause(t *testing.T) {
	s := server(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users", nil).WithContext(ctx))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "context canceled") || !strings.Contains(w.Body.String(), "internal_error") {
		t.Fatalf("internal error = %d %s", w.Code, w.Body)
	}
}

func TestConstructorValidation(t *testing.T) {
	s, err := bootstrap.Initialize(config.Config{HTTPAddress: ":8080"})
	var invalid *domain.ValidationError
	if s != nil || !errors.As(err, &invalid) || invalid.Field != "name" {
		t.Fatalf("initialize = %v %v", s, err)
	}
}

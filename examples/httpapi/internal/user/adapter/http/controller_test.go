package userhttp_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/palma99/palma-framework/examples/httpapi/internal/config"
	userhttp "github.com/palma99/palma-framework/examples/httpapi/internal/user/adapter/http"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/adapter/memory"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/application"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/domain"
	pfwhttp "github.com/palma99/palma-framework/http"
	pfwecho "github.com/palma99/palma-framework/transport/echo"
)

type alternateMapper struct{ received error }

func (m *alternateMapper) Map(err error) *pfwhttp.Error {
	m.received = err
	return pfwhttp.NewError(http.StatusConflict, "alternate", "custom mapper")
}

func (m *alternateMapper) Write(w http.ResponseWriter, err error) error { return m.Map(err).Write(w) }

func TestControllerAcceptsAlternateMapper(t *testing.T) {
	store, err := memory.NewMemoryStore(config.Config{Seed: config.SeedConfig{UserName: "Ada"}})
	if err != nil {
		t.Fatal(err)
	}
	mapper := &alternateMapper{}
	controller := userhttp.NewController(application.NewService(store))
	router := echo.New()
	router.HTTPErrorHandler = pfwecho.ErrorHandler(mapper, nil)
	controller.Register(router.Group("/users"))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users/missing", nil))
	if response.Code != http.StatusConflict || !errors.Is(mapper.received, domain.ErrNotFound) {
		t.Fatalf("alternate mapper: %d %s, cause=%v", response.Code, response.Body, mapper.received)
	}
}

func TestControllerMountAndNativeRouteMiddleware(t *testing.T) {
	store, err := memory.NewMemoryStore(config.Config{Seed: config.SeedConfig{UserName: "Seed"}})
	if err != nil {
		t.Fatal(err)
	}
	controller := userhttp.NewController(application.NewService(store))
	router := echo.New()
	router.HTTPErrorHandler = pfwecho.ErrorHandler(userhttp.NewErrorMapper(), nil)
	group := router.Group("/api/v1/users")
	controller.Register(group)
	group.POST("/protected", controller.Create, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx *echo.Context) error { return echo.ErrForbidden }
	})
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := request("POST", "/api/v1/users/protected", `{"name":"Blocked"}`); w.Code != 403 {
		t.Fatalf("protected: %d %s", w.Code, w.Body)
	}
	if w := request("GET", "/api/v1/users", ""); w.Code != 200 || strings.Contains(w.Body.String(), "Blocked") {
		t.Fatalf("public: %d %s", w.Code, w.Body)
	}
	created := request("POST", "/api/v1/users", `{"name":"Ada"}`)
	location := created.Header().Get("Location")
	if created.Code != 201 || !strings.HasPrefix(location, "/api/v1/users/") {
		t.Fatalf("create: %d %s %q", created.Code, created.Body, location)
	}
	if w := request("GET", location, ""); w.Code != 200 || w.Body.String() != created.Body.String() {
		t.Fatalf("get: %d %s", w.Code, w.Body)
	}
	if w := request("GET", "/users", ""); w.Code != 404 {
		t.Fatalf("controller owns prefix: %d", w.Code)
	}
}

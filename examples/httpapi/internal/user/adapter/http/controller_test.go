package userhttp_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/palma99/palma-framework/examples/httpapi/internal/config"
	userhttp "github.com/palma99/palma-framework/examples/httpapi/internal/user/adapter/http"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/adapter/memory"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/application"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/domain"
	pfwhttp "github.com/palma99/palma-framework/http"
)

type alternateMapper struct{ received error }

func (m *alternateMapper) Map(err error) *pfwhttp.Error {
	m.received = err
	return pfwhttp.NewError(http.StatusConflict, "alternate", "custom mapper")
}

func (m *alternateMapper) Write(w http.ResponseWriter, err error) error { return m.Map(err).Write(w) }

func TestControllerAcceptsAlternateMapper(t *testing.T) {
	store, err := memory.NewMemoryStore(config.Config{SeedUserName: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	mapper := &alternateMapper{}
	controller := userhttp.NewController(application.NewService(store), mapper)
	router := echo.New()
	controller.Register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users/missing", nil))
	if response.Code != http.StatusConflict || !errors.Is(mapper.received, domain.ErrNotFound) {
		t.Fatalf("alternate mapper: %d %s, cause=%v", response.Code, response.Body, mapper.received)
	}
}

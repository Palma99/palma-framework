package echo_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	pfwhttp "github.com/palma99/palma-framework/http"
	pfwecho "github.com/palma99/palma-framework/transport/echo"
)

func TestErrorHandler(t *testing.T) {
	missing := errors.New("private missing item")
	mapper := pfwhttp.NewMapper(pfwhttp.Is(missing, pfwhttp.NewError(404, "not_found", "item not found")))
	for _, tc := range []struct {
		name      string
		err       error
		status    int
		body      string
		committed bool
	}{
		{"application", missing, 404, "not_found", false},
		{"unknown", errors.New("password=secret"), 500, "internal_error", false},
		{"native middleware", echo.ErrForbidden, 403, "Forbidden", false},
		{"wrapped native", errors.Join(errors.New("detail"), echo.ErrUnauthorized), 401, "Unauthorized", false},
		{"typed HTTP", pfwhttp.NewError(422, "validation_failed", "invalid input"), 422, "validation_failed", false},
		{"committed", errors.New("late"), 200, "already sent", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := echo.New()
			router.HTTPErrorHandler = pfwecho.ErrorHandler(mapper, nil)
			router.GET("/", func(ctx *echo.Context) error {
				if tc.committed {
					_, _ = ctx.Response().Write([]byte("already sent"))
				}
				return tc.err
			})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) || strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("response: %d %s", w.Code, w.Body)
			}
		})
	}
	router := echo.New()
	router.HTTPErrorHandler = pfwecho.ErrorHandler(mapper, nil)
	for _, req := range []*http.Request{httptest.NewRequest("GET", "/missing", nil), httptest.NewRequest("HEAD", "/missing", nil)} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 404 || (req.Method == "HEAD" && w.Body.Len() != 0) {
			t.Fatalf("native routing: %d %s", w.Code, w.Body)
		}
	}
}

func TestNativeMiddlewareOrderAndScope(t *testing.T) {
	router := echo.New()
	router.HTTPErrorHandler = pfwecho.ErrorHandler(nil, nil)
	var events []string
	record := func(name string) echo.MiddlewareFunc {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(ctx *echo.Context) error {
				events = append(events, name+" before")
				err := next(ctx)
				events = append(events, name+" after")
				return err
			}
		}
	}
	router.Use(record("global"))
	group := router.Group("/api/v1", record("group"))
	handler := func(ctx *echo.Context) error { events = append(events, "handler"); return ctx.NoContent(204) }
	group.GET("/public", handler)
	group.POST("/protected", handler, record("route"), func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx *echo.Context) error { return echo.ErrUnauthorized }
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/public", nil))
	if w.Code != 204 || !reflect.DeepEqual(events, []string{"global before", "group before", "handler", "group after", "global after"}) {
		t.Fatalf("public: %d %v", w.Code, events)
	}
	events = nil
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/protected", nil))
	if w.Code != 401 || !reflect.DeepEqual(events, []string{"global before", "group before", "route before", "route after", "group after", "global after"}) {
		t.Fatalf("protected: %d %v", w.Code, events)
	}
}

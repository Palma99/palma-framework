package echo_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	pfwhttp "github.com/palma99/palma-framework/http"
	"github.com/palma99/palma-framework/security"
	securityhttp "github.com/palma99/palma-framework/security/http"
	pfwecho "github.com/palma99/palma-framework/transport/echo"
)

type echoPrincipal struct{ ID string }
type sessionExtractor struct{}

func (*sessionExtractor) Extract(r *http.Request) (string, error) {
	if r.Header.Get("Session") == "" {
		return "", security.ErrMissingCredentials
	}
	return r.Header.Get("Session"), nil
}

type sessionAuthenticator struct{}

func (*sessionAuthenticator) Authenticate(_ context.Context, c string) (string, error) {
	if c == "invalid" {
		return "", security.ErrInvalidCredentials
	}
	if c == "failed" {
		return "", errors.New("private backend error")
	}
	return c, nil
}

type sessionResolver struct{}

func (*sessionResolver) Resolve(_ context.Context, id string) (echoPrincipal, error) {
	return echoPrincipal{ID: id}, nil
}

func TestEchoSecurityRoutesAndRequestRestoration(t *testing.T) {
	mapper := pfwhttp.NewMapper(securityhttp.ErrorRules()...)
	m, err := securityhttp.NewMiddleware(&sessionExtractor{}, &sessionAuthenticator{}, &sessionResolver{}, mapper)
	if err != nil {
		t.Fatal(err)
	}
	router := echo.New()
	router.HTTPErrorHandler = pfwecho.ErrorHandler(mapper, nil)
	handler := func(ctx *echo.Context) error {
		p, err := security.Current[echoPrincipal](ctx.Request().Context())
		if errors.Is(err, security.ErrUnauthenticated) {
			return ctx.String(200, "anonymous")
		}
		if err != nil {
			return err
		}
		if p.ID == "denied" {
			return security.ErrForbidden
		}
		return ctx.String(200, p.ID)
	}
	router.GET("/public", handler)
	router.GET("/required", handler, pfwecho.RequiredSecurity(m))
	router.GET("/optional", handler, pfwecho.OptionalSecurity(m))
	for _, tc := range []struct {
		path, session, body string
		status              int
	}{
		{"/public", "user", "anonymous", 200},
		{"/required", "", "unauthenticated", 401},
		{"/required", "user", "user", 200},
		{"/required", "denied", "forbidden", 403},
		{"/optional", "", "anonymous", 200},
		{"/optional", "user", "user", 200},
		{"/optional", "invalid", "unauthenticated", 401},
		{"/optional", "failed", "internal_error", 500},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.Header.Set("Session", tc.session)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) || strings.Contains(w.Body.String(), "private") {
			t.Fatalf("%s/%s: %d %s", tc.path, tc.session, w.Code, w.Body)
		}
	}
	for _, panics := range []bool{false, true} {
		original := httptest.NewRequest("GET", "/", nil)
		original.Header.Set("Session", "user")
		ctx := router.NewContext(original, httptest.NewRecorder())
		wrapped := pfwecho.RequiredSecurity(m)(func(ctx *echo.Context) error {
			if p, err := security.Current[echoPrincipal](ctx.Request().Context()); err != nil || p.ID != "user" {
				t.Fatal("principal not installed")
			}
			if panics {
				panic("handler panic")
			}
			return security.ErrForbidden
		})
		func() {
			defer func() {
				if got := recover(); panics && got != "handler panic" {
					t.Fatalf("panic: %v", got)
				} else if !panics && got != nil {
					t.Fatalf("unexpected panic: %v", got)
				}
			}()
			if err := wrapped(ctx); !errors.Is(err, security.ErrForbidden) {
				t.Fatalf("handler error: %v", err)
			}
		}()
		if ctx.Request() != original {
			t.Fatal("Echo request not restored")
		}
	}
}

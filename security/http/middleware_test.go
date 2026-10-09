package http_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	pfwhttp "github.com/palma99/palma-framework/http"
	"github.com/palma99/palma-framework/security"
	securityhttp "github.com/palma99/palma-framework/security/http"
)

type credentials string
type identity string
type principal struct{ ID string }
type extractorFunc func(*http.Request) (credentials, error)

func (f extractorFunc) Extract(r *http.Request) (credentials, error) { return f(r) }

type authenticatorFunc func(context.Context, credentials) (identity, error)

func (f authenticatorFunc) Authenticate(ctx context.Context, c credentials) (identity, error) {
	return f(ctx, c)
}

type resolverFunc func(context.Context, identity) (principal, error)

func (f resolverFunc) Resolve(ctx context.Context, i identity) (principal, error) { return f(ctx, i) }

func TestAuthenticationStagesAndOptionalSemantics(t *testing.T) {
	private := errors.New("private database password")
	for _, tc := range []struct {
		name                            string
		optional                        bool
		extractErr, authErr, resolveErr error
		status                          int
		calls                           string
		anonymous                       bool
	}{
		{name: "required success", status: 204, calls: "EARH"},
		{name: "optional success", optional: true, status: 204, calls: "EARH"},
		{name: "required absent", extractErr: security.ErrMissingCredentials, status: 401, calls: "E"},
		{name: "optional absent", optional: true, extractErr: fmt.Errorf("cookie: %w", security.ErrMissingCredentials), status: 204, calls: "EH", anonymous: true},
		{name: "optional malformed", optional: true, extractErr: security.ErrInvalidCredentials, status: 401, calls: "E"},
		{name: "optional rejected", optional: true, authErr: security.ErrInvalidCredentials, status: 401, calls: "EA"},
		{name: "auth missing is not anonymous", optional: true, authErr: security.ErrMissingCredentials, status: 401, calls: "EA"},
		{name: "auth infrastructure", optional: true, authErr: private, status: 500, calls: "EA"},
		{name: "extract infrastructure", optional: true, extractErr: private, status: 500, calls: "E"},
		{name: "resolver unauthenticated", resolveErr: security.ErrUnauthenticated, status: 401, calls: "EAR"},
		{name: "resolver disabled", optional: true, resolveErr: security.ErrForbidden, status: 403, calls: "EAR"},
		{name: "resolver infrastructure", optional: true, resolveErr: private, status: 500, calls: "EAR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := ""
			m, err := securityhttp.NewMiddleware(
				extractorFunc(func(r *http.Request) (credentials, error) { calls += "E"; return "session", tc.extractErr }),
				authenticatorFunc(func(ctx context.Context, c credentials) (identity, error) {
					calls += "A"
					if c != "session" {
						t.Fatalf("credentials: %q", c)
					}
					return "verified-user", tc.authErr
				}),
				resolverFunc(func(ctx context.Context, i identity) (principal, error) {
					calls += "R"
					if i != "verified-user" {
						t.Fatalf("identity: %q", i)
					}
					if _, err := security.Current[principal](ctx); !errors.Is(err, security.ErrUnauthenticated) {
						t.Fatal("upstream identity retained")
					}
					return principal{ID: "resolved-user"}, tc.resolveErr
				}), nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls += "H"
				p, err := security.Current[principal](r.Context())
				if tc.anonymous {
					if !errors.Is(err, security.ErrUnauthenticated) {
						t.Fatalf("anonymous principal: %+v %v", p, err)
					}
				} else if err != nil || p.ID != "resolved-user" {
					t.Fatalf("principal: %+v %v", p, err)
				}
				w.WriteHeader(204)
			})
			handler := m.Required(next)
			if tc.optional {
				handler = m.Optional(next)
			}
			r := httptest.NewRequest("GET", "/", nil)
			r = r.WithContext(security.WithPrincipal(r.Context(), principal{ID: "upstream"}))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status || calls != tc.calls || strings.Contains(w.Body.String(), "private") {
				t.Fatalf("response: %d %s calls=%s", w.Code, w.Body, calls)
			}
			if got, _ := security.Current[principal](r.Context()); got.ID != "upstream" {
				t.Fatal("original request mutated")
			}
		})
	}
}

func TestSharedMapperAndHandlerAuthorization(t *testing.T) {
	mapper := pfwhttp.NewMapper(securityhttp.ErrorRules()...)
	m, err := securityhttp.NewMiddleware(
		extractorFunc(func(*http.Request) (credentials, error) { return "ok", nil }),
		authenticatorFunc(func(context.Context, credentials) (identity, error) { return "ok", nil }),
		resolverFunc(func(context.Context, identity) (principal, error) { return principal{ID: "user"}, nil }), mapper,
	)
	if err != nil {
		t.Fatal(err)
	}
	handler := pfwhttp.NewHandlerAdapter(mapper, nil).Wrap(func(http.ResponseWriter, *http.Request) error {
		return fmt.Errorf("document policy: %w", security.ErrForbidden)
	})
	w := httptest.NewRecorder()
	m.Required(handler).ServeHTTP(w, httptest.NewRequest("PATCH", "/", nil))
	if w.Code != 403 || !strings.Contains(w.Body.String(), "forbidden") {
		t.Fatalf("policy: %d %s", w.Code, w.Body)
	}
	for _, sentinel := range []error{security.ErrMissingCredentials, security.ErrInvalidCredentials, security.ErrUnauthenticated} {
		mapped := mapper.Map(fmt.Errorf("wrapped: %w", sentinel))
		if mapped.Status != 401 || !errors.Is(mapped, sentinel) {
			t.Fatalf("mapping: %+v", mapped)
		}
	}
}

func TestRequestIsolationAndCancellation(t *testing.T) {
	m, err := securityhttp.NewMiddleware(
		extractorFunc(func(r *http.Request) (credentials, error) { return credentials(r.Header.Get("Session")), nil }),
		authenticatorFunc(func(ctx context.Context, c credentials) (identity, error) { return identity(c), ctx.Err() }),
		resolverFunc(func(ctx context.Context, i identity) (principal, error) { return principal{ID: string(i)}, ctx.Err() }), nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.RequiredRequest(nil); err == nil {
		t.Fatal("nil request accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.OptionalRequest(httptest.NewRequest("GET", "/", nil).WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			id := fmt.Sprintf("user-%d", i)
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Session", id)
			authenticated, err := m.RequiredRequest(r)
			if err != nil {
				t.Error(err)
				return
			}
			got, err := security.Current[principal](authenticated.Context())
			if err != nil || got.ID != id {
				t.Errorf("cross-request identity: %+v %v", got, err)
			}
		})
	}
	wg.Wait()
}

func TestMissingDependencies(t *testing.T) {
	if _, err := securityhttp.NewMiddleware[credentials, identity, principal](nil, nil, nil, nil); err == nil {
		t.Fatal("missing dependencies accepted")
	}
}

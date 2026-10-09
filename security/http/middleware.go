// Package http connects typed security contracts to net/http. It neither
// chooses a credential scheme nor imposes a principal type.
package http

import (
	"errors"
	"fmt"
	stdhttp "net/http"

	pfwhttp "github.com/palma99/palma-framework/http"
	"github.com/palma99/palma-framework/logging"
	"github.com/palma99/palma-framework/security"
)

// CredentialExtractor reads credentials without authenticating them. Return
// ErrMissingCredentials only when absent, ErrInvalidCredentials when malformed.
// Extractors must be safe for concurrent use and must not consume the body
// unless the application's handler contract explicitly allows that.
type CredentialExtractor[C any] interface {
	Extract(*stdhttp.Request) (C, error)
}

// RequestAuthenticator is the transport bridge used by router adapters. On
// success the returned request has the authenticated or anonymous context.
type RequestAuthenticator interface {
	RequiredRequest(*stdhttp.Request) (*stdhttp.Request, error)
	OptionalRequest(*stdhttp.Request) (*stdhttp.Request, error)
}

// ErrorRules maps wrapped security errors to safe HTTP responses. Register these
// rules in the same mapper used by middleware, controllers and router adapters.
// Operational errors are left to the mapper's normal fallback.
func ErrorRules() []pfwhttp.Rule {
	unauthenticated := pfwhttp.NewError(stdhttp.StatusUnauthorized, "unauthenticated", "authentication required")
	return []pfwhttp.Rule{
		pfwhttp.Is(security.ErrMissingCredentials, unauthenticated),
		pfwhttp.Is(security.ErrInvalidCredentials, unauthenticated),
		pfwhttp.Is(security.ErrUnauthenticated, unauthenticated),
		pfwhttp.Is(security.ErrForbidden, pfwhttp.NewError(stdhttp.StatusForbidden, "forbidden", "access denied")),
	}
}

// Middleware composes extraction, authentication and principal resolution.
// Dependencies must be non-nil (including values inside interfaces) and safe
// for concurrent use. No credentials or principals are kept on the middleware.
type Middleware[C, I, P any] struct {
	extractor     CredentialExtractor[C]
	authenticator security.Authenticator[C, I]
	resolver      security.PrincipalResolver[I, P]
	handler       *pfwhttp.HandlerAdapter
}

// NewMiddleware uses the supplied mapper unchanged; it must include ErrorRules
// if standard security responses are desired. A nil mapper selects those rules
// and the safe 500 fallback. Default structured logging reports server errors.
func NewMiddleware[C, I, P any](extractor CredentialExtractor[C], authenticator security.Authenticator[C, I], resolver security.PrincipalResolver[I, P], mapper pfwhttp.ErrorMapper) (*Middleware[C, I, P], error) {
	if extractor == nil || authenticator == nil || resolver == nil {
		return nil, errors.New("securityhttp: extractor, authenticator and resolver are required")
	}
	if mapper == nil {
		mapper = pfwhttp.NewMapper(ErrorRules()...)
	}
	return &Middleware[C, I, P]{
		extractor: extractor, authenticator: authenticator, resolver: resolver,
		handler: pfwhttp.NewHandlerAdapter(mapper, logging.NewDefault()),
	}, nil
}

// RequiredRequest authenticates a request; absent credentials are an error.
func (m *Middleware[C, I, P]) RequiredRequest(r *stdhttp.Request) (*stdhttp.Request, error) {
	return m.authenticate(r, false)
}

// OptionalRequest allows only absent credentials to proceed anonymously.
// Malformed/rejected credentials and resolver failures remain errors. Any
// upstream principal of P is masked before processing this boundary.
func (m *Middleware[C, I, P]) OptionalRequest(r *stdhttp.Request) (*stdhttp.Request, error) {
	return m.authenticate(r, true)
}

func (m *Middleware[C, I, P]) authenticate(r *stdhttp.Request, optional bool) (*stdhttp.Request, error) {
	if r == nil {
		return nil, errors.New("securityhttp: request is required")
	}
	r = r.WithContext(security.WithoutPrincipal[P](r.Context()))
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	credentials, err := m.extractor.Extract(r)
	if err != nil {
		if optional && errors.Is(err, security.ErrMissingCredentials) {
			return r, nil
		}
		return nil, fmt.Errorf("securityhttp: extract credentials: %w", err)
	}
	identity, err := m.authenticator.Authenticate(r.Context(), credentials)
	if err != nil {
		return nil, fmt.Errorf("securityhttp: authenticate: %w", err)
	}
	principal, err := m.resolver.Resolve(r.Context(), identity)
	if err != nil {
		return nil, fmt.Errorf("securityhttp: resolve principal: %w", err)
	}
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	return r.WithContext(security.WithPrincipal(r.Context(), principal)), nil
}

// Required protects a net/http handler with mandatory authentication.
func (m *Middleware[C, I, P]) Required(next stdhttp.Handler) stdhttp.Handler {
	return m.wrap(next, m.RequiredRequest)
}

// Optional permits anonymous requests only when credentials are absent.
func (m *Middleware[C, I, P]) Optional(next stdhttp.Handler) stdhttp.Handler {
	return m.wrap(next, m.OptionalRequest)
}

func (m *Middleware[C, I, P]) wrap(next stdhttp.Handler, authenticate func(*stdhttp.Request) (*stdhttp.Request, error)) stdhttp.Handler {
	return m.handler.Wrap(func(w stdhttp.ResponseWriter, r *stdhttp.Request) error {
		authenticated, err := authenticate(r)
		if err != nil {
			return err
		}
		next.ServeHTTP(w, authenticated)
		return nil
	})
}

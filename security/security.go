// Package security defines transport-independent, typed authentication and
// authorization contracts. Applications own credentials, identities, principals,
// resources and the meaning of their fields. Implementations shared through DI
// must be safe for concurrent use.
package security

import (
	"context"
	"errors"
)

var (
	ErrMissingCredentials = errors.New("security: credentials are missing")
	ErrInvalidCredentials = errors.New("security: credentials are invalid")
	ErrUnauthenticated    = errors.New("security: principal is unavailable")
	ErrForbidden          = errors.New("security: access is forbidden")
)

// Authenticator verifies credentials before producing an identity. Credential
// rejection should wrap ErrInvalidCredentials; infrastructure errors must remain
// distinguishable. I carries only data from the verified identity source.
type Authenticator[C, I any] interface {
	Authenticate(context.Context, C) (I, error)
}

// PrincipalResolver builds an application principal from a verified identity.
// It may load application data or use the identity directly. A principal is
// accepted only on a nil error; its shape and validity belong to the application.
type PrincipalResolver[I, P any] interface {
	Resolve(context.Context, I) (P, error)
}

// Authorizer evaluates an action on a typed resource for an explicit principal.
// Policies should deny unsupported actions with ErrForbidden. Operational errors
// must not be converted into access denials.
type Authorizer[P, R any] interface {
	Check(context.Context, P, string, R) error
}

type principalKey[P any] struct{}
type principalValue[P any] struct {
	value   P
	present bool
}

// WithPrincipal propagates a trusted principal. Only authentication boundaries
// or trusted application code should call it. P must be the same instantiated
// type used by Current. Principals (including slices/maps/pointers) must be
// treated as immutable after publication; no copy or validation is performed.
func WithPrincipal[P any](ctx context.Context, principal P) context.Context {
	return context.WithValue(ctx, principalKey[P]{}, principalValue[P]{value: principal, present: true})
}

// WithoutPrincipal masks an upstream principal of P while preserving other
// context values and cancellation. Anonymous authentication uses this to avoid
// accidentally retaining an upstream identity of the same type.
func WithoutPrincipal[P any](ctx context.Context) context.Context {
	return context.WithValue(ctx, principalKey[P]{}, principalValue[P]{})
}

// Current retrieves P or returns ErrUnauthenticated. A different type, nil
// context, or a masked/absent principal never causes a type assertion panic.
// A zero-valued P deliberately published by WithPrincipal is still present.
func Current[P any](ctx context.Context) (P, error) {
	if ctx != nil {
		if principal, ok := ctx.Value(principalKey[P]{}).(principalValue[P]); ok && principal.present {
			return principal.value, nil
		}
	}
	var zero P
	return zero, ErrUnauthenticated
}

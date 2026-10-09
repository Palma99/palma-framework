package echo

import (
	"github.com/labstack/echo/v5"
	securityhttp "github.com/palma99/palma-framework/security/http"
)

// RequiredSecurity authenticates before calling the next Echo handler. Configure
// ErrorHandler with a mapper containing securityhttp.ErrorRules. The original
// request is restored after the handler returns, including on panic.
func RequiredSecurity(authenticator securityhttp.RequestAuthenticator) echo.MiddlewareFunc {
	return securityMiddleware(authenticator, false)
}

// OptionalSecurity accepts anonymous requests only when credentials are absent.
func OptionalSecurity(authenticator securityhttp.RequestAuthenticator) echo.MiddlewareFunc {
	return securityMiddleware(authenticator, true)
}

func securityMiddleware(authenticator securityhttp.RequestAuthenticator, optional bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx *echo.Context) error {
			original := ctx.Request()
			var request = original
			var err error
			if optional {
				request, err = authenticator.OptionalRequest(original)
			} else {
				request, err = authenticator.RequiredRequest(original)
			}
			if err != nil {
				return err
			}
			ctx.SetRequest(request)
			defer ctx.SetRequest(original)
			return next(ctx)
		}
	}
}

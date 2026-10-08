// Package echo provides optional helpers for native Echo handlers and middleware.
package echo

import (
	"errors"

	"github.com/labstack/echo/v5"
	pfwhttp "github.com/palma99/palma-framework/http"
	"github.com/palma99/palma-framework/logging"
)

// ErrorHandler maps application errors at the router boundary. Native Echo
// status errors (including routing and middleware errors) retain Echo's handling.
// A committed response is never overwritten; late errors are only logged.
func ErrorHandler(mapper pfwhttp.ErrorMapper, logger logging.Logger) echo.HTTPErrorHandler {
	if mapper == nil {
		mapper = pfwhttp.NewMapper()
	}
	native := echo.DefaultHTTPErrorHandler(false)
	return func(ctx *echo.Context, err error) {
		if err == nil {
			return
		}
		log := func(message string, err error) {
			if logger != nil {
				logger.Error(ctx.Request().Context(), message, "error", err)
			}
		}
		if response, _ := echo.UnwrapResponse(ctx.Response()); response != nil && response.Committed {
			log("HTTP response failed", err)
			return
		}
		var status echo.HTTPStatusCoder
		if errors.As(err, &status) {
			if status.StatusCode() >= 500 {
				log("request failed", err)
			}
			native(ctx, err)
			return
		}
		mapped := mapper.Map(err)
		if mapped == nil {
			return
		}
		if mapped.Status >= 500 {
			log("request failed", err)
		}
		if err := mapped.Write(ctx.Response()); err != nil {
			log("HTTP response failed", err)
		}
	}
}

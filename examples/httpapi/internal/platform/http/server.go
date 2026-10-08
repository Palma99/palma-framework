package apihttp

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/palma99/palma-framework/examples/httpapi/internal/config"
	userhttp "github.com/palma99/palma-framework/examples/httpapi/internal/user/adapter/http"
	pfwhttp "github.com/palma99/palma-framework/http"
	"github.com/palma99/palma-framework/logging"
	pfwecho "github.com/palma99/palma-framework/transport/echo"
)

func Routes(users *userhttp.Controller, errors pfwhttp.ErrorMapper, logger logging.Logger) *echo.Echo {
	router := echo.New()
	router.HTTPErrorHandler = pfwecho.ErrorHandler(errors, logger)
	users.Register(router.Group("/users"))
	return router
}

func Server(router *echo.Echo, cfg config.Config) *http.Server {
	return &http.Server{Addr: cfg.HTTP.Address, Handler: router, ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout}
}

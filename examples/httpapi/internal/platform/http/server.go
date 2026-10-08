package apihttp

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/palma99/palma-framework/examples/httpapi/internal/config"
	userhttp "github.com/palma99/palma-framework/examples/httpapi/internal/user/adapter/http"
)

func Routes(users *userhttp.Controller) *echo.Echo {
	router := echo.New()
	users.Register(router)
	return router
}

func Server(router *echo.Echo, cfg config.Config) *http.Server {
	return &http.Server{Addr: cfg.HTTP.Address, Handler: router, ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout}
}

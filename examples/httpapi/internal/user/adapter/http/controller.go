package userhttp

import (
	"github.com/labstack/echo/v5"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/application"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/domain"
	pfwhttp "github.com/palma99/palma-framework/http"
)

type Controller struct {
	service *application.Service
	errors  pfwhttp.ErrorMapper
}

//pfw:coconut
func NewController(service *application.Service, errors pfwhttp.ErrorMapper) *Controller {
	return &Controller{service: service, errors: errors}
}

func (c *Controller) Register(router *echo.Echo) {
	router.GET("/users", c.list)
	router.GET("/users/:id", c.get)
	router.POST("/users", c.create)
}

type userResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func response(user domain.User) userResponse { return userResponse{ID: user.ID, Name: user.Name} }

func (c *Controller) list(ctx *echo.Context) error {
	users, err := c.service.List(ctx.Request().Context())
	if err != nil {
		return c.errors.Write(ctx.Response(), err)
	}
	result := make([]userResponse, 0, len(users))
	for _, user := range users {
		result = append(result, response(user))
	}
	return pfwhttp.OK(result).Write(ctx.Response())
}

func (c *Controller) get(ctx *echo.Context) error {
	user, err := c.service.Get(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return c.errors.Write(ctx.Response(), err)
	}
	return pfwhttp.OK(response(user)).Write(ctx.Response())
}

type createRequest struct {
	Name string `json:"name"`
}

func (c *Controller) create(ctx *echo.Context) error {
	input, err := pfwhttp.DecodeJSON[createRequest](ctx.Request(), pfwhttp.DecodeOptions{})
	if err != nil {
		return c.errors.Write(ctx.Response(), err)
	}
	user, err := c.service.Create(ctx.Request().Context(), input.Name)
	if err != nil {
		return c.errors.Write(ctx.Response(), err)
	}
	return pfwhttp.Created("/users/"+user.ID, response(user)).Write(ctx.Response())
}

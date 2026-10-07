package userhttp

import (
	"errors"
	"mime"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/application"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/domain"
)

type Controller struct{ service *application.Service }

//pfw:coconut
func NewController(service *application.Service) *Controller { return &Controller{service: service} }

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

type errorResponse struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (c *Controller) list(ctx *echo.Context) error {
	users, err := c.service.List(ctx.Request().Context())
	if err != nil {
		return writeError(ctx, err)
	}
	result := make([]userResponse, 0, len(users))
	for _, user := range users {
		result = append(result, response(user))
	}
	return ctx.JSON(http.StatusOK, result)
}

func (c *Controller) get(ctx *echo.Context) error {
	user, err := c.service.Get(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return writeError(ctx, err)
	}
	return ctx.JSON(http.StatusOK, response(user))
}

func (c *Controller) create(ctx *echo.Context) error {
	mediaType, _, err := mime.ParseMediaType(ctx.Request().Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return ctx.JSON(http.StatusUnsupportedMediaType, errorResponse{Code: "unsupported_media_type", Message: "Content-Type must be application/json"})
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := ctx.Bind(&input); err != nil {
		return ctx.JSON(http.StatusBadRequest, errorResponse{Code: "invalid_request", Message: "invalid JSON body"})
	}
	user, err := c.service.Create(ctx.Request().Context(), input.Name)
	if err != nil {
		return writeError(ctx, err)
	}
	ctx.Response().Header().Set("Location", "/users/"+user.ID)
	return ctx.JSON(http.StatusCreated, response(user))
}

func writeError(ctx *echo.Context, err error) error {
	var invalid *domain.ValidationError
	switch {
	case errors.As(err, &invalid):
		return ctx.JSON(http.StatusUnprocessableEntity, errorResponse{Code: "validation_failed", Message: "invalid input", Fields: map[string]string{invalid.Field: invalid.Message}})
	case errors.Is(err, domain.ErrNotFound):
		return ctx.JSON(http.StatusNotFound, errorResponse{Code: "not_found", Message: "user not found"})
	default:
		return ctx.JSON(http.StatusInternalServerError, errorResponse{Code: "internal_error", Message: "unable to process request"})
	}
}

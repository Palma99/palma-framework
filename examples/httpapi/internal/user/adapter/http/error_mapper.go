package userhttp

import (
	"net/http"

	"github.com/palma99/palma-framework/examples/httpapi/internal/user/domain"
	pfwhttp "github.com/palma99/palma-framework/http"
)

//pfw:coconut
func NewErrorMapper() *pfwhttp.Mapper {
	return pfwhttp.NewMapper(
		pfwhttp.As(func(invalid *domain.ValidationError) *pfwhttp.Error {
			return pfwhttp.NewError(
				http.StatusUnprocessableEntity, "validation_failed", "invalid input",
			).WithFields(map[string]string{
				invalid.Field: invalid.Message,
			})
		}),
		pfwhttp.Is(
			domain.ErrNotFound, pfwhttp.NewError(http.StatusNotFound, "not_found", "user not found"),
		),
	)
}

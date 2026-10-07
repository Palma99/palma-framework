package application

import (
	"context"
	"strings"

	"github.com/palma99/palma-framework/examples/httpapi/internal/user/domain"
)

// Repository is the application port. Storage adapters implement it.
type Repository interface {
	List(context.Context) ([]domain.User, error)
	Get(context.Context, string) (domain.User, error)
	Create(context.Context, domain.User) (domain.User, error)
}

type Service struct{ repository Repository }

//pfw:coconut
func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) List(ctx context.Context) ([]domain.User, error) { return s.repository.List(ctx) }

func (s *Service) Get(ctx context.Context, id string) (domain.User, error) {
	if strings.TrimSpace(id) == "" {
		return domain.User{}, &domain.ValidationError{Field: "id", Message: "id is required"}
	}
	return s.repository.Get(ctx, id)
}

func (s *Service) Create(ctx context.Context, name string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	user, err := domain.New(name)
	if err != nil {
		return domain.User{}, err
	}
	return s.repository.Create(ctx, user)
}

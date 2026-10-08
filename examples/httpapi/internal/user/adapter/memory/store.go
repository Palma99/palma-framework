package memory

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/palma99/palma-framework/examples/httpapi/internal/config"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/domain"
)

type Store struct {
	mu    sync.RWMutex
	users map[string]domain.User
	ids   []string
	next  uint64
}

//pfw:coconut
func NewMemoryStore(cfg config.Config) (*Store, error) {
	seed, err := domain.New(cfg.Seed.UserName)
	if err != nil {
		return nil, fmt.Errorf("initial user: %w", err)
	}
	seed.ID = "1"
	return &Store{users: map[string]domain.User{"1": seed}, ids: []string{"1"}, next: 2}, nil
}

func (s *Store) List(ctx context.Context) ([]domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]domain.User, 0, len(s.ids))
	for _, id := range s.ids {
		result = append(result, s.users[id])
	}
	return result, nil
}

func (s *Store) Get(ctx context.Context, id string) (domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	user, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return user, nil
}

func (s *Store) Create(ctx context.Context, user domain.User) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	user.ID = strconv.FormatUint(s.next, 10)
	s.next++
	s.users[user.ID] = user
	s.ids = append(s.ids, user.ID)
	return user, nil
}

package memory_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/palma99/palma-framework/examples/httpapi/internal/config"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/adapter/memory"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/application"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/domain"
)

func TestConcurrentCreationAndSnapshotIsolation(t *testing.T) {
	store, err := memory.NewMemoryStore(config.Config{Seed: config.SeedConfig{UserName: "Ada"}})
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewService(store)
	var wg sync.WaitGroup
	results := make(chan domain.User, 64)
	errors := make(chan error, 64)
	for n := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			user, err := service.Create(context.Background(), fmt.Sprintf("User %d", n))
			if err != nil {
				errors <- err
				return
			}
			results <- user
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	ids := map[string]bool{"1": true}
	for user := range results {
		if ids[user.ID] {
			t.Fatalf("duplicate ID %s", user.ID)
		}
		ids[user.ID] = true
	}
	users, err := service.List(context.Background())
	if err != nil || len(users) != 65 || len(ids) != 65 {
		t.Fatalf("users=%d IDs=%d err=%v", len(users), len(ids), err)
	}
	users[0].Name = "changed outside storage"
	seed, err := service.Get(context.Background(), "1")
	if err != nil || seed.Name != "Ada" {
		t.Fatalf("snapshot aliasing: %+v %v", seed, err)
	}
}

func TestCancelledCreateDoesNotStoreUser(t *testing.T) {
	store, err := memory.NewMemoryStore(config.Config{Seed: config.SeedConfig{UserName: "Ada"}})
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewService(store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Create(ctx, "Grace"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	users, err := service.List(context.Background())
	if err != nil || len(users) != 1 {
		t.Fatalf("users=%v err=%v", users, err)
	}
}

package security_test

import (
	"context"
	"errors"
	"testing"

	"github.com/palma99/palma-framework/security"
)

type principal struct {
	ID         string
	Roles      []string
	Attributes map[string]string
}
type otherPrincipal principal

func TestPrincipalTypeIsolationMaskingAndContext(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	value := principal{ID: "user", Roles: []string{"editor"}, Attributes: map[string]string{"tenant": "one"}}
	ctx := security.WithPrincipal(base, value)
	got, err := security.Current[principal](ctx)
	if err != nil || got.ID != "user" || got.Attributes["tenant"] != "one" {
		t.Fatalf("principal: %+v %v", got, err)
	}
	for _, missing := range []context.Context{nil, base, security.WithoutPrincipal[principal](ctx)} {
		if _, err := security.Current[principal](missing); !errors.Is(err, security.ErrUnauthenticated) {
			t.Fatalf("missing: %v", err)
		}
	}
	if _, err := security.Current[otherPrincipal](ctx); !errors.Is(err, security.ErrUnauthenticated) {
		t.Fatalf("different type: %v", err)
	}
	if _, err := security.Current[*principal](ctx); !errors.Is(err, security.ErrUnauthenticated) {
		t.Fatalf("different pointer type: %v", err)
	}
	zero := security.WithPrincipal(base, principal{})
	if _, err := security.Current[principal](zero); err != nil {
		t.Fatalf("published zero value: %v", err)
	}
	withOther := security.WithPrincipal(ctx, otherPrincipal{ID: "other"})
	masked := security.WithoutPrincipal[principal](withOther)
	if got, err := security.Current[otherPrincipal](masked); err != nil || got.ID != "other" {
		t.Fatalf("mask must preserve unrelated types: %+v %v", got, err)
	}
	if got, _ := security.Current[principal](ctx); got.ID != "user" {
		t.Fatal("mask mutated parent context")
	}
	cancel()
	if !errors.Is(masked.Err(), context.Canceled) {
		t.Fatal("principal context lost cancellation")
	}
}

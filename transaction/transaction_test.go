package transaction_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/palma99/palma-framework/transaction"
)

type backend struct {
	tx       *resource
	beginErr error
	begins   int
	beginCtx context.Context
	onBegin  func()
}

func (b *backend) Begin(ctx context.Context) (transaction.Transaction, error) {
	b.begins++
	b.beginCtx = ctx
	if b.onBegin != nil {
		b.onBegin()
	}
	return b.tx, b.beginErr
}

type resource struct {
	commits, rollbacks     int
	commitErr, rollbackErr error
	commitCtx, rollbackCtx context.Context
	rollbackCancelled      bool
}

func (r *resource) Commit(ctx context.Context) error {
	r.commits++
	r.commitCtx = ctx
	return r.commitErr
}
func (r *resource) Rollback(ctx context.Context) error {
	r.rollbacks++
	r.rollbackCtx = ctx
	r.rollbackCancelled = ctx.Err() != nil
	return r.rollbackErr
}
func manager(t *testing.T, b *backend) *transaction.Manager {
	t.Helper()
	m, err := transaction.New(b, transaction.Options{RollbackTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

type valueKey struct{}

func TestWithinPropagatesContextAndExpiresScope(t *testing.T) {
	b := &backend{tx: &resource{}}
	m := manager(t, b)
	parent, cancel := context.WithTimeout(context.WithValue(context.Background(), valueKey{}, "trace"), time.Minute)
	defer cancel()
	var retained context.Context
	err := m.Within(parent, func(ctx context.Context) error {
		retained = ctx
		if ctx.Value(valueKey{}) != "trace" {
			t.Fatal("lost parent values")
		}
		want, _ := parent.Deadline()
		got, ok := ctx.Deadline()
		if !ok || !got.Equal(want) {
			t.Fatal("lost deadline")
		}
		tx, err := transaction.Current(ctx)
		if err != nil || tx != b.tx {
			t.Fatalf("current: %v %v", tx, err)
		}
		return nil
	})
	if err != nil || b.begins != 1 || b.tx.commits != 1 || b.tx.rollbacks != 0 {
		t.Fatalf("outcome: %+v %v", b.tx, err)
	}
	if _, err := transaction.Current(retained); !errors.Is(err, transaction.ErrClosed) {
		t.Fatalf("retained context: %v", err)
	}
	if retained.Err() != context.Canceled {
		t.Fatal("scope context not cancelled")
	}
	if parent.Err() != nil {
		t.Fatal("cancelled parent")
	}
}

func TestFailureCancellationAndCommitFailure(t *testing.T) {
	operation := errors.New("operation failed")
	cleanup := errors.New("cleanup failed")
	commit := errors.New("commit failed")
	for _, tc := range []struct {
		name                                string
		callbackErr, commitErr, rollbackErr error
		cancel                              bool
		want                                error
		commits                             int
	}{
		{name: "operation", callbackErr: operation, want: operation},
		{name: "cleanup", callbackErr: operation, rollbackErr: cleanup, want: cleanup},
		{name: "cancel", cancel: true, want: context.Canceled},
		{name: "commit", commitErr: commit, want: commit, commits: 1},
		{name: "commit and cleanup", commitErr: commit, rollbackErr: cleanup, want: commit, commits: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &backend{tx: &resource{commitErr: tc.commitErr, rollbackErr: tc.rollbackErr}}
			m := manager(t, b)
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), valueKey{}, "trace"))
			defer cancel()
			err := m.Within(ctx, func(context.Context) error {
				if tc.cancel {
					cancel()
				}
				return tc.callbackErr
			})
			if !errors.Is(err, tc.want) || b.tx.commits != tc.commits || b.tx.rollbacks != 1 {
				t.Fatalf("outcome: %+v %v", b.tx, err)
			}
			if tc.callbackErr != nil && !errors.Is(err, tc.callbackErr) {
				t.Fatal("lost callback error")
			}
			if tc.rollbackErr != nil && !errors.Is(err, tc.rollbackErr) {
				t.Fatal("lost rollback error")
			}
			if b.tx.rollbackCancelled {
				t.Fatal("rollback received cancelled context")
			}
			if b.tx.rollbackCtx.Value(valueKey{}) != "trace" {
				t.Fatal("cleanup lost values")
			}
			if _, ok := b.tx.rollbackCtx.Deadline(); !ok {
				t.Fatal("cleanup has no deadline")
			}
		})
	}
}

func TestNestedCallsJoinAndCannotHideFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "handled failure"}[fail], func(t *testing.T) {
			b := &backend{tx: &resource{}}
			m := manager(t, b)
			cause := errors.New("nested failure")
			err := m.Within(context.Background(), func(ctx context.Context) error {
				derived := context.WithValue(ctx, valueKey{}, "nested")
				_ = m.Within(derived, func(inner context.Context) error {
					if inner.Value(valueKey{}) != "nested" {
						t.Fatal("nested context replaced")
					}
					if fail {
						return cause
					}
					return nil
				})
				return nil
			})
			if b.begins != 1 {
				t.Fatal("nested call began a second transaction")
			}
			if fail {
				if !errors.Is(err, transaction.ErrRollbackOnly) || !errors.Is(err, cause) || b.tx.commits != 0 || b.tx.rollbacks != 1 {
					t.Fatalf("hidden failure committed: %+v %v", b.tx, err)
				}
			} else if err != nil || b.tx.commits != 1 {
				t.Fatalf("nested success: %v", err)
			}
		})
	}
}

func TestPanicRollsBackAndPropagates(t *testing.T) {
	b := &backend{tx: &resource{}}
	m := manager(t, b)
	token := &struct{}{}
	defer func() {
		if recover() != token || b.tx.rollbacks != 1 || b.tx.commits != 0 {
			t.Fatalf("panic outcome: %+v", b.tx)
		}
	}()
	_ = m.Within(context.Background(), func(context.Context) error { panic(token) })
}

func TestRecoveredNestedPanicStillRollsBack(t *testing.T) {
	b := &backend{tx: &resource{}}
	m := manager(t, b)
	err := m.Within(context.Background(), func(ctx context.Context) error {
		func() {
			defer func() { _ = recover() }()
			_ = m.Within(ctx, func(context.Context) error { panic("inner") })
		}()
		return nil
	})
	if !errors.Is(err, transaction.ErrRollbackOnly) || b.tx.rollbacks != 1 || b.tx.commits != 0 {
		t.Fatalf("recovered panic committed: %v", err)
	}
}

func TestDifferentManagerAndCancelledBegin(t *testing.T) {
	b := &backend{tx: &resource{}}
	m := manager(t, b)
	otherBackend := &backend{tx: &resource{}}
	other := manager(t, otherBackend)
	err := m.Within(context.Background(), func(ctx context.Context) error {
		_ = other.Within(ctx, func(context.Context) error { t.Fatal("foreign callback ran"); return nil })
		return nil
	})
	if !errors.Is(err, transaction.ErrDifferentManager) || otherBackend.begins != 0 || b.tx.commits != 0 {
		t.Fatalf("foreign manager: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := other.Within(cancelled, func(context.Context) error { return nil }); !errors.Is(err, context.Canceled) || otherBackend.begins != 0 {
		t.Fatalf("cancelled begin: %v", err)
	}
}

func TestBeginFailureAndDoResult(t *testing.T) {
	cause := errors.New("begin failed")
	b := &backend{tx: &resource{}, beginErr: cause}
	m := manager(t, b)
	if err := m.Within(context.Background(), func(context.Context) error { t.Fatal("callback ran"); return nil }); !errors.Is(err, cause) || b.tx.commits != 0 || b.tx.rollbacks != 0 {
		t.Fatalf("begin: %v", err)
	}
	b.beginErr = nil
	b.tx.commitErr = cause
	value, err := transaction.Do(context.Background(), m, func(context.Context) (string, error) { return "uncommitted", nil })
	if value != "" || !errors.Is(err, cause) {
		t.Fatalf("result escaped failed commit: %q %v", value, err)
	}
}

func TestCancellationCauseIsPreserved(t *testing.T) {
	b := &backend{tx: &resource{}}
	m := manager(t, b)
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("request superseded")
	err := m.Within(ctx, func(context.Context) error { cancel(cause); return nil })
	if !errors.Is(err, context.Canceled) || !errors.Is(err, cause) || b.tx.rollbacks != 1 || b.tx.commits != 0 {
		t.Fatalf("cancellation: %v", err)
	}
}

type isolatedBackend struct {
	mu        sync.Mutex
	resources []*resource
}

func (b *isolatedBackend) Begin(context.Context) (transaction.Transaction, error) {
	resource := &resource{}
	b.mu.Lock()
	b.resources = append(b.resources, resource)
	b.mu.Unlock()
	return resource, nil
}
func TestConcurrentRequestsHaveIndependentScopes(t *testing.T) {
	b := &isolatedBackend{}
	m, err := transaction.New(b, transaction.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	seen := sync.Map{}
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			parent := context.WithValue(context.Background(), valueKey{}, i)
			err := m.Within(parent, func(ctx context.Context) error {
				resource, err := transaction.Current(ctx)
				if err != nil {
					return err
				}
				if _, loaded := seen.LoadOrStore(resource, i); loaded {
					t.Error("request reused another transaction")
				}
				return m.Within(ctx, func(ctx context.Context) error {
					nested, err := transaction.Current(ctx)
					if err != nil {
						return err
					}
					if nested != resource || ctx.Value(valueKey{}) != i {
						t.Error("nested request lost its scope")
					}
					return nil
				})
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(b.resources) != 16 {
		t.Fatalf("transactions: %d", len(b.resources))
	}
	for _, resource := range b.resources {
		if resource.commits != 1 || resource.rollbacks != 0 {
			t.Fatalf("resource: %+v", resource)
		}
	}
}

func TestCancellationDuringBeginDoesNotRunCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &backend{tx: &resource{}, onBegin: cancel}
	m := manager(t, b)
	err := m.Within(ctx, func(context.Context) error { t.Fatal("callback ran after cancellation"); return nil })
	if !errors.Is(err, context.Canceled) || b.tx.rollbacks != 1 || b.tx.commits != 0 {
		t.Fatalf("cancelled begin: %v", err)
	}
}

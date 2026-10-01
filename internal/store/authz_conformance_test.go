package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/store"
)

func TestConformanceAuthzGeneration(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()

		if got, err := st.AuthzGeneration(ctx); err != nil || got != 0 {
			t.Fatalf("a new database is at generation %d, %v, want 0", got, err)
		}
		if err := st.BumpAuthzGeneration(ctx); err != nil {
			t.Fatal(err)
		}
		errRollback := errors.New("rollback")
		err := st.WithTx(ctx, func(ctx context.Context, tx store.Store) error {
			if err := tx.BumpAuthzGeneration(ctx); err != nil {
				return err
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatal(err)
		}
		if got, err := st.AuthzGeneration(ctx); err != nil || got != 1 {
			t.Errorf("generation %d, %v, want 1: a rolled-back bump must not count", got, err)
		}
	})
}

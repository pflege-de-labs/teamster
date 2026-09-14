package store

import (
	"context"
	"fmt"
)

// CreateSchemaForTest and DropSchemaForTest give a Postgres test a namespace of
// its own, which is what lets the conformance suite keep t.Parallel() against
// one server. They live here rather than in a _test.go file because the suite
// that needs them is an external test package.
//
// The identifier is built by the caller from a timestamp and a random number,
// never from anything a user supplies.
func CreateSchemaForTest(ctx context.Context, st Store, schema string) error {
	pg, ok := st.(*PostgresStore)
	if !ok {
		return fmt.Errorf("not a postgres store")
	}
	_, err := pg.db.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %q", schema))
	return err
}

func DropSchemaForTest(ctx context.Context, st Store, schema string) error {
	pg, ok := st.(*PostgresStore)
	if !ok {
		return fmt.Errorf("not a postgres store")
	}
	_, err := pg.db.ExecContext(ctx, fmt.Sprintf("DROP SCHEMA %q CASCADE", schema))
	return err
}

package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// isPrimaryKeyConflict reports whether err is the specific constraint the two
// backends raise for a colliding primary key or unique index -- not any
// constraint violation, because a caller retrying past a check it actually
// wanted enforced (a required column, a foreign key) would loop forever
// instead of failing.
func isPrimaryKeyConflict(err error) bool {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY, sqlite3.SQLITE_CONSTRAINT_UNIQUE:
			return true
		}
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

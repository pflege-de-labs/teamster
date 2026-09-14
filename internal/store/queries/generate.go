// Package queries holds the SQL this service runs, one directory per dialect.
// The SQLite files are the source; the Postgres ones are generated from them.
package queries

//go:generate go run ./gen

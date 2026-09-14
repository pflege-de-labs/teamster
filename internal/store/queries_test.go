package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// sqlc computes byte offsets when it slices a statement out of its file, and
// miscounts them when a preceding comment carries a multi-byte character: the
// tail of the statement is dropped, by as many bytes as the comment was over
// ASCII. The result still compiles and often still parses — `AND claimed_at <= ?`
// truncated to `AND claimed_at` is a valid truthiness test — so nothing catches
// it but a reader who happens to look at the generated SQL.
//
// The trigger is exactly "a non-ASCII byte in a .sql file", so that is what
// this refuses. Prose that wants an em dash can have one in the Go.
func TestQueryFilesAreASCII(t *testing.T) {
	t.Parallel()

	matches, err := filepath.Glob(filepath.Join("queries", "*", "*.sql"))
	if err != nil {
		t.Fatalf("glob queries: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no query files found, so this proves nothing")
	}

	for _, path := range matches {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			for offset, r := range string(body) {
				if r >= utf8.RuneSelf {
					line := 1 + strings.Count(string(body[:offset]), "\n")
					t.Errorf("line %d holds %q; sqlc truncates statements after a multi-byte character", line, r)
				}
			}
		})
	}
}

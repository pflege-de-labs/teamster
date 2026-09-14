// Command gen writes the Postgres query files from the SQLite ones.
//
// The two dialects run the same statements. SQLite copied Postgres's
// ON CONFLICT ... excluded form and has had RETURNING since 3.35, so across
// all of them the only difference is how a parameter is spelled: ? there,
// $1..$n here, numbered per statement.
//
// Generating rather than maintaining two copies makes drift impossible instead
// of merely detectable. If a statement ever has to differ for real, take its
// file out of this generator and say why in a comment — a hand-written file
// beside generated ones is fine, silently diverging copies are not.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const header = `-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

`

// A parameter is a bare ?. Nothing else in these files uses the character —
// there is no LIKE pattern and no question mark in a comment — and the
// generator checks that assumption rather than trusting it.
var parameter = regexp.MustCompile(`\?`)

func main() {
	if err := run(); err != nil {
		log.Fatalf("gen: %v", err)
	}
}

func run() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	src := filepath.Join(root, "sqlite")
	dst := filepath.Join(root, "postgres")

	entries, err := filepath.Glob(filepath.Join(src, "*.sql"))
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no SQLite queries in %s", src)
	}

	for _, path := range entries {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, err := translate(string(body))
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		target := filepath.Join(dst, filepath.Base(path))
		if err := os.WriteFile(target, []byte(header+out), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// translate numbers the parameters of each statement from one. Statements are
// split on the semicolon, which is safe here because none of these queries
// contains one inside a literal.
func translate(body string) (string, error) {
	if strings.Contains(body, "$") {
		return "", fmt.Errorf("a SQLite query already uses $, which this generator would renumber")
	}

	statements := strings.Split(body, ";")
	for i, statement := range statements {
		n := 0
		statements[i] = parameter.ReplaceAllStringFunc(statement, func(string) string {
			n++
			return fmt.Sprintf("$%d", n)
		})
	}
	return strings.Join(statements, ";"), nil
}

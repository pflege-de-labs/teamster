package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// eventsVersion is the migration that renames alerts to events (ADR 0056).
var eventsVersion = map[Dialect]int64{SQLite: 19, Postgres: 16}

// oldPreset is a preset as the release before events stored it; the fixture is
// what cards.Presets returned then.
type oldPreset struct {
	Key, Title, Text, Body string
	Sources                []string
}

func readOldPresets(t *testing.T) []oldPreset {
	t.Helper()
	return readPresets(t, "testdata/presets_before_events.json")
}

// readMigratedPresets is cards.Presets as this migration shipped: presets
// change later, and only seed new installations (ADR 0055).
func readMigratedPresets(t *testing.T) []oldPreset {
	t.Helper()
	return readPresets(t, "testdata/presets_after_events.json")
}

func readPresets(t *testing.T, path string) []oldPreset {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var presets []oldPreset
	if err := json.Unmarshal(raw, &presets); err != nil {
		t.Fatal(err)
	}
	return presets
}

// migrationDBs opens one empty database per dialect this machine can test.
func migrationDBs(t *testing.T) map[Dialect]*sql.DB {
	t.Helper()

	dbs := map[Dialect]*sql.DB{SQLite: openDB(t, "events.db")}
	dsn := os.Getenv("TEAMSTER_TEST_POSTGRES_DSN")
	if dsn == "" {
		return dbs
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	schema := fmt.Sprintf("m_%d_%d", time.Now().UnixNano(), rand.N(1000))
	if _, err := admin.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close()
	})
	db, err := sql.Open("pgx", dsn+"&search_path="+schema)
	if err != nil {
		t.Fatalf("open postgres schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dbs[Postgres] = db
	return dbs
}

// placeholders turns ? into $n for Postgres.
func placeholders(dialect Dialect, query string) string {
	if dialect != Postgres {
		return query
	}
	var out strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			fmt.Fprintf(&out, "$%d", n)
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// userTemplate is a template somebody wrote against the old API, with what the
// migration should make of it.
type userTemplate struct {
	id, sources, before, after string
}

var userTemplates = []userTemplate{
	{
		id: "universal-only", sources: "universal",
		before: `{{ .Alert.Annotations.summary }} at {{ .Alert.StartsAt }} via {{ .Alert.Generator }}`,
		after:  `{{ .Event.Universal.Attributes.summary }} at {{ .Event.Universal.Time }} via {{ .Event.Universal.URL }}`,
	},
	{
		id: "any-source", sources: "",
		before: `{{ if ne .Alert.Status "firing" }}{{ toJSON .Alert }}{{ end }}{{ $.Alert.Fingerprint }}{{ .Alert.EndsAt }}`,
		after:  `{{ if ne .Event.State "open" }}{{ toJSON .Event }}{{ end }}{{ $.Event.Key }}{{ .Event.Alertmanager.EndsAt }}`,
	},
}

func TestEventsMigrationRewritesStateAndTemplates(t *testing.T) {
	t.Parallel()

	for dialect, db := range migrationDBs(t) {
		t.Run(string(dialect), func(t *testing.T) {
			ctx := t.Context()
			provider, err := New(dialect, db)
			if err != nil {
				t.Fatalf("provider: %v", err)
			}
			before := eventsVersion[dialect] - 1
			if _, err := provider.UpTo(ctx, before); err != nil {
				t.Fatalf("up to %d: %v", before, err)
			}
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.ExecContext(ctx, placeholders(dialect, query), args...); err != nil {
					t.Fatalf("%s: %v", query, err)
				}
			}
			now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

			exec(`INSERT INTO active_alerts (fingerprint, status, team_id, channel_id, message_id, claim_owner, posted_at, last_update)
				VALUES ('fp', 'firing', 't', 'c', 'm', '', ?, ?)`, now, now)
			exec(`INSERT INTO active_alert_recipients (fingerprint, status, recipient_id, message_id, claim_owner, posted_at, last_update)
				VALUES ('fp', 'resolved', 'r', 'm', '', ?, ?)`, now, now)
			exec(`INSERT INTO alert_samples (kind, key, value, seen_count, first_seen, last_seen) VALUES ('annotation', 'summary', '', 1, ?, ?)`, now, now)
			exec(`INSERT INTO alert_samples (kind, key, value, seen_count, first_seen, last_seen) VALUES ('label', 'env', 'prod', 1, ?, ?)`, now, now)
			old := readOldPresets(t)
			for _, p := range old {
				exec(`INSERT INTO templates (id, name, title, message_text, body, sources, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					p.Key, p.Key, p.Title, p.Text, p.Body, strings.Join(p.Sources, ","), now, now)
			}
			for _, u := range userTemplates {
				exec(`INSERT INTO templates (id, name, title, message_text, body, sources, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					u.id, u.id, u.before, u.before, u.before, u.sources, now, now)
			}

			if _, err := provider.UpTo(ctx, eventsVersion[dialect]); err != nil {
				t.Fatalf("up: %v", err)
			}

			var key, state, kind string
			if err := db.QueryRowContext(ctx, `SELECT event_key, state FROM active_events`).Scan(&key, &state); err != nil {
				t.Fatalf("read active_events: %v", err)
			}
			if key != "fp" || state != "open" {
				t.Errorf("active_events = (%q, %q), want (fp, open)", key, state)
			}
			if err := db.QueryRowContext(ctx, `SELECT state FROM active_event_recipients`).Scan(&state); err != nil {
				t.Fatalf("read active_event_recipients: %v", err)
			}
			if state != "closed" {
				t.Errorf("recipient state = %q, want closed", state)
			}
			if err := db.QueryRowContext(ctx, `SELECT kind FROM event_samples WHERE key = 'summary'`).Scan(&kind); err != nil {
				t.Fatalf("read event_samples: %v", err)
			}
			if kind != "attribute" {
				t.Errorf("sample kind = %q, want attribute", kind)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO event_samples (kind, key, value, seen_count, first_seen, last_seen) VALUES ('annotation', 'x', '', 1, '2026-01-01', '2026-01-01')`); err == nil {
				t.Error("event_samples accepted the old annotation kind")
			}

			readTemplate := func(id string) (title, text, body string) {
				t.Helper()
				if err := db.QueryRowContext(ctx, placeholders(dialect, `SELECT title, message_text, body FROM templates WHERE id = ?`), id).Scan(&title, &text, &body); err != nil {
					t.Fatalf("read template %s: %v", id, err)
				}
				return title, text, body
			}
			// The Go presets and the SQL rewrite had to agree, or a new
			// installation and an upgraded one would have differed.
			for _, p := range readMigratedPresets(t) {
				title, text, body := readTemplate(p.Key)
				if title != p.Title || text != p.Text || body != p.Body {
					t.Errorf("preset %s after the migration differs from the presets it shipped with:\n title %q\n want  %q\n text  %q\n want  %q\n body  %q\n want  %q",
						p.Key, title, p.Title, text, p.Text, body, p.Body)
				}
			}
			for _, u := range userTemplates {
				if title, _, _ := readTemplate(u.id); title != u.after {
					t.Errorf("template %s = %q, want %q", u.id, title, u.after)
				}
			}

			if _, err := provider.DownTo(ctx, before); err != nil {
				t.Fatalf("down: %v", err)
			}
			if err := db.QueryRowContext(ctx, `SELECT fingerprint, status FROM active_alerts`).Scan(&key, &state); err != nil {
				t.Fatalf("read active_alerts after down: %v", err)
			}
			if state != "firing" {
				t.Errorf("status after down = %q, want firing", state)
			}
			if err := db.QueryRowContext(ctx, `SELECT kind FROM alert_samples WHERE key = 'summary'`).Scan(&kind); err != nil {
				t.Fatalf("read alert_samples after down: %v", err)
			}
			if kind != "annotation" {
				t.Errorf("sample kind after down = %q, want annotation", kind)
			}
			for _, p := range old {
				title, text, body := readTemplate(p.Key)
				if title != p.Title || text != p.Text || body != p.Body {
					t.Errorf("preset %s does not round-trip through down:\n title %q\n want  %q\n body  %q\n want  %q", p.Key, title, p.Title, body, p.Body)
				}
			}
			for _, u := range userTemplates {
				if title, _, _ := readTemplate(u.id); title != u.before {
					t.Errorf("template %s after down = %q, want %q", u.id, title, u.before)
				}
			}
		})
	}
}

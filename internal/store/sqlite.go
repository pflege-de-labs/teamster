package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/pflege-de-labs/teamster/internal/models"
)

type SQLiteStore struct {
	db   *sql.DB
	path string
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &SQLiteStore{db: db, path: path}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS templates (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	message_text TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS destinations (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS routes (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	parent_id TEXT NOT NULL DEFAULT '',
	greedy INTEGER NOT NULL DEFAULT 0,
	label_selector TEXT NOT NULL,
	destination_id TEXT NOT NULL,
	template_id TEXT NOT NULL,
	is_default INTEGER NOT NULL,
	priority INTEGER NOT NULL,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	subject TEXT NOT NULL,
	name TEXT NOT NULL,
	source TEXT NOT NULL,
	role TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL,
	expires_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS login_flows (
	state TEXT PRIMARY KEY,
	verifier TEXT NOT NULL,
	nonce TEXT NOT NULL,
	expires_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS active_alerts (
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	last_update DATETIME NOT NULL,
	PRIMARY KEY (fingerprint, team_id, channel_id)
);
`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	if err := s.addMissingColumns(); err != nil {
		return err
	}
	if err := s.rekeyActiveAlerts(); err != nil {
		return err
	}
	return s.checkTimestampColumns()
}

// addedColumns are columns a later release introduced. CREATE TABLE IF NOT
// EXISTS leaves an existing table alone, so a database created before them
// needs them added rather than the whole schema replayed.
var addedColumns = []struct {
	table, column, definition string
}{
	{"templates", "title", "TEXT NOT NULL DEFAULT ''"},
	{"templates", "message_text", "TEXT NOT NULL DEFAULT ''"},
	{"routes", "parent_id", "TEXT NOT NULL DEFAULT ''"},
	{"routes", "greedy", "INTEGER NOT NULL DEFAULT 0"},
	{"sessions", "role", "TEXT NOT NULL DEFAULT ''"},
}

// A template written before this migration is card-only, and renders the title
// it always had: templates.DefaultTitle fills in for an empty one.
func (s *SQLiteStore) addMissingColumns() error {
	for _, add := range addedColumns {
		declared, err := s.columnTypes(add.table)
		if err != nil {
			return err
		}
		if _, ok := declared[add.column]; ok {
			continue
		}
		if _, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", add.table, add.column, add.definition)); err != nil {
			return fmt.Errorf("add %s.%s: %w", add.table, add.column, err)
		}
	}
	return nil
}

// timestampColumns are read back as time.Time only while they are declared
// DATETIME; a database written before that fix silently fails every Scan.
var timestampColumns = map[string][]string{
	"templates":     {"created_at", "updated_at"},
	"destinations":  {"created_at", "updated_at"},
	"routes":        {"created_at", "updated_at"},
	"active_alerts": {"last_update"},
	"sessions":      {"created_at", "expires_at"},
	"login_flows":   {"expires_at"},
}

func (s *SQLiteStore) checkTimestampColumns() error {
	for table, columns := range timestampColumns {
		declared, err := s.columnTypes(table)
		if err != nil {
			return err
		}
		for _, column := range columns {
			if got := strings.ToUpper(declared[column]); got != "" && got != "DATETIME" {
				return fmt.Errorf("%s: table %s column %s is declared %s, expected DATETIME; "+
					"this database was created by an older build and cannot be read, delete it and restart",
					s.path, table, column, got)
			}
		}
	}
	return nil
}

// An alert that fans out has one card per channel, so active_alerts is keyed by
// the channel as well as the fingerprint. A database written before that has the
// old single-column key, which SQLite can only change by rebuilding the table —
// the rows survive, because one card per alert is a valid fan-out of one.
func (s *SQLiteStore) rekeyActiveAlerts() error {
	key, err := s.primaryKey("active_alerts")
	if err != nil {
		return err
	}
	if len(key) != 1 || key[0] != "fingerprint" {
		return nil
	}

	// In one transaction: a rebuild interrupted half way would otherwise leave
	// the scratch table behind, and every later start would fail trying to
	// create it again. SQLite makes DDL transactional, so this rolls back whole.
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("rekey active alerts: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
CREATE TABLE active_alerts_rekeyed (
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	last_update DATETIME NOT NULL,
	PRIMARY KEY (fingerprint, team_id, channel_id)
);
INSERT INTO active_alerts_rekeyed SELECT fingerprint, status, team_id, channel_id, message_id, last_update FROM active_alerts;
DROP TABLE active_alerts;
ALTER TABLE active_alerts_rekeyed RENAME TO active_alerts;`)
	if err != nil {
		return fmt.Errorf("rekey active alerts: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rekey active alerts: %w", err)
	}
	return nil
}

// primaryKey returns the key columns in key order, which is what distinguishes
// the rebuilt active_alerts table from the one that preceded it.
func (s *SQLiteStore) primaryKey(table string) ([]string, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	type keyColumn struct {
		name     string
		position int
	}
	var key []keyColumn
	for rows.Next() {
		var (
			cid        int
			name, kind string
			notNull    int
			dflt       sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &kind, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scan %s column: %w", table, err)
		}
		if pk > 0 {
			key = append(key, keyColumn{name: name, position: pk})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(key, func(i, j int) bool { return key[i].position < key[j].position })
	names := make([]string, 0, len(key))
	for _, column := range key {
		names = append(names, column.name)
	}
	return names, nil
}

func (s *SQLiteStore) columnTypes(table string) (map[string]string, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	types := map[string]string{}
	for rows.Next() {
		var (
			cid        int
			name, kind string
			notNull    int
			dflt       sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &kind, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scan %s column: %w", table, err)
		}
		types[name] = kind
	}
	return types, rows.Err()
}

func (s *SQLiteStore) ListTemplates() ([]models.Template, error) {
	rows, err := s.db.Query(`SELECT id, name, title, message_text, body, created_at, updated_at FROM templates ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []models.Template
	for rows.Next() {
		var t models.Template
		if err := rows.Scan(&t.ID, &t.Name, &t.Title, &t.Text, &t.Body, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan template: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) CreateTemplate(t models.Template) (models.Template, error) {
	now := time.Now().UTC()
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	t.CreatedAt = now
	t.UpdatedAt = now

	_, err := s.db.Exec(`INSERT INTO templates (id, name, title, message_text, body, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Title, t.Text, t.Body, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return models.Template{}, fmt.Errorf("create template: %w", err)
	}
	return t, nil
}

func (s *SQLiteStore) UpdateTemplate(t models.Template) (models.Template, error) {
	if t.ID == "" {
		return models.Template{}, errors.New("template id is required")
	}
	t.UpdatedAt = time.Now().UTC()

	_, err := s.db.Exec(`UPDATE templates SET name = ?, title = ?, message_text = ?, body = ?, updated_at = ? WHERE id = ?`,
		t.Name, t.Title, t.Text, t.Body, t.UpdatedAt, t.ID)
	if err != nil {
		return models.Template{}, fmt.Errorf("update template: %w", err)
	}
	return t, nil
}

func (s *SQLiteStore) DeleteTemplate(id string) error {
	_, err := s.db.Exec(`DELETE FROM templates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetTemplate(id string) (models.Template, error) {
	var t models.Template
	err := s.db.QueryRow(`SELECT id, name, title, message_text, body, created_at, updated_at FROM templates WHERE id = ?`, id).
		Scan(&t.ID, &t.Name, &t.Title, &t.Text, &t.Body, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Template{}, ErrNotFound
		}
		return models.Template{}, fmt.Errorf("get template: %w", err)
	}
	return t, nil
}

func (s *SQLiteStore) ListDestinations() ([]models.Destination, error) {
	rows, err := s.db.Query(`SELECT id, name, team_id, channel_id, created_at, updated_at FROM destinations ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list destinations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []models.Destination
	for rows.Next() {
		var d models.Destination
		if err := rows.Scan(&d.ID, &d.Name, &d.TeamID, &d.ChannelID, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan destination: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) CreateDestination(d models.Destination) (models.Destination, error) {
	now := time.Now().UTC()
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	d.CreatedAt = now
	d.UpdatedAt = now

	_, err := s.db.Exec(`INSERT INTO destinations (id, name, team_id, channel_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		d.ID, d.Name, d.TeamID, d.ChannelID, d.CreatedAt, d.UpdatedAt)
	if err != nil {
		return models.Destination{}, fmt.Errorf("create destination: %w", err)
	}
	return d, nil
}

func (s *SQLiteStore) UpdateDestination(d models.Destination) (models.Destination, error) {
	if d.ID == "" {
		return models.Destination{}, errors.New("destination id is required")
	}
	d.UpdatedAt = time.Now().UTC()

	_, err := s.db.Exec(`UPDATE destinations SET name = ?, team_id = ?, channel_id = ?, updated_at = ? WHERE id = ?`,
		d.Name, d.TeamID, d.ChannelID, d.UpdatedAt, d.ID)
	if err != nil {
		return models.Destination{}, fmt.Errorf("update destination: %w", err)
	}
	return d, nil
}

func (s *SQLiteStore) DeleteDestination(id string) error {
	_, err := s.db.Exec(`DELETE FROM destinations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete destination: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetDestination(id string) (models.Destination, error) {
	var d models.Destination
	err := s.db.QueryRow(`SELECT id, name, team_id, channel_id, created_at, updated_at FROM destinations WHERE id = ?`, id).
		Scan(&d.ID, &d.Name, &d.TeamID, &d.ChannelID, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Destination{}, ErrNotFound
		}
		return models.Destination{}, fmt.Errorf("get destination: %w", err)
	}
	return d, nil
}

func (s *SQLiteStore) ListRoutes() ([]models.Route, error) {
	rows, err := s.db.Query(`SELECT id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at FROM routes ORDER BY priority DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []models.Route
	for rows.Next() {
		var (
			r            models.Route
			selectorJSON string
			isDefault    int
			greedy       int
		)
		if err := rows.Scan(&r.ID, &r.Name, &r.ParentID, &greedy, &selectorJSON, &r.DestinationID, &r.TemplateID, &isDefault, &r.Priority, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan route: %w", err)
		}
		r.LabelSelector = parseSelector(selectorJSON)
		r.IsDefault = isDefault == 1
		r.Greedy = greedy == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) CreateRoute(r models.Route) (models.Route, error) {
	now := time.Now().UTC()
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.CreatedAt = now
	r.UpdatedAt = now

	selectorJSON := serializeSelector(r.LabelSelector)

	_, err := s.db.Exec(`INSERT INTO routes (id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Name, r.ParentID, boolToInt(r.Greedy), selectorJSON, r.DestinationID, r.TemplateID, boolToInt(r.IsDefault), r.Priority, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return models.Route{}, fmt.Errorf("create route: %w", err)
	}
	return r, nil
}

func (s *SQLiteStore) UpdateRoute(r models.Route) (models.Route, error) {
	if r.ID == "" {
		return models.Route{}, errors.New("route id is required")
	}
	r.UpdatedAt = time.Now().UTC()

	selectorJSON := serializeSelector(r.LabelSelector)

	_, err := s.db.Exec(`UPDATE routes SET name = ?, parent_id = ?, greedy = ?, label_selector = ?, destination_id = ?, template_id = ?, is_default = ?, priority = ?, updated_at = ? WHERE id = ?`,
		r.Name, r.ParentID, boolToInt(r.Greedy), selectorJSON, r.DestinationID, r.TemplateID, boolToInt(r.IsDefault), r.Priority, r.UpdatedAt, r.ID)
	if err != nil {
		return models.Route{}, fmt.Errorf("update route: %w", err)
	}
	return r, nil
}

func (s *SQLiteStore) DeleteRoute(id string) error {
	_, err := s.db.Exec(`DELETE FROM routes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete route: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetRoute(id string) (models.Route, error) {
	var (
		r            models.Route
		selectorJSON string
		isDefault    int
		greedy       int
	)
	row := s.db.QueryRow(`SELECT id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at FROM routes WHERE id = ?`, id)
	if err := row.Scan(&r.ID, &r.Name, &r.ParentID, &greedy, &selectorJSON, &r.DestinationID, &r.TemplateID, &isDefault, &r.Priority, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Route{}, ErrNotFound
		}
		return models.Route{}, fmt.Errorf("get route: %w", err)
	}
	r.LabelSelector = parseSelector(selectorJSON)
	r.IsDefault = isDefault == 1
	r.Greedy = greedy == 1
	return r, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *SQLiteStore) UpsertActiveAlert(a models.ActiveAlert) error {
	_, err := s.db.Exec(`INSERT INTO active_alerts (fingerprint, status, team_id, channel_id, message_id, last_update)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(fingerprint, team_id, channel_id) DO UPDATE SET status = excluded.status, message_id = excluded.message_id, last_update = excluded.last_update`,
		a.Fingerprint, a.Status, a.TeamID, a.ChannelID, a.MessageID, a.LastUpdate)
	if err != nil {
		return fmt.Errorf("upsert active alert: %w", err)
	}
	return nil
}

// ListActiveAlerts returns every card posted for an alert, one per channel it
// fanned out to. Ordering is stable so that delivery, and its tests, see the
// cards in the same order every time.
func (s *SQLiteStore) ListActiveAlerts(fingerprint string) ([]models.ActiveAlert, error) {
	rows, err := s.db.Query(`SELECT fingerprint, status, team_id, channel_id, message_id, last_update FROM active_alerts WHERE fingerprint = ? ORDER BY team_id, channel_id`, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("list active alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []models.ActiveAlert
	for rows.Next() {
		var a models.ActiveAlert
		if err := rows.Scan(&a.Fingerprint, &a.Status, &a.TeamID, &a.ChannelID, &a.MessageID, &a.LastUpdate); err != nil {
			return nil, fmt.Errorf("scan active alert: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetActiveAlert(fingerprint, teamID, channelID string) (models.ActiveAlert, error) {
	var a models.ActiveAlert
	err := s.db.QueryRow(`SELECT fingerprint, status, team_id, channel_id, message_id, last_update FROM active_alerts WHERE fingerprint = ? AND team_id = ? AND channel_id = ?`, fingerprint, teamID, channelID).
		Scan(&a.Fingerprint, &a.Status, &a.TeamID, &a.ChannelID, &a.MessageID, &a.LastUpdate)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ActiveAlert{}, ErrNotFound
		}
		return models.ActiveAlert{}, fmt.Errorf("get active alert: %w", err)
	}
	return a, nil
}

func (s *SQLiteStore) DeleteActiveAlert(fingerprint, teamID, channelID string) error {
	_, err := s.db.Exec(`DELETE FROM active_alerts WHERE fingerprint = ? AND team_id = ? AND channel_id = ?`, fingerprint, teamID, channelID)
	if err != nil {
		return fmt.Errorf("delete active alert: %w", err)
	}
	return nil
}

func serializeSelector(selector map[string]string) string {
	if selector == nil {
		return "{}"
	}
	data, err := json.Marshal(selector)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func parseSelector(raw string) map[string]string {
	if raw == "" {
		return map[string]string{}
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]string{}
	}
	if out == nil {
		return map[string]string{}
	}
	return out
}

func (s *SQLiteStore) CreateSession(session models.Session) error {
	_, err := s.db.Exec(`INSERT INTO sessions (id, subject, name, source, role, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.Subject, session.Name, session.Source, session.Roles, session.CreatedAt, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// An expired session is reported as missing, so a caller cannot accidentally
// honour one by forgetting to check the time.
func (s *SQLiteStore) GetSession(id string) (models.Session, error) {
	var session models.Session
	err := s.db.QueryRow(`SELECT id, subject, name, source, role, created_at, expires_at FROM sessions WHERE id = ?`, id).
		Scan(&session.ID, &session.Subject, &session.Name, &session.Source, &session.Roles, &session.CreatedAt, &session.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Session{}, ErrNotFound
		}
		return models.Session{}, fmt.Errorf("get session: %w", err)
	}
	if !session.ExpiresAt.After(time.Now()) {
		return models.Session{}, ErrNotFound
	}
	return session, nil
}

func (s *SQLiteStore) DeleteSession(id string) error {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteExpiredSessions() error {
	now := time.Now()
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, now); err != nil {
		return fmt.Errorf("sweep sessions: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM login_flows WHERE expires_at <= ?`, now); err != nil {
		return fmt.Errorf("sweep login flows: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateLoginFlow(flow models.LoginFlow) error {
	_, err := s.db.Exec(`INSERT INTO login_flows (state, verifier, nonce, expires_at) VALUES (?, ?, ?, ?)`,
		flow.State, flow.Verifier, flow.Nonce, flow.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create login flow: %w", err)
	}
	return nil
}

// Taking the flow deletes it: a state may be redeemed once, so a replayed
// callback finds nothing.
func (s *SQLiteStore) TakeLoginFlow(state string) (models.LoginFlow, error) {
	var flow models.LoginFlow
	err := s.db.QueryRow(`DELETE FROM login_flows WHERE state = ? RETURNING state, verifier, nonce, expires_at`, state).
		Scan(&flow.State, &flow.Verifier, &flow.Nonce, &flow.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.LoginFlow{}, ErrNotFound
		}
		return models.LoginFlow{}, fmt.Errorf("take login flow: %w", err)
	}
	if !flow.ExpiresAt.After(time.Now()) {
		return models.LoginFlow{}, ErrNotFound
	}
	return flow, nil
}

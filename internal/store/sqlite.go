package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store/migrations"
)

// A queryer is whatever the statements run against: the database, or a
// transaction while one is open. Every method uses it, so the same code serves
// both and an import can be applied as a unit.
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type SQLiteStore struct {
	// db is nil inside a transaction: there is nothing there to close, to ping,
	// or to begin a second transaction on.
	db   *sql.DB
	sql  queryer
	path string
}

// NewSQLiteStore opens the file and brings the schema to the version this
// build expects, or checks that somebody else already did — see MigrateMode.
func NewSQLiteStore(ctx context.Context, path string, migrate MigrateMode) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &SQLiteStore{db: db, sql: db, path: path}
	if err := applyMigrations(ctx, migrations.SQLite, db, migrate); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	if s.db == nil {
		return errors.New("cannot close the store inside a transaction")
	}
	return s.db.Close()
}

// WithTx runs fn against a store bound to one transaction, committing when it
// returns nil and rolling back otherwise. It is what lets an import that turns
// out to be invalid half way through leave the configuration as it found it.
func (s *SQLiteStore) WithTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error {
	if s.db == nil {
		return errors.New("already in a transaction")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// A panic must not leave the transaction open, and it must keep travelling.
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := fn(ctx, &SQLiteStore{sql: tx, path: s.path}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

// Ping is a round trip to the database rather than a look at a connection
// struct: a file that has been deleted or a disk that has gone read-only shows
// up here and nowhere else.
func (s *SQLiteStore) Ping(ctx context.Context) error {
	if s.db == nil {
		return errors.New("cannot ping inside a transaction")
	}
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListTemplates(ctx context.Context) ([]models.Template, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT id, name, title, message_text, body, created_at, updated_at FROM templates ORDER BY name`)
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

func (s *SQLiteStore) CreateTemplate(ctx context.Context, t models.Template) (models.Template, error) {
	now := time.Now().UTC()
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	t.CreatedAt = now
	t.UpdatedAt = now

	_, err := s.sql.ExecContext(ctx, `INSERT INTO templates (id, name, title, message_text, body, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Title, t.Text, t.Body, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return models.Template{}, fmt.Errorf("create template: %w", err)
	}
	return t, nil
}

func (s *SQLiteStore) UpdateTemplate(ctx context.Context, t models.Template) (models.Template, error) {
	if t.ID == "" {
		return models.Template{}, errors.New("template id is required")
	}
	t.UpdatedAt = time.Now().UTC()

	_, err := s.sql.ExecContext(ctx, `UPDATE templates SET name = ?, title = ?, message_text = ?, body = ?, updated_at = ? WHERE id = ?`,
		t.Name, t.Title, t.Text, t.Body, t.UpdatedAt, t.ID)
	if err != nil {
		return models.Template{}, fmt.Errorf("update template: %w", err)
	}
	return t, nil
}

func (s *SQLiteStore) DeleteTemplate(ctx context.Context, id string) error {
	_, err := s.sql.ExecContext(ctx, `DELETE FROM templates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetTemplate(ctx context.Context, id string) (models.Template, error) {
	var t models.Template
	err := s.sql.QueryRowContext(ctx, `SELECT id, name, title, message_text, body, created_at, updated_at FROM templates WHERE id = ?`, id).
		Scan(&t.ID, &t.Name, &t.Title, &t.Text, &t.Body, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Template{}, ErrNotFound
		}
		return models.Template{}, fmt.Errorf("get template: %w", err)
	}
	return t, nil
}

func (s *SQLiteStore) ListDestinations(ctx context.Context) ([]models.Destination, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT id, name, team_id, channel_id, created_at, updated_at FROM destinations ORDER BY name`)
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

func (s *SQLiteStore) CreateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	now := time.Now().UTC()
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	d.CreatedAt = now
	d.UpdatedAt = now

	_, err := s.sql.ExecContext(ctx, `INSERT INTO destinations (id, name, team_id, channel_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		d.ID, d.Name, d.TeamID, d.ChannelID, d.CreatedAt, d.UpdatedAt)
	if err != nil {
		return models.Destination{}, fmt.Errorf("create destination: %w", err)
	}
	return d, nil
}

func (s *SQLiteStore) UpdateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	if d.ID == "" {
		return models.Destination{}, errors.New("destination id is required")
	}
	d.UpdatedAt = time.Now().UTC()

	_, err := s.sql.ExecContext(ctx, `UPDATE destinations SET name = ?, team_id = ?, channel_id = ?, updated_at = ? WHERE id = ?`,
		d.Name, d.TeamID, d.ChannelID, d.UpdatedAt, d.ID)
	if err != nil {
		return models.Destination{}, fmt.Errorf("update destination: %w", err)
	}
	return d, nil
}

func (s *SQLiteStore) DeleteDestination(ctx context.Context, id string) error {
	_, err := s.sql.ExecContext(ctx, `DELETE FROM destinations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete destination: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetDestination(ctx context.Context, id string) (models.Destination, error) {
	var d models.Destination
	err := s.sql.QueryRowContext(ctx, `SELECT id, name, team_id, channel_id, created_at, updated_at FROM destinations WHERE id = ?`, id).
		Scan(&d.ID, &d.Name, &d.TeamID, &d.ChannelID, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Destination{}, ErrNotFound
		}
		return models.Destination{}, fmt.Errorf("get destination: %w", err)
	}
	return d, nil
}

func (s *SQLiteStore) ListRoutes(ctx context.Context) ([]models.Route, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at FROM routes ORDER BY priority DESC, name`)
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

func (s *SQLiteStore) CreateRoute(ctx context.Context, r models.Route) (models.Route, error) {
	now := time.Now().UTC()
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.CreatedAt = now
	r.UpdatedAt = now

	selectorJSON := serializeSelector(r.LabelSelector)

	_, err := s.sql.ExecContext(ctx, `INSERT INTO routes (id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Name, r.ParentID, boolToInt(r.Greedy), selectorJSON, r.DestinationID, r.TemplateID, boolToInt(r.IsDefault), r.Priority, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return models.Route{}, fmt.Errorf("create route: %w", err)
	}
	return r, nil
}

func (s *SQLiteStore) UpdateRoute(ctx context.Context, r models.Route) (models.Route, error) {
	if r.ID == "" {
		return models.Route{}, errors.New("route id is required")
	}
	r.UpdatedAt = time.Now().UTC()

	selectorJSON := serializeSelector(r.LabelSelector)

	_, err := s.sql.ExecContext(ctx, `UPDATE routes SET name = ?, parent_id = ?, greedy = ?, label_selector = ?, destination_id = ?, template_id = ?, is_default = ?, priority = ?, updated_at = ? WHERE id = ?`,
		r.Name, r.ParentID, boolToInt(r.Greedy), selectorJSON, r.DestinationID, r.TemplateID, boolToInt(r.IsDefault), r.Priority, r.UpdatedAt, r.ID)
	if err != nil {
		return models.Route{}, fmt.Errorf("update route: %w", err)
	}
	return r, nil
}

func (s *SQLiteStore) DeleteRoute(ctx context.Context, id string) error {
	_, err := s.sql.ExecContext(ctx, `DELETE FROM routes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete route: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetRoute(ctx context.Context, id string) (models.Route, error) {
	var (
		r            models.Route
		selectorJSON string
		isDefault    int
		greedy       int
	)
	row := s.sql.QueryRowContext(ctx, `SELECT id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at FROM routes WHERE id = ?`, id)
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

func (s *SQLiteStore) UpsertActiveAlert(ctx context.Context, a models.ActiveAlert) error {
	_, err := s.sql.ExecContext(ctx, `INSERT INTO active_alerts (fingerprint, status, team_id, channel_id, message_id, last_update)
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
func (s *SQLiteStore) ListActiveAlerts(ctx context.Context, fingerprint string) ([]models.ActiveAlert, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT fingerprint, status, team_id, channel_id, message_id, last_update FROM active_alerts WHERE fingerprint = ? ORDER BY team_id, channel_id`, fingerprint)
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

func (s *SQLiteStore) CountActiveAlerts(ctx context.Context) (int64, error) {
	var count int64
	if err := s.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM active_alerts`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active alerts: %w", err)
	}
	return count, nil
}

func (s *SQLiteStore) GetActiveAlert(ctx context.Context, fingerprint, teamID, channelID string) (models.ActiveAlert, error) {
	var a models.ActiveAlert
	err := s.sql.QueryRowContext(ctx, `SELECT fingerprint, status, team_id, channel_id, message_id, last_update FROM active_alerts WHERE fingerprint = ? AND team_id = ? AND channel_id = ?`, fingerprint, teamID, channelID).
		Scan(&a.Fingerprint, &a.Status, &a.TeamID, &a.ChannelID, &a.MessageID, &a.LastUpdate)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ActiveAlert{}, ErrNotFound
		}
		return models.ActiveAlert{}, fmt.Errorf("get active alert: %w", err)
	}
	return a, nil
}

func (s *SQLiteStore) DeleteActiveAlert(ctx context.Context, fingerprint, teamID, channelID string) error {
	_, err := s.sql.ExecContext(ctx, `DELETE FROM active_alerts WHERE fingerprint = ? AND team_id = ? AND channel_id = ?`, fingerprint, teamID, channelID)
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

// ListGrants returns the scopes in a stable order, so the admin page and its
// tests see them the same way every time.
func (s *SQLiteStore) ListGrants(ctx context.Context) ([]models.Grant, error) {
	rows, err := s.sql.QueryContext(ctx, `SELECT id, role, team_id, channel_id, created_at, updated_at FROM grants ORDER BY role, team_id, channel_id`)
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []models.Grant
	for rows.Next() {
		var g models.Grant
		if err := rows.Scan(&g.ID, &g.Role, &g.TeamID, &g.ChannelID, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan grant: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) CreateGrant(ctx context.Context, g models.Grant) (models.Grant, error) {
	now := time.Now().UTC()
	if g.ID == "" {
		g.ID = uuid.NewString()
	}
	g.CreatedAt, g.UpdatedAt = now, now

	_, err := s.sql.ExecContext(ctx, `INSERT INTO grants (id, role, team_id, channel_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		g.ID, g.Role, g.TeamID, g.ChannelID, g.CreatedAt, g.UpdatedAt)
	if err != nil {
		return models.Grant{}, fmt.Errorf("create grant: %w", err)
	}
	return g, nil
}

func (s *SQLiteStore) DeleteGrant(ctx context.Context, id string) error {
	_, err := s.sql.ExecContext(ctx, `DELETE FROM grants WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete grant: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateSession(ctx context.Context, session models.Session) error {
	_, err := s.sql.ExecContext(ctx, `INSERT INTO sessions (id, subject, name, source, role, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.Subject, session.Name, session.Source, session.Roles, session.CreatedAt, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// An expired session is reported as missing, so a caller cannot accidentally
// honour one by forgetting to check the time.
func (s *SQLiteStore) GetSession(ctx context.Context, id string) (models.Session, error) {
	var session models.Session
	err := s.sql.QueryRowContext(ctx, `SELECT id, subject, name, source, role, created_at, expires_at FROM sessions WHERE id = ?`, id).
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

func (s *SQLiteStore) DeleteSession(ctx context.Context, id string) error {
	if _, err := s.sql.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteExpiredSessions(ctx context.Context) error {
	now := time.Now()
	if _, err := s.sql.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now); err != nil {
		return fmt.Errorf("sweep sessions: %w", err)
	}
	if _, err := s.sql.ExecContext(ctx, `DELETE FROM login_flows WHERE expires_at <= ?`, now); err != nil {
		return fmt.Errorf("sweep login flows: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateLoginFlow(ctx context.Context, flow models.LoginFlow) error {
	_, err := s.sql.ExecContext(ctx, `INSERT INTO login_flows (state, verifier, nonce, expires_at) VALUES (?, ?, ?, ?)`,
		flow.State, flow.Verifier, flow.Nonce, flow.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create login flow: %w", err)
	}
	return nil
}

// Taking the flow deletes it: a state may be redeemed once, so a replayed
// callback finds nothing.
func (s *SQLiteStore) TakeLoginFlow(ctx context.Context, state string) (models.LoginFlow, error) {
	var flow models.LoginFlow
	err := s.sql.QueryRowContext(ctx, `DELETE FROM login_flows WHERE state = ? RETURNING state, verifier, nonce, expires_at`, state).
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

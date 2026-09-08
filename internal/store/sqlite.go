package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/pflege-de/teamster/internal/models"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &SQLiteStore{db: db}
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
	body TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS destinations (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS routes (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	label_selector TEXT NOT NULL,
	destination_id TEXT NOT NULL,
	template_id TEXT NOT NULL,
	is_default INTEGER NOT NULL,
	priority INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS active_alerts (
	fingerprint TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	last_update TEXT NOT NULL
);
`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListTemplates() ([]models.Template, error) {
	rows, err := s.db.Query(`SELECT id, name, body, created_at, updated_at FROM templates ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []models.Template
	for rows.Next() {
		var t models.Template
		if err := rows.Scan(&t.ID, &t.Name, &t.Body, &t.CreatedAt, &t.UpdatedAt); err != nil {
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

	_, err := s.db.Exec(`INSERT INTO templates (id, name, body, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Body, t.CreatedAt, t.UpdatedAt)
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

	_, err := s.db.Exec(`UPDATE templates SET name = ?, body = ?, updated_at = ? WHERE id = ?`,
		t.Name, t.Body, t.UpdatedAt, t.ID)
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
	err := s.db.QueryRow(`SELECT id, name, body, created_at, updated_at FROM templates WHERE id = ?`, id).
		Scan(&t.ID, &t.Name, &t.Body, &t.CreatedAt, &t.UpdatedAt)
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
	rows, err := s.db.Query(`SELECT id, name, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at FROM routes ORDER BY priority DESC, name`)
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
		)
		if err := rows.Scan(&r.ID, &r.Name, &selectorJSON, &r.DestinationID, &r.TemplateID, &isDefault, &r.Priority, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan route: %w", err)
		}
		r.LabelSelector = parseSelector(selectorJSON)
		r.IsDefault = isDefault == 1
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
	isDefault := 0
	if r.IsDefault {
		isDefault = 1
	}

	_, err := s.db.Exec(`INSERT INTO routes (id, name, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Name, selectorJSON, r.DestinationID, r.TemplateID, isDefault, r.Priority, r.CreatedAt, r.UpdatedAt)
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
	isDefault := 0
	if r.IsDefault {
		isDefault = 1
	}

	_, err := s.db.Exec(`UPDATE routes SET name = ?, label_selector = ?, destination_id = ?, template_id = ?, is_default = ?, priority = ?, updated_at = ? WHERE id = ?`,
		r.Name, selectorJSON, r.DestinationID, r.TemplateID, isDefault, r.Priority, r.UpdatedAt, r.ID)
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
	)
	row := s.db.QueryRow(`SELECT id, name, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at FROM routes WHERE id = ?`, id)
	if err := row.Scan(&r.ID, &r.Name, &selectorJSON, &r.DestinationID, &r.TemplateID, &isDefault, &r.Priority, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Route{}, ErrNotFound
		}
		return models.Route{}, fmt.Errorf("get route: %w", err)
	}
	r.LabelSelector = parseSelector(selectorJSON)
	r.IsDefault = isDefault == 1
	return r, nil
}

func (s *SQLiteStore) UpsertActiveAlert(a models.ActiveAlert) error {
	_, err := s.db.Exec(`INSERT INTO active_alerts (fingerprint, status, team_id, channel_id, message_id, last_update)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(fingerprint) DO UPDATE SET status = excluded.status, team_id = excluded.team_id, channel_id = excluded.channel_id, message_id = excluded.message_id, last_update = excluded.last_update`,
		a.Fingerprint, a.Status, a.TeamID, a.ChannelID, a.MessageID, a.LastUpdate)
	if err != nil {
		return fmt.Errorf("upsert active alert: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetActiveAlert(fingerprint string) (models.ActiveAlert, error) {
	var a models.ActiveAlert
	err := s.db.QueryRow(`SELECT fingerprint, status, team_id, channel_id, message_id, last_update FROM active_alerts WHERE fingerprint = ?`, fingerprint).
		Scan(&a.Fingerprint, &a.Status, &a.TeamID, &a.ChannelID, &a.MessageID, &a.LastUpdate)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ActiveAlert{}, ErrNotFound
		}
		return models.ActiveAlert{}, fmt.Errorf("get active alert: %w", err)
	}
	return a, nil
}

func (s *SQLiteStore) DeleteActiveAlert(fingerprint string) error {
	_, err := s.db.Exec(`DELETE FROM active_alerts WHERE fingerprint = ?`, fingerprint)
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

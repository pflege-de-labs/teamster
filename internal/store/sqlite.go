package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store/migrations"
	"github.com/pflege-de-labs/teamster/internal/store/sqlitedb"
)

// The three operations that need the database itself, rather than somewhere to
// run a statement, used to be guarded by a nil field that the other twenty-odd
// methods trusted silently. Now a transaction is a type that cannot perform
// them.
var (
	errInTransaction        = errors.New("cannot do this inside a transaction")
	errAlreadyInTransaction = errors.New("already in a transaction")
)

// sqliteQueries is everything that only needs somewhere to run a statement. The
// store and a transaction-bound view of it both embed one, so the data methods
// are written once and the two cannot drift apart.
type sqliteQueries struct {
	q *sqlitedb.Queries
}

type SQLiteStore struct {
	sqliteQueries
	db   *sql.DB
	path string
}

// sqliteTx is the store as the function inside WithTx sees it: the same data
// methods, and a refusal on the three that would reach past the transaction.
type sqliteTx struct {
	sqliteQueries
}

func (sqliteTx) Close() error               { return errInTransaction }
func (sqliteTx) Ping(context.Context) error { return errInTransaction }
func (sqliteTx) WithTx(context.Context, func(context.Context, Store) error) error {
	return errAlreadyInTransaction
}

// NewSQLiteStore opens the file and brings the schema to the version this
// build expects, or checks that somebody else already did — see MigrateMode.
func NewSQLiteStore(ctx context.Context, path string, migrate MigrateMode) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := applyMigrations(ctx, migrations.SQLite, db, migrate); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &SQLiteStore{
		sqliteQueries: sqliteQueries{q: sqlitedb.New(db)},
		db:            db,
		path:          path,
	}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Ping is a round trip to the database rather than a look at a connection
// struct: a file that has been deleted or a disk that has gone read-only shows
// up here and nowhere else.
func (s *SQLiteStore) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// WithTx runs fn against a store bound to one transaction, committing when it
// returns nil and rolling back otherwise. It is what lets an import that turns
// out to be invalid half way through leave the configuration as it found it.
func (s *SQLiteStore) WithTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error {
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

	if err := fn(ctx, &sqliteTx{sqliteQueries{q: s.q.WithTx(tx)}}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

func (s sqliteQueries) ListTemplates(ctx context.Context) ([]models.Template, error) {
	rows, err := s.q.ListTemplates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.Template, 0, len(rows))
	for _, row := range rows {
		out = append(out, templateOf(row))
	}
	return out, nil
}

func (s sqliteQueries) CreateTemplate(ctx context.Context, t models.Template) (models.Template, error) {
	t.ID = newID(t.ID)
	t.CreatedAt = nowUTC()
	t.UpdatedAt = t.CreatedAt

	err := s.q.CreateTemplate(ctx, sqlitedb.CreateTemplateParams{
		ID:          t.ID,
		Name:        t.Name,
		Title:       t.Title,
		MessageText: t.Text,
		Body:        t.Body,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	})
	if err != nil {
		return models.Template{}, fmt.Errorf("create template: %w", err)
	}
	return t, nil
}

func (s sqliteQueries) UpdateTemplate(ctx context.Context, t models.Template) (models.Template, error) {
	if t.ID == "" {
		return models.Template{}, errors.New("template id is required")
	}
	t.UpdatedAt = nowUTC()

	err := s.q.UpdateTemplate(ctx, sqlitedb.UpdateTemplateParams{
		Name:        t.Name,
		Title:       t.Title,
		MessageText: t.Text,
		Body:        t.Body,
		UpdatedAt:   t.UpdatedAt,
		ID:          t.ID,
	})
	if err != nil {
		return models.Template{}, fmt.Errorf("update template: %w", err)
	}
	return t, nil
}

func (s sqliteQueries) DeleteTemplate(ctx context.Context, id string) error {
	if err := s.q.DeleteTemplate(ctx, id); err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}

func (s sqliteQueries) GetTemplate(ctx context.Context, id string) (models.Template, error) {
	row, err := s.q.GetTemplate(ctx, id)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Template{}, err
		}
		return models.Template{}, fmt.Errorf("get template: %w", err)
	}
	return templateOf(row), nil
}

func templateOf(row sqlitedb.Template) models.Template {
	return models.Template{
		ID:        row.ID,
		Name:      row.Name,
		Title:     row.Title,
		Text:      row.MessageText,
		Body:      row.Body,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func (s sqliteQueries) ListDestinations(ctx context.Context) ([]models.Destination, error) {
	rows, err := s.q.ListDestinations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list destinations: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.Destination, 0, len(rows))
	for _, row := range rows {
		out = append(out, destinationOf(row))
	}
	return out, nil
}

func (s sqliteQueries) CreateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	d.ID = newID(d.ID)
	d.CreatedAt = nowUTC()
	d.UpdatedAt = d.CreatedAt

	err := s.q.CreateDestination(ctx, sqlitedb.CreateDestinationParams{
		ID:        d.ID,
		Name:      d.Name,
		TeamID:    d.TeamID,
		ChannelID: d.ChannelID,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	})
	if err != nil {
		return models.Destination{}, fmt.Errorf("create destination: %w", err)
	}
	return d, nil
}

func (s sqliteQueries) UpdateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	if d.ID == "" {
		return models.Destination{}, errors.New("destination id is required")
	}
	d.UpdatedAt = nowUTC()

	err := s.q.UpdateDestination(ctx, sqlitedb.UpdateDestinationParams{
		Name:      d.Name,
		TeamID:    d.TeamID,
		ChannelID: d.ChannelID,
		UpdatedAt: d.UpdatedAt,
		ID:        d.ID,
	})
	if err != nil {
		return models.Destination{}, fmt.Errorf("update destination: %w", err)
	}
	return d, nil
}

func (s sqliteQueries) DeleteDestination(ctx context.Context, id string) error {
	if err := s.q.DeleteDestination(ctx, id); err != nil {
		return fmt.Errorf("delete destination: %w", err)
	}
	return nil
}

func (s sqliteQueries) GetDestination(ctx context.Context, id string) (models.Destination, error) {
	row, err := s.q.GetDestination(ctx, id)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Destination{}, err
		}
		return models.Destination{}, fmt.Errorf("get destination: %w", err)
	}
	return destinationOf(row), nil
}

func destinationOf(row sqlitedb.Destination) models.Destination {
	return models.Destination{
		ID:        row.ID,
		Name:      row.Name,
		TeamID:    row.TeamID,
		ChannelID: row.ChannelID,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func (s sqliteQueries) ListRoutes(ctx context.Context) ([]models.Route, error) {
	rows, err := s.q.ListRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.Route, 0, len(rows))
	for _, row := range rows {
		out = append(out, routeOf(row))
	}
	return out, nil
}

func (s sqliteQueries) CreateRoute(ctx context.Context, r models.Route) (models.Route, error) {
	r.ID = newID(r.ID)
	r.CreatedAt = nowUTC()
	r.UpdatedAt = r.CreatedAt

	err := s.q.CreateRoute(ctx, sqlitedb.CreateRouteParams{
		ID:            r.ID,
		Name:          r.Name,
		ParentID:      r.ParentID,
		Greedy:        r.Greedy,
		LabelSelector: serializeSelector(r.LabelSelector),
		DestinationID: r.DestinationID,
		TemplateID:    r.TemplateID,
		IsDefault:     r.IsDefault,
		Priority:      int64(r.Priority),
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	})
	if err != nil {
		return models.Route{}, fmt.Errorf("create route: %w", err)
	}
	return r, nil
}

func (s sqliteQueries) UpdateRoute(ctx context.Context, r models.Route) (models.Route, error) {
	if r.ID == "" {
		return models.Route{}, errors.New("route id is required")
	}
	r.UpdatedAt = nowUTC()

	err := s.q.UpdateRoute(ctx, sqlitedb.UpdateRouteParams{
		Name:          r.Name,
		ParentID:      r.ParentID,
		Greedy:        r.Greedy,
		LabelSelector: serializeSelector(r.LabelSelector),
		DestinationID: r.DestinationID,
		TemplateID:    r.TemplateID,
		IsDefault:     r.IsDefault,
		Priority:      int64(r.Priority),
		UpdatedAt:     r.UpdatedAt,
		ID:            r.ID,
	})
	if err != nil {
		return models.Route{}, fmt.Errorf("update route: %w", err)
	}
	return r, nil
}

func (s sqliteQueries) DeleteRoute(ctx context.Context, id string) error {
	if err := s.q.DeleteRoute(ctx, id); err != nil {
		return fmt.Errorf("delete route: %w", err)
	}
	return nil
}

func (s sqliteQueries) GetRoute(ctx context.Context, id string) (models.Route, error) {
	row, err := s.q.GetRoute(ctx, id)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Route{}, err
		}
		return models.Route{}, fmt.Errorf("get route: %w", err)
	}
	return routeOf(row), nil
}

func routeOf(row sqlitedb.Route) models.Route {
	return models.Route{
		ID:            row.ID,
		Name:          row.Name,
		ParentID:      row.ParentID,
		Greedy:        row.Greedy,
		LabelSelector: parseSelector(row.LabelSelector),
		DestinationID: row.DestinationID,
		TemplateID:    row.TemplateID,
		IsDefault:     row.IsDefault,
		Priority:      int(row.Priority),
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func (s sqliteQueries) UpsertActiveAlert(ctx context.Context, a models.ActiveAlert) error {
	err := s.q.UpsertActiveAlert(ctx, sqlitedb.UpsertActiveAlertParams{
		Fingerprint: a.Fingerprint,
		Status:      a.Status,
		TeamID:      a.TeamID,
		ChannelID:   a.ChannelID,
		MessageID:   a.MessageID,
		LastUpdate:  a.LastUpdate,
	})
	if err != nil {
		return fmt.Errorf("upsert active alert: %w", err)
	}
	return nil
}

func (s sqliteQueries) ListActiveAlerts(ctx context.Context, fingerprint string) ([]models.ActiveAlert, error) {
	rows, err := s.q.ListActiveAlerts(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("list active alerts: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.ActiveAlert, 0, len(rows))
	for _, row := range rows {
		out = append(out, activeAlertOf(row))
	}
	return out, nil
}

func (s sqliteQueries) CountActiveAlerts(ctx context.Context) (int64, error) {
	count, err := s.q.CountActiveAlerts(ctx)
	if err != nil {
		return 0, fmt.Errorf("count active alerts: %w", err)
	}
	return count, nil
}

func (s sqliteQueries) GetActiveAlert(ctx context.Context, fingerprint, teamID, channelID string) (models.ActiveAlert, error) {
	row, err := s.q.GetActiveAlert(ctx, sqlitedb.GetActiveAlertParams{
		Fingerprint: fingerprint,
		TeamID:      teamID,
		ChannelID:   channelID,
	})
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.ActiveAlert{}, err
		}
		return models.ActiveAlert{}, fmt.Errorf("get active alert: %w", err)
	}
	return activeAlertOf(row), nil
}

func (s sqliteQueries) DeleteActiveAlert(ctx context.Context, fingerprint, teamID, channelID string) error {
	err := s.q.DeleteActiveAlert(ctx, sqlitedb.DeleteActiveAlertParams{
		Fingerprint: fingerprint,
		TeamID:      teamID,
		ChannelID:   channelID,
	})
	if err != nil {
		return fmt.Errorf("delete active alert: %w", err)
	}
	return nil
}

func activeAlertOf(row sqlitedb.ActiveAlert) models.ActiveAlert {
	return models.ActiveAlert{
		Fingerprint: row.Fingerprint,
		Status:      row.Status,
		TeamID:      row.TeamID,
		ChannelID:   row.ChannelID,
		MessageID:   row.MessageID,
		LastUpdate:  row.LastUpdate,
	}
}

func (s sqliteQueries) ListGrants(ctx context.Context) ([]models.Grant, error) {
	rows, err := s.q.ListGrants(ctx)
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.Grant, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.Grant{
			ID:        row.ID,
			Role:      row.Role,
			TeamID:    row.TeamID,
			ChannelID: row.ChannelID,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		})
	}
	return out, nil
}

func (s sqliteQueries) CreateGrant(ctx context.Context, g models.Grant) (models.Grant, error) {
	g.ID = newID(g.ID)
	g.CreatedAt = nowUTC()
	g.UpdatedAt = g.CreatedAt

	err := s.q.CreateGrant(ctx, sqlitedb.CreateGrantParams{
		ID:        g.ID,
		Role:      g.Role,
		TeamID:    g.TeamID,
		ChannelID: g.ChannelID,
		CreatedAt: g.CreatedAt,
		UpdatedAt: g.UpdatedAt,
	})
	if err != nil {
		return models.Grant{}, fmt.Errorf("create grant: %w", err)
	}
	return g, nil
}

func (s sqliteQueries) DeleteGrant(ctx context.Context, id string) error {
	if err := s.q.DeleteGrant(ctx, id); err != nil {
		return fmt.Errorf("delete grant: %w", err)
	}
	return nil
}

func (s sqliteQueries) CreateSession(ctx context.Context, session models.Session) error {
	err := s.q.CreateSession(ctx, sqlitedb.CreateSessionParams{
		ID:        session.ID,
		Subject:   session.Subject,
		Name:      session.Name,
		Source:    session.Source,
		Role:      session.Roles,
		CreatedAt: session.CreatedAt,
		ExpiresAt: session.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// An expired session is reported as missing, so a caller cannot accidentally
// honour one by forgetting to check the time.
func (s sqliteQueries) GetSession(ctx context.Context, id string) (models.Session, error) {
	row, err := s.q.GetSession(ctx, id)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Session{}, err
		}
		return models.Session{}, fmt.Errorf("get session: %w", err)
	}
	return liveSession(models.Session{
		ID:        row.ID,
		Subject:   row.Subject,
		Name:      row.Name,
		Source:    row.Source,
		Roles:     row.Role,
		CreatedAt: row.CreatedAt,
		ExpiresAt: row.ExpiresAt,
	})
}

func (s sqliteQueries) DeleteSession(ctx context.Context, id string) error {
	if err := s.q.DeleteSession(ctx, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s sqliteQueries) DeleteExpiredSessions(ctx context.Context) error {
	now := time.Now()
	if err := s.q.DeleteExpiredSessions(ctx, now); err != nil {
		return fmt.Errorf("sweep sessions: %w", err)
	}
	if err := s.q.DeleteExpiredLoginFlows(ctx, now); err != nil {
		return fmt.Errorf("sweep login flows: %w", err)
	}
	return nil
}

func (s sqliteQueries) CreateLoginFlow(ctx context.Context, flow models.LoginFlow) error {
	err := s.q.CreateLoginFlow(ctx, sqlitedb.CreateLoginFlowParams{
		State:     flow.State,
		Verifier:  flow.Verifier,
		Nonce:     flow.Nonce,
		ExpiresAt: flow.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("create login flow: %w", err)
	}
	return nil
}

// Taking the flow deletes it: a state may be redeemed once, so a replayed
// callback finds nothing.
func (s sqliteQueries) TakeLoginFlow(ctx context.Context, state string) (models.LoginFlow, error) {
	row, err := s.q.TakeLoginFlow(ctx, state)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.LoginFlow{}, err
		}
		return models.LoginFlow{}, fmt.Errorf("take login flow: %w", err)
	}
	return liveFlow(models.LoginFlow{
		State:     row.State,
		Verifier:  row.Verifier,
		Nonce:     row.Nonce,
		ExpiresAt: row.ExpiresAt,
	})
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store/sqlitedb"
)

// usersPageMax bounds a listing; the UI pages by search, not by offset.
const usersPageMax = 500

func (s queryAdapter) RecordSignIn(ctx context.Context, u models.User) error {
	groups, err := json.Marshal(nonNil(u.IdPGroups))
	if err != nil {
		return err
	}
	seen := u.LastSeen
	if seen.IsZero() {
		seen = nowUTC()
	}
	err = s.q.UpsertUser(ctx, sqlitedb.UpsertUserParams{
		Subject:   u.Subject,
		Source:    u.Source,
		Name:      u.Name,
		Email:     u.Email,
		Roles:     strings.Join(u.Roles, " "),
		IdpGroups: string(groups),
		SeenAt:    seen.UTC(),
	})
	if err != nil {
		return fmt.Errorf("record sign-in: %w", err)
	}
	return nil
}

func (s queryAdapter) GetUser(ctx context.Context, subject string) (models.User, error) {
	row, err := s.q.GetUser(ctx, subject)
	if err != nil {
		return models.User{}, notFound(err)
	}
	return userFromRow(row), nil
}

func (s queryAdapter) ListUsers(ctx context.Context, search string, limit int) ([]models.User, error) {
	if limit <= 0 || limit > usersPageMax {
		limit = usersPageMax
	}
	pattern := ""
	if search = strings.TrimSpace(search); search != "" {
		pattern = "%" + strings.ToLower(search) + "%"
	}
	rows, err := s.q.ListUsers(ctx, sqlitedb.ListUsersParams{Pattern: pattern, MaxRows: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	out := make([]models.User, 0, len(rows))
	for _, row := range rows {
		out = append(out, userFromRow(row))
	}
	return out, nil
}

// DisableUser is the transaction-bound half; the stores wrap it in one.
func (s queryAdapter) DisableUser(ctx context.Context, subject, by string) error {
	n, err := s.q.SetUserDisabled(ctx, sqlitedb.SetUserDisabledParams{
		DisabledAt: sql.NullTime{Time: nowUTC(), Valid: true}, DisabledBy: by, Subject: subject,
	})
	if err != nil {
		return fmt.Errorf("disable user: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := s.q.DeleteBrokerTokensForSubject(ctx, subject); err != nil {
		return fmt.Errorf("delete broker tokens of a disabled user: %w", err)
	}
	if err := s.q.DeleteSessionsForSubject(ctx, subject); err != nil {
		return fmt.Errorf("end sessions of a disabled user: %w", err)
	}
	return nil
}

func (s queryAdapter) EnableUser(ctx context.Context, subject string) error {
	n, err := s.q.SetUserDisabled(ctx, sqlitedb.SetUserDisabledParams{Subject: subject})
	if err != nil {
		return fmt.Errorf("enable user: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func disableUserInTx(ctx context.Context, s interface {
	WithTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error
}, subject, by string) error {
	return s.WithTx(ctx, func(ctx context.Context, tx Store) error {
		return tx.DisableUser(ctx, subject, by)
	})
}

func userFromRow(row sqlitedb.User) models.User {
	u := models.User{
		Subject:    row.Subject,
		Source:     row.Source,
		Name:       row.Name,
		Email:      row.Email,
		Roles:      strings.Fields(row.Roles),
		FirstSeen:  row.FirstSeen.UTC(),
		LastSeen:   row.LastSeen.UTC(),
		DisabledBy: row.DisabledBy,
	}
	if row.DisabledAt.Valid {
		u.DisabledAt = row.DisabledAt.Time.UTC()
	}
	// A row written by hand with bad JSON still lists, without groups.
	_ = json.Unmarshal([]byte(row.IdpGroups), &u.IdPGroups)
	if u.Roles == nil {
		u.Roles = []string{}
	}
	if u.IdPGroups == nil {
		u.IdPGroups = []string{}
	}
	return u
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store/sqlitedb"
)

// addressKey is how UPNs and mail addresses are compared: without case.
func addressKey(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

func (s queryAdapter) UpsertDirectoryUser(ctx context.Context, u models.DirectoryUser) error {
	if u.DirectorySeenAt.IsZero() {
		u.DirectorySeenAt = nowUTC()
	}
	err := s.q.UpsertDirectoryUser(ctx, sqlitedb.UpsertDirectoryUserParams{
		AadObjectID:       u.AADObjectID,
		TenantID:          u.TenantID,
		UserPrincipalName: u.UserPrincipalName,
		Mail:              u.Mail,
		UpnKey:            addressKey(u.UserPrincipalName),
		MailKey:           addressKey(u.Mail),
		DisplayName:       u.DisplayName,
		GivenName:         u.GivenName,
		Surname:           u.Surname,
		Eligible:          u.Eligible,
		SeenAt:            u.DirectorySeenAt.UTC(),
	})
	if err != nil {
		return fmt.Errorf("upsert directory user: %w", err)
	}
	return nil
}

func (s queryAdapter) GetDirectoryUser(ctx context.Context, aadObjectID string) (models.DirectoryUser, error) {
	row, err := s.q.GetDirectoryUser(ctx, aadObjectID)
	return directoryUserResult(row, err, "get directory user")
}

func (s queryAdapter) FindDirectoryUser(ctx context.Context, address string) (models.DirectoryUser, error) {
	key := addressKey(address)
	if key == "" {
		return models.DirectoryUser{}, ErrNotFound
	}
	row, err := s.q.FindDirectoryUserByUPNKey(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		row, err = s.q.FindDirectoryUserByMailKey(ctx, key)
	}
	return directoryUserResult(row, err, "find directory user")
}

func (s queryAdapter) GetDirectoryUserByConversation(ctx context.Context, conversationID string) (models.DirectoryUser, error) {
	if conversationID == "" {
		return models.DirectoryUser{}, ErrNotFound
	}
	row, err := s.q.GetDirectoryUserByConversation(ctx, conversationID)
	return directoryUserResult(row, err, "get directory user by conversation")
}

func (s queryAdapter) SetDirectoryUserInstalled(ctx context.Context, aadObjectID, conversationID, serviceURL string, at time.Time) error {
	n, err := s.q.SetDirectoryUserInstalled(ctx, sqlitedb.SetDirectoryUserInstalledParams{
		ConversationID: conversationID,
		ServiceUrl:     serviceURL,
		At:             nullTime(at.UTC()),
		AadObjectID:    aadObjectID,
	})
	return oneRow(n, err, "set directory user installed")
}

func (s queryAdapter) RecordDirectoryInstallFailure(ctx context.Context, aadObjectID string, state models.InstallState, lastError string, next, at time.Time) error {
	n, err := s.q.RecordDirectoryInstallFailure(ctx, sqlitedb.RecordDirectoryInstallFailureParams{
		State:         string(state),
		LastError:     lastError,
		NextAttemptAt: nullTime(next.UTC()),
		At:            at.UTC(),
		AadObjectID:   aadObjectID,
	})
	return oneRow(n, err, "record directory install failure")
}

func (s queryAdapter) MarkDirectoryUserRemoved(ctx context.Context, aadObjectID string, at time.Time) error {
	n, err := s.q.MarkDirectoryUserRemoved(ctx, sqlitedb.MarkDirectoryUserRemovedParams{
		At:          nullTime(at.UTC()),
		AadObjectID: aadObjectID,
	})
	return oneRow(n, err, "mark directory user removed")
}

func (s queryAdapter) MarkDirectoryUserBlocked(ctx context.Context, aadObjectID string, at time.Time, reason string) error {
	err := s.q.MarkDirectoryUserBlocked(ctx, sqlitedb.MarkDirectoryUserBlockedParams{
		At: nullTime(at.UTC()), Reason: reason, AadObjectID: aadObjectID,
	})
	if err != nil {
		return fmt.Errorf("mark directory user blocked: %w", err)
	}
	return nil
}

func (s queryAdapter) ClearDirectoryUserBlocked(ctx context.Context, aadObjectID string) error {
	if err := s.q.ClearDirectoryUserBlocked(ctx, aadObjectID); err != nil {
		return fmt.Errorf("clear directory user blocked: %w", err)
	}
	return nil
}

func (s queryAdapter) ListDirectoryUsersDue(ctx context.Context, now, reverifyBefore time.Time, limit int) ([]models.DirectoryUser, error) {
	rows, err := s.q.ListDirectoryUsersDue(ctx, sqlitedb.ListDirectoryUsersDueParams{
		Now:            nullTime(now.UTC()),
		ReverifyBefore: nullTime(reverifyBefore.UTC()),
		MaxRows:        int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list directory users due: %w", err)
	}
	out := make([]models.DirectoryUser, 0, len(rows))
	for _, row := range rows {
		out = append(out, directoryUserOf(row))
	}
	return out, nil
}

func (s queryAdapter) MarkDirectoryUsersDeparted(ctx context.Context, seenBefore, at time.Time) (int64, error) {
	n, err := s.q.MarkDirectoryUsersDeparted(ctx, sqlitedb.MarkDirectoryUsersDepartedParams{
		At:         at.UTC(),
		SeenBefore: seenBefore.UTC(),
	})
	if err != nil {
		return 0, fmt.Errorf("mark directory users departed: %w", err)
	}
	return n, nil
}

func (s queryAdapter) PurgeDepartedDirectoryUsers(ctx context.Context, before time.Time) (int64, error) {
	n, err := s.q.PurgeDepartedDirectoryUsers(ctx, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("purge departed directory users: %w", err)
	}
	return n, nil
}

func (s queryAdapter) ListDirectoryUserProblems(ctx context.Context, limit int) ([]models.DirectoryUser, error) {
	rows, err := s.q.ListDirectoryUserProblems(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list directory user problems: %w", err)
	}
	out := make([]models.DirectoryUser, 0, len(rows))
	for _, row := range rows {
		out = append(out, directoryUserOf(row))
	}
	return out, nil
}

func (s queryAdapter) CountDirectoryUsersByState(ctx context.Context) (map[models.InstallState]int64, error) {
	rows, err := s.q.CountDirectoryUsersByState(ctx)
	if err != nil {
		return nil, fmt.Errorf("count directory users: %w", err)
	}
	out := make(map[models.InstallState]int64, len(rows))
	for _, row := range rows {
		out[models.InstallState(row.InstallState)] = row.Users
	}
	return out, nil
}

func (s queryAdapter) RequestDirectoryRun(ctx context.Context, r models.DirectoryRun) (models.DirectoryRun, error) {
	r.ID = newID(r.ID)
	if r.RequestedAt.IsZero() {
		r.RequestedAt = nowUTC()
	}
	err := s.q.InsertDirectoryRun(ctx, sqlitedb.InsertDirectoryRunParams{
		ID:          r.ID,
		Kind:        string(r.Kind),
		RequestedBy: r.RequestedBy,
		RequestedAt: r.RequestedAt.UTC(),
	})
	if err != nil {
		if isPrimaryKeyConflict(err) {
			return models.DirectoryRun{}, ErrConflict
		}
		return models.DirectoryRun{}, fmt.Errorf("request directory run: %w", err)
	}
	return s.GetDirectoryRun(ctx, r.ID)
}

func (s queryAdapter) GetDirectoryRun(ctx context.Context, id string) (models.DirectoryRun, error) {
	row, err := s.q.GetDirectoryRun(ctx, id)
	return directoryRunResult(row, err, "get directory run")
}

func (s queryAdapter) LatestDirectoryRun(ctx context.Context) (models.DirectoryRun, error) {
	row, err := s.q.LatestDirectoryRun(ctx)
	return directoryRunResult(row, err, "latest directory run")
}

func (s queryAdapter) ClaimDirectoryRun(ctx context.Context, id, owner string, now, staleBefore time.Time) (bool, error) {
	n, err := s.q.ClaimDirectoryRun(ctx, sqlitedb.ClaimDirectoryRunParams{
		Owner:       owner,
		Now:         nullTime(now.UTC()),
		ID:          id,
		StaleBefore: nullTime(staleBefore.UTC()),
	})
	if err != nil {
		return false, fmt.Errorf("claim directory run: %w", err)
	}
	return n == 1, nil
}

func (s queryAdapter) HeartbeatDirectoryRun(ctx context.Context, id, owner string, c models.RunCounts, now time.Time) (bool, error) {
	n, err := s.q.HeartbeatDirectoryRun(ctx, sqlitedb.HeartbeatDirectoryRunParams{
		Now:        nullTime(now.UTC()),
		Total:      c.Total,
		Installed:  c.Installed,
		Already:    c.Already,
		Failed:     c.Failed,
		Ineligible: c.Ineligible,
		ID:         id,
		Owner:      owner,
	})
	if err != nil {
		return false, fmt.Errorf("heartbeat directory run: %w", err)
	}
	return n == 1, nil
}

func (s queryAdapter) FinishDirectoryRun(ctx context.Context, id, owner string, state models.RunState, c models.RunCounts, lastError string, now time.Time) (bool, error) {
	n, err := s.q.FinishDirectoryRun(ctx, sqlitedb.FinishDirectoryRunParams{
		State:      string(state),
		Now:        nullTime(now.UTC()),
		Total:      c.Total,
		Installed:  c.Installed,
		Already:    c.Already,
		Failed:     c.Failed,
		Ineligible: c.Ineligible,
		LastError:  lastError,
		ID:         id,
		Owner:      owner,
	})
	if err != nil {
		return false, fmt.Errorf("finish directory run: %w", err)
	}
	return n == 1, nil
}

func (s queryAdapter) PruneDirectoryRuns(ctx context.Context, before time.Time) (int64, error) {
	n, err := s.q.PruneDirectoryRuns(ctx, nullTime(before.UTC()))
	if err != nil {
		return 0, fmt.Errorf("prune directory runs: %w", err)
	}
	return n, nil
}

// oneRow turns an update that matched nothing into ErrNotFound.
func oneRow(n int64, err error, op string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func directoryUserResult(row sqlitedb.DirectoryUser, err error, op string) (models.DirectoryUser, error) {
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.DirectoryUser{}, err
		}
		return models.DirectoryUser{}, fmt.Errorf("%s: %w", op, err)
	}
	return directoryUserOf(row), nil
}

func directoryUserOf(row sqlitedb.DirectoryUser) models.DirectoryUser {
	return models.DirectoryUser{
		AADObjectID:       row.AadObjectID,
		TenantID:          row.TenantID,
		UserPrincipalName: row.UserPrincipalName,
		Mail:              row.Mail,
		DisplayName:       row.DisplayName,
		GivenName:         row.GivenName,
		Surname:           row.Surname,
		Eligible:          row.Eligible,
		ConversationID:    row.ConversationID,
		ServiceURL:        row.ServiceUrl,
		InstallState:      models.InstallState(row.InstallState),
		InstalledAt:       row.InstalledAt.Time,
		NextAttemptAt:     row.NextAttemptAt.Time,
		Attempts:          row.Attempts,
		LastError:         row.LastError,
		BlockedAt:         row.BlockedAt.Time,
		BlockedReason:     row.BlockedReason,
		DirectorySeenAt:   row.DirectorySeenAt,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func directoryRunResult(row sqlitedb.DirectoryRun, err error, op string) (models.DirectoryRun, error) {
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.DirectoryRun{}, err
		}
		return models.DirectoryRun{}, fmt.Errorf("%s: %w", op, err)
	}
	return models.DirectoryRun{
		ID:          row.ID,
		Kind:        models.RunKind(row.Kind),
		RequestedBy: row.RequestedBy,
		RequestedAt: row.RequestedAt,
		State:       models.RunState(row.State),
		Owner:       row.Owner,
		HeartbeatAt: row.HeartbeatAt.Time,
		StartedAt:   row.StartedAt.Time,
		FinishedAt:  row.FinishedAt.Time,
		RunCounts: models.RunCounts{
			Total:      row.Total,
			Installed:  row.Installed,
			Already:    row.Already,
			Failed:     row.Failed,
			Ineligible: row.Ineligible,
		},
		LastError: row.LastError,
	}, nil
}

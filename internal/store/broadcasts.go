package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store/sqlitedb"
)

// broadcastsPageMax bounds a listing of broadcasts.
const broadcastsPageMax = 200

func (s queryAdapter) ListReachableDirectoryUsers(ctx context.Context, after string, limit int) ([]models.DirectoryUser, error) {
	rows, err := s.q.ListReachableDirectoryUsers(ctx, sqlitedb.ListReachableDirectoryUsersParams{After: after, MaxRows: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("list reachable directory users: %w", err)
	}
	out := make([]models.DirectoryUser, 0, len(rows))
	for _, row := range rows {
		out = append(out, directoryUserOf(row))
	}
	return out, nil
}

func (s queryAdapter) CreateBroadcast(ctx context.Context, b models.Broadcast) (models.Broadcast, error) {
	b.ID = newID(b.ID)
	if b.RequestedAt.IsZero() {
		b.RequestedAt = nowUTC()
	}
	err := s.q.InsertBroadcast(ctx, sqlitedb.InsertBroadcastParams{
		ID: b.ID, RequestedBy: b.RequestedBy, TokenName: b.TokenName, RequestedAt: b.RequestedAt.UTC(),
		Event: string(b.Event), Plan: string(b.Plan),
	})
	if err != nil {
		return models.Broadcast{}, fmt.Errorf("create broadcast: %w", err)
	}
	return s.GetBroadcast(ctx, b.ID)
}

func (s queryAdapter) GetBroadcast(ctx context.Context, id string) (models.Broadcast, error) {
	row, err := s.q.GetBroadcast(ctx, id)
	return broadcastResult(row, err, "get broadcast")
}

func (s queryAdapter) ListBroadcasts(ctx context.Context, requestedBy string, limit int) ([]models.Broadcast, error) {
	if limit <= 0 || limit > broadcastsPageMax {
		limit = broadcastsPageMax
	}
	rows, err := s.q.ListBroadcasts(ctx, sqlitedb.ListBroadcastsParams{RequestedBy: requestedBy, MaxRows: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("list broadcasts: %w", err)
	}
	out := make([]models.Broadcast, 0, len(rows))
	for _, row := range rows {
		out = append(out, broadcastOf(row))
	}
	return out, nil
}

func (s queryAdapter) NextBroadcast(ctx context.Context, staleBefore time.Time) (models.Broadcast, error) {
	row, err := s.q.NextBroadcast(ctx, nullTime(staleBefore.UTC()))
	return broadcastResult(row, err, "next broadcast")
}

func (s queryAdapter) ClaimBroadcast(ctx context.Context, id, owner string, now, staleBefore time.Time) (bool, error) {
	n, err := s.q.ClaimBroadcast(ctx, sqlitedb.ClaimBroadcastParams{
		Owner: owner, Now: nullTime(now.UTC()), ID: id, StaleBefore: nullTime(staleBefore.UTC()),
	})
	if err != nil {
		return false, fmt.Errorf("claim broadcast: %w", err)
	}
	return n == 1, nil
}

func (s queryAdapter) HeartbeatBroadcast(ctx context.Context, id, owner, cursor string, c models.BroadcastCounts, now time.Time) (bool, error) {
	n, err := s.q.HeartbeatBroadcast(ctx, sqlitedb.HeartbeatBroadcastParams{
		Now: nullTime(now.UTC()), Cursor: cursor,
		Total: c.Total, Delivered: c.Delivered, Unreachable: c.Unreachable, Failed: c.Failed,
		ID: id, Owner: owner,
	})
	if err != nil {
		return false, fmt.Errorf("heartbeat broadcast: %w", err)
	}
	return n == 1, nil
}

func (s queryAdapter) FinishBroadcast(ctx context.Context, id, owner string, state models.RunState, cursor string, c models.BroadcastCounts, lastError string, now time.Time) (bool, error) {
	n, err := s.q.FinishBroadcast(ctx, sqlitedb.FinishBroadcastParams{
		State: string(state), Now: nullTime(now.UTC()), Cursor: cursor,
		Total: c.Total, Delivered: c.Delivered, Unreachable: c.Unreachable, Failed: c.Failed,
		LastError: lastError, ID: id, Owner: owner,
	})
	if err != nil {
		return false, fmt.Errorf("finish broadcast: %w", err)
	}
	return n == 1, nil
}

func (s queryAdapter) PruneBroadcasts(ctx context.Context, before time.Time) (int64, error) {
	n, err := s.q.PruneBroadcasts(ctx, nullTime(before.UTC()))
	if err != nil {
		return 0, fmt.Errorf("prune broadcasts: %w", err)
	}
	return n, nil
}

func broadcastResult(row sqlitedb.Broadcast, err error, op string) (models.Broadcast, error) {
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Broadcast{}, err
		}
		return models.Broadcast{}, fmt.Errorf("%s: %w", op, err)
	}
	return broadcastOf(row), nil
}

func broadcastOf(row sqlitedb.Broadcast) models.Broadcast {
	return models.Broadcast{
		ID: row.ID, RequestedBy: row.RequestedBy, TokenName: row.TokenName, RequestedAt: row.RequestedAt.UTC(),
		State: models.RunState(row.State), Event: []byte(row.Event), Plan: []byte(row.Plan), Owner: row.Owner,
		HeartbeatAt: row.HeartbeatAt.Time.UTC(), StartedAt: row.StartedAt.Time.UTC(), FinishedAt: row.FinishedAt.Time.UTC(),
		Cursor: row.Cursor,
		BroadcastCounts: models.BroadcastCounts{
			Total: row.Total, Delivered: row.Delivered, Unreachable: row.Unreachable, Failed: row.Failed,
		},
		LastError: row.LastError,
	}
}

package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestScopedTokens(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "tokens.db")
	st, err := NewSQLiteStore(t.Context(), path, MigrateAuto)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := t.Context()

	legacy, err := st.CreateAccessToken(ctx, models.AccessToken{Name: "legacy", TokenHash: "digest-legacy", CreatedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := st.AuthzGeneration(ctx)
	scoped, err := st.CreateAccessToken(ctx, models.AccessToken{Name: "scoped", TokenHash: "digest-scoped", CreatedBy: "alice", Scope: []string{"alertmanager"}, Messages: "self"})
	if err != nil {
		t.Fatal(err)
	}
	if after, _ := st.AuthzGeneration(ctx); after != before+1 {
		t.Errorf("a scoped token did not move the generation: %d → %d", before, after)
	}

	tests := []struct {
		digest    string
		wantID    string
		wantScope []string
		wantMsgs  string
	}{
		{"digest-legacy", legacy.ID, nil, ""},
		{"digest-scoped", scoped.ID, []string{"alertmanager"}, "self"},
	}
	for _, tt := range tests {
		got, err := st.GetAccessTokenByHash(ctx, tt.digest)
		if err != nil || got.ID != tt.wantID || !slices.Equal(got.Scope, tt.wantScope) || got.TokenHash != tt.digest || got.Messages != tt.wantMsgs {
			t.Errorf("GetAccessTokenByHash(%s) = %+v, %v", tt.digest, got, err)
		}
	}
	listed, err := st.ListAccessTokens(ctx)
	if err != nil || len(listed) != 2 || !listed[1].Scoped() || listed[0].Scoped() {
		t.Errorf("ListAccessTokens = %+v, %v", listed, err)
	}

	// The previous release looks a token up by token_hash alone: a scoped one must not match.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var id string
	err = db.QueryRowContext(ctx, `SELECT id FROM access_tokens WHERE token_hash = ?`, "digest-scoped").Scan(&id)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("the previous release's lookup found %q, %v; want nothing", id, err)
	}

	before, _ = st.AuthzGeneration(ctx)
	if err := st.DeleteAccessToken(ctx, scoped.ID); err != nil {
		t.Fatal(err)
	}
	if after, _ := st.AuthzGeneration(ctx); after != before+1 {
		t.Error("deleting a token did not move the generation")
	}
}

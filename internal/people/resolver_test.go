package people

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
)

const aliceID = "0f8c1a2b-3c4d-4e5f-8a9b-0c1d2e3f4a5b"

func TestResolve(t *testing.T) {
	t.Parallel()

	guest := graph.User{ID: "11111111-2222-3333-4444-555555555555", UserPrincipalName: "guest#EXT#@corp.example", AccountEnabled: true, UserType: "Guest"}
	shared1 := member("22222222-2222-3333-4444-555555555555", "carol@corp.example", "team@corp.example")
	shared2 := member("33333333-2222-3333-4444-555555555555", "dave@corp.example", "team@corp.example")

	tests := []struct {
		name       string
		address    string
		wantID     string
		wantErr    error
		wantLookup string
	}{
		{name: "by object id", address: aliceID, wantID: aliceID, wantLookup: LookupGraph},
		{name: "by upn, any case", address: " ALICE@corp.example ", wantID: aliceID, wantLookup: LookupGraph},
		{name: "by mail alias", address: "a.smith@corp.example", wantID: aliceID, wantLookup: LookupGraph},
		{name: "a guest", address: guest.UserPrincipalName, wantErr: ErrIneligible, wantLookup: LookupGraph},
		{name: "nobody", address: "bob@corp.example", wantErr: ErrUnknown, wantLookup: LookupUnknown},
		{name: "not an address", address: "alice", wantErr: ErrInvalidAddress},
		{name: "a mail two people carry", address: "team@corp.example", wantErr: ErrAmbiguous, wantLookup: LookupGraph},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := newFakeRecorder()
			r := NewResolver(openStore(t), newFakeGraph(member(aliceID, "alice@corp.example", "a.smith@corp.example"), guest, shared1, shared2), rec, "tenant", time.Hour)

			u, err := r.Resolve(t.Context(), tt.address)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("Resolve(%q) error = %v, want %v", tt.address, err, tt.wantErr)
			}
			if tt.wantErr == nil && (u.AADObjectID != tt.wantID || u.TenantID != "tenant") {
				t.Errorf("Resolve(%q) = %+v", tt.address, u)
			}
			if tt.wantLookup != "" && rec.lookups[tt.wantLookup] != 1 {
				t.Errorf("lookups = %v, want one %s", rec.lookups, tt.wantLookup)
			}
		})
	}
}

func TestResolveUsesTheStoreWhileFresh(t *testing.T) {
	t.Parallel()

	g := newFakeGraph(member(aliceID, "alice@corp.example", "a.smith@corp.example"))
	rec := newFakeRecorder()
	r := NewResolver(openStore(t), g, rec, "tenant", time.Hour)
	c := &clock{t: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	r.now = c.now

	for _, address := range []string{"alice@corp.example", "a.smith@corp.example", aliceID} {
		if _, err := r.Resolve(t.Context(), address); err != nil {
			t.Fatalf("Resolve(%q): %v", address, err)
		}
	}
	if g.gets != 1 || rec.lookups[LookupStore] != 2 {
		t.Errorf("graph gets = %d, lookups = %v; want one Graph call and two store hits", g.gets, rec.lookups)
	}

	// Stale: Graph is asked again, and a change of name is picked up.
	c.advance(2 * time.Hour)
	renamed := member(aliceID, "alice@corp.example", "a.smith@corp.example")
	renamed.GivenName = "Alicia"
	g.users[aliceID] = renamed
	u, err := r.Resolve(t.Context(), "alice@corp.example")
	if err != nil || u.GivenName != "Alicia" || g.gets != 2 {
		t.Errorf("stale Resolve() = %+v, %v, gets %d; want Graph asked again", u, err, g.gets)
	}
}

func TestResolveRemembersUnknownAddresses(t *testing.T) {
	t.Parallel()

	g := newFakeGraph()
	rec := newFakeRecorder()
	r := NewResolver(openStore(t), g, rec, "tenant", time.Hour)
	c := &clock{t: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	r.now = c.now

	for range 3 {
		if _, err := r.Resolve(t.Context(), "typo@corp.example"); !errors.Is(err, ErrUnknown) {
			t.Fatalf("Resolve() = %v, want ErrUnknown", err)
		}
	}
	if g.gets != 1 || rec.lookups[LookupNegative] != 2 {
		t.Errorf("gets = %d, lookups = %v; want Graph asked once", g.gets, rec.lookups)
	}

	c.advance(negativeTTL + time.Second)
	_, _ = r.Resolve(t.Context(), "typo@corp.example")
	if g.gets != 2 {
		t.Errorf("gets after the negative TTL = %d, want Graph asked again", g.gets)
	}
}

func TestResolveNegativeCacheIsBounded(t *testing.T) {
	t.Parallel()

	r := NewResolver(openStore(t), newFakeGraph(), newFakeRecorder(), "tenant", time.Hour)
	for i := range negativeSize + 10 {
		r.rememberUnknown(fmt.Sprintf("u%d@corp.example", i))
	}
	if len(r.unknown) > negativeSize {
		t.Errorf("negative cache holds %d entries, want at most %d", len(r.unknown), negativeSize)
	}
}

func TestResolveReportsTransientFailures(t *testing.T) {
	t.Parallel()

	g := newFakeGraph()
	g.getErr = &graph.APIError{Status: 503}
	r := NewResolver(openStore(t), g, newFakeRecorder(), "tenant", time.Hour)

	_, err := r.Resolve(t.Context(), "alice@corp.example")
	if err == nil || Reason(err) != "" {
		t.Errorf("Resolve() = %v, want a transient error without a reason", err)
	}
}

func TestReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("x: %w", ErrInvalidAddress), "invalid-address"},
		{fmt.Errorf("x: %w", ErrUnknown), "unknown-recipient"},
		{fmt.Errorf("x: %w", ErrIneligible), "ineligible"},
		{fmt.Errorf("x: %w", ErrNotInstalled), "not-installed"},
		{errors.New("boom"), ""},
	}
	for _, tt := range tests {
		if got := Reason(tt.err); got != tt.want {
			t.Errorf("Reason(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

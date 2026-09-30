package people

import (
	"errors"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

const botServiceURL = "https://smba.example/teams/"

// installerFixture is an installer over a store holding one eligible person.
func installerFixture(t *testing.T, g *fakeGraph, chats *fakeChats, global bool) (*Installer, store.Store, models.DirectoryUser) {
	t.Helper()
	st := openStore(t)
	if err := st.UpsertDirectoryUser(t.Context(), models.DirectoryUser{AADObjectID: "oid-1", TenantID: "tenant", Eligible: true}); err != nil {
		t.Fatal(err)
	}
	u, err := st.GetDirectoryUser(t.Context(), "oid-1")
	if err != nil {
		t.Fatal(err)
	}
	inst := NewInstaller(st, g, chats, newFakeRecorder(), InstallerConfig{BotID: "bot", ServiceURL: botServiceURL, AppID: "app", Global: global})
	inst.sleep = noSleep
	return inst, st, u
}

func TestEnsure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		global       bool
		preinstalled bool
		chatDelay    int
		installFn    func(string) (bool, error)
		wantOutcome  string
		wantState    models.InstallState
		wantErr      error
		wantInstalls int
	}{
		{name: "installed by a setup policy", global: true, preinstalled: true, wantOutcome: OutcomeAlready, wantState: models.InstallInstalled},
		{name: "found without global install", preinstalled: true, wantOutcome: OutcomeAlready, wantState: models.InstallInstalled},
		{name: "missing without global install", wantOutcome: OutcomeFailed, wantState: models.InstallUnknown, wantErr: ErrNotInstalled},
		{name: "installed now", global: true, wantOutcome: OutcomeInstalled, wantState: models.InstallInstalled, wantInstalls: 1},
		{name: "chat opens after a moment", global: true, chatDelay: 2, wantOutcome: OutcomeInstalled, wantState: models.InstallInstalled, wantInstalls: 1},
		{name: "chat never opens", global: true, chatDelay: chatAttempts, wantOutcome: OutcomeFailed, wantState: models.InstallFailed, wantErr: ErrNotInstalled, wantInstalls: 1},
		{
			name: "graph says already installed", global: true, wantOutcome: OutcomeAlready, wantState: models.InstallInstalled, wantInstalls: 1,
			installFn: func(string) (bool, error) { return true, nil },
		},
		{
			name: "refused for this person", global: true, wantOutcome: OutcomeIneligible, wantState: models.InstallIneligible, wantErr: ErrNotInstalled, wantInstalls: 1,
			installFn: func(string) (bool, error) { return false, &graph.APIError{Status: 404, Code: "NotFound"} },
		},
		{
			name: "graph unavailable", global: true, wantOutcome: OutcomeFailed, wantState: models.InstallFailed, wantErr: ErrNotInstalled, wantInstalls: 1,
			installFn: func(string) (bool, error) { return false, &graph.APIError{Status: 503} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newFakeGraph()
			chats := newFakeChats()
			if tt.preinstalled {
				chats.install("oid-1")
			}
			g.installFn = func(id string) (bool, error) {
				if tt.installFn != nil {
					already, err := tt.installFn(id)
					if err == nil {
						chats.install(id)
					}
					return already, err
				}
				chats.install(id)
				chats.mu.Lock()
				chats.failFirst[id] = tt.chatDelay
				chats.mu.Unlock()
				return false, nil
			}
			inst, st, u := installerFixture(t, g, chats, tt.global)

			got, outcome, err := inst.Ensure(t.Context(), u, false, true)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("Ensure() error = %v, want %v", err, tt.wantErr)
			}
			if outcome != tt.wantOutcome || got.InstallState != tt.wantState {
				t.Errorf("Ensure() = %s, %s; want %s, %s", outcome, got.InstallState, tt.wantOutcome, tt.wantState)
			}
			if g.installs != tt.wantInstalls {
				t.Errorf("installs = %d, want %d", g.installs, tt.wantInstalls)
			}
			stored, _ := st.GetDirectoryUser(t.Context(), "oid-1")
			if stored.InstallState != tt.wantState {
				t.Errorf("stored state = %s, want %s", stored.InstallState, tt.wantState)
			}
			if tt.wantState == models.InstallInstalled && (stored.ConversationID != "a:oid-1" || stored.ServiceURL != botServiceURL) {
				t.Errorf("stored chat = %q at %q", stored.ConversationID, stored.ServiceURL)
			}
			if tt.wantState == models.InstallFailed && (stored.Attempts != 1 || !stored.NextAttemptAt.After(stored.UpdatedAt)) {
				t.Errorf("stored failure = %+v, want one attempt and a later retry", stored)
			}
		})
	}
}

func TestEnsureTrustsAStoredChatUnlessVerifying(t *testing.T) {
	t.Parallel()

	chats := newFakeChats("oid-1")
	inst, _, u := installerFixture(t, newFakeGraph(), chats, true)
	u.InstallState, u.ConversationID = models.InstallInstalled, "a:oid-1"

	if _, outcome, err := inst.Ensure(t.Context(), u, false, false); err != nil || outcome != OutcomeAlready || chats.calls != 0 {
		t.Errorf("Ensure(trusted) = %s, %v, %d chat calls; want no call", outcome, err, chats.calls)
	}
	if _, _, err := inst.Ensure(t.Context(), u, true, false); err != nil || chats.calls != 1 {
		t.Errorf("Ensure(verify) = %v, %d chat calls; want one", err, chats.calls)
	}
}

func TestEnsureStopsInstallingWithoutPermission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *graph.APIError
	}{
		{name: "directory refusal", err: &graph.APIError{Status: 403, Code: "Authorization_RequestDenied"}},
		{name: "missing Teams role", err: &graph.APIError{Status: 403, Code: "Forbidden", Message: "Missing role permissions on the request. API requires one of 'TeamsAppInstallation.ReadWriteForUser.All'."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newFakeGraph()
			g.installFn = func(string) (bool, error) { return false, tt.err }
			inst, st, u := installerFixture(t, g, newFakeChats(), true)

			for range 3 {
				if _, _, err := inst.Ensure(t.Context(), u, false, true); !errors.Is(err, ErrNotInstalled) {
					t.Fatalf("Ensure() = %v, want ErrNotInstalled", err)
				}
			}
			if g.installs != 1 {
				t.Errorf("installs = %d, want Graph asked once, then skipped", g.installs)
			}
			if got, _ := st.GetDirectoryUser(t.Context(), u.AADObjectID); got.InstallState != models.InstallFailed {
				t.Errorf("install state = %s, want failed, not a verdict on the person", got.InstallState)
			}

			inst.ResetPermission()
			_, _, _ = inst.Ensure(t.Context(), u, false, true)
			if g.installs != 2 {
				t.Errorf("installs after ResetPermission = %d, want asked again", g.installs)
			}
		})
	}
}

func TestEnsureResolvesTheCatalogOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		catalogAppID string
		catalogErr   error
		wantCatalogs int
		wantErr      bool
	}{
		{name: "looked up once", wantCatalogs: 1},
		{name: "configured", catalogAppID: "cat-9", wantCatalogs: 0},
		{name: "not in the catalog", catalogErr: graph.ErrNotFound, wantCatalogs: 1, wantErr: true},
		{name: "catalog unreachable", catalogErr: &graph.APIError{Status: 503}, wantCatalogs: 2, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newFakeGraph()
			chats := newFakeChats()
			g.installFn = func(id string) (bool, error) {
				chats.install(id)
				return false, nil
			}
			if tt.catalogErr != nil {
				g.catalogFn = func() (string, error) { return "", tt.catalogErr }
			}
			inst, _, u := installerFixture(t, g, chats, true)
			inst.catalogID = tt.catalogAppID

			for range 2 {
				_, _, err := inst.Ensure(t.Context(), u, true, true)
				if (err != nil) != tt.wantErr {
					t.Fatalf("Ensure() = %v, want error %v", err, tt.wantErr)
				}
			}
			if g.catalogs != tt.wantCatalogs {
				t.Errorf("catalog lookups = %d, want %d", g.catalogs, tt.wantCatalogs)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		attempts int64
		want     time.Duration
	}{
		{0, time.Hour},
		{1, 2 * time.Hour},
		{3, 8 * time.Hour},
		{7, 128 * time.Hour},
		{8, maxBackoff},
		{100, maxBackoff},
	}
	for _, tt := range tests {
		if got := backoff(tt.attempts); got != tt.want {
			t.Errorf("backoff(%d) = %v, want %v", tt.attempts, got, tt.want)
		}
	}
}

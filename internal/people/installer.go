package people

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
)

const (
	// minBackoff and maxBackoff bound how long a failed install waits.
	minBackoff = time.Hour
	maxBackoff = 7 * 24 * time.Hour
	// chatAttempts is how often the chat is asked for after an install:
	// Teams takes a moment before the new installation answers.
	chatAttempts = 3
	chatWait     = 2 * time.Second
)

// InstallerConfig is what an Installer needs from the bot configuration.
type InstallerConfig struct {
	// BotID is the bot's app id, bot.client-id.
	BotID string
	// ServiceURL reaches a person whose own endpoint is not known yet.
	ServiceURL string
	// AppID and CatalogAppID find the app in the organization catalog.
	AppID, CatalogAppID string
	// Global allows installing; without it the installer only finds chats.
	Global bool
}

// Installer makes sure the bot has a chat with a person, installing its Teams
// app through Graph when it has to and may.
type Installer struct {
	store Store
	graph Directory
	chats Chats
	rec   Recorder
	cfg   InstallerConfig
	now   func() time.Time
	sleep func(context.Context, time.Duration) error

	catalogMu  sync.Mutex
	catalogID  string
	catalogErr error
	// forbidden is set once Graph refuses the install permission itself, so the
	// rest of a run discovers chats without asking again.
	forbidden atomic.Bool
}

func NewInstaller(st Store, dir Directory, chats Chats, rec Recorder, cfg InstallerConfig) *Installer {
	return &Installer{
		store: st, graph: dir, chats: chats, rec: rec, cfg: cfg,
		now:       func() time.Time { return time.Now().UTC() },
		sleep:     sleep,
		catalogID: cfg.CatalogAppID,
	}
}

// ResetPermission forgets that Graph refused the install permission or had no
// such app, so a new run tries again after consent or publishing.
func (i *Installer) ResetPermission() {
	i.forbidden.Store(false)
	i.catalogMu.Lock()
	defer i.catalogMu.Unlock()
	i.catalogErr = nil
}

// Ensure returns the person with a chat the bot can post to. A stored chat is
// trusted unless verify is set. install says whether this call may install the
// app; a message may, within its budget, and a reconcile always may. Ensure
// records the outcome on the person and reports it.
func (i *Installer) Ensure(ctx context.Context, u models.DirectoryUser, verify, install bool) (models.DirectoryUser, string, error) {
	if !verify && u.InstallState == models.InstallInstalled && u.ConversationID != "" {
		return u, OutcomeAlready, nil
	}

	// The chat opens whenever the app is there, however it got there -- a setup
	// policy included -- so nothing is written to Graph for those people.
	if conv, err := i.openChat(ctx, u); err == nil {
		return i.installed(ctx, u, conv, OutcomeAlready)
	}

	if !install || !i.cfg.Global || i.forbidden.Load() {
		return u, OutcomeFailed, fmt.Errorf("%s: %w", u.AADObjectID, ErrNotInstalled)
	}

	catalogID, err := i.catalog(ctx)
	if err != nil {
		return u, OutcomeFailed, err
	}
	already, err := i.graph.InstallAppForUser(ctx, u.AADObjectID, catalogID)
	if err != nil {
		return i.failed(ctx, u, err)
	}

	outcome := OutcomeInstalled
	if already {
		outcome = OutcomeAlready
	}
	var conv string
	for attempt := range chatAttempts {
		if conv, err = i.openChat(ctx, u); err == nil {
			return i.installed(ctx, u, conv, outcome)
		}
		if attempt < chatAttempts-1 {
			if err := i.sleep(ctx, chatWait<<attempt); err != nil {
				return u, OutcomeFailed, err
			}
		}
	}
	return i.failed(ctx, u, fmt.Errorf("installed, but no chat opened: %w", err))
}

func (i *Installer) openChat(ctx context.Context, u models.DirectoryUser) (string, error) {
	serviceURL := u.ServiceURL
	if serviceURL == "" {
		serviceURL = i.cfg.ServiceURL
	}
	return i.chats.CreatePersonalConversation(ctx, serviceURL, u.TenantID, i.cfg.BotID, u.AADObjectID)
}

func (i *Installer) installed(ctx context.Context, u models.DirectoryUser, conv, outcome string) (models.DirectoryUser, string, error) {
	now := i.now()
	if u.ServiceURL == "" {
		u.ServiceURL = i.cfg.ServiceURL
	}
	if err := i.store.SetDirectoryUserInstalled(ctx, u.AADObjectID, conv, u.ServiceURL, now); err != nil {
		return u, OutcomeFailed, err
	}
	u.ConversationID, u.InstallState, u.InstalledAt = conv, models.InstallInstalled, now
	u.Attempts, u.LastError, u.NextAttemptAt = 0, "", time.Time{}
	i.rec.AppInstall(ctx, outcome)
	return u, outcome, nil
}

// failed records a refused install. A missing permission stops installs for
// the rest of the run; another refusal about this person is ineligible;
// anything else is retried later.
func (i *Installer) failed(ctx context.Context, u models.DirectoryUser, cause error) (models.DirectoryUser, string, error) {
	state, outcome := models.InstallFailed, OutcomeFailed
	var apiErr *graph.APIError
	switch {
	case permissionDenied(cause):
		i.forbidden.Store(true)
	case errors.As(cause, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 && apiErr.Status != http.StatusTooManyRequests:
		state, outcome = models.InstallIneligible, OutcomeIneligible
	}

	now := i.now()
	next := now.Add(backoff(u.Attempts))
	if err := i.store.RecordDirectoryInstallFailure(ctx, u.AADObjectID, state, cause.Error(), next, now); err != nil {
		return u, outcome, errors.Join(cause, err)
	}
	u.InstallState, u.LastError, u.NextAttemptAt = state, cause.Error(), next
	u.Attempts++
	i.rec.AppInstall(ctx, outcome)
	return u, outcome, fmt.Errorf("%s: %w: %w", u.AADObjectID, ErrNotInstalled, cause)
}

// catalog finds the app's catalog id once and remembers it. A missing app is
// remembered too, until ResetPermission, so a run asks once rather than per person.
func (i *Installer) catalog(ctx context.Context) (string, error) {
	i.catalogMu.Lock()
	defer i.catalogMu.Unlock()
	if i.catalogID != "" {
		return i.catalogID, nil
	}
	if i.catalogErr != nil {
		return "", i.catalogErr
	}
	id, err := i.graph.ResolveCatalogApp(ctx, i.cfg.AppID, i.cfg.BotID)
	if errors.Is(err, graph.ErrNotFound) {
		i.catalogErr = fmt.Errorf("find the Teams app in the catalog: %w", err)
		return "", i.catalogErr
	}
	if err != nil {
		return "", fmt.Errorf("find the Teams app in the catalog: %w", err)
	}
	i.catalogID = id
	return id, nil
}

// backoff doubles from an hour per failed attempt, up to a week.
func backoff(attempts int64) time.Duration {
	d := minBackoff
	for range attempts {
		if d >= maxBackoff {
			break
		}
		d *= 2
	}
	return min(d, maxBackoff)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

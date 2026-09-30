// Package people finds the people a message names and makes sure the bot can
// reach them: it resolves addresses against the directory, installs the Teams
// app for a person, and reconciles installs for the whole tenant (ADR 0059).
package people

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// Why a person cannot be reached. Each is permanent for the message at hand,
// so a sender retrying would get the same answer.
var (
	ErrInvalidAddress = errors.New("not an object id, UPN or mail address")
	ErrUnknown        = errors.New("no such person in the directory")
	ErrIneligible     = errors.New("not an enabled member of the tenant")
	ErrNotInstalled   = errors.New("the Teams app is not installed for this person")
)

// Reason names err for a response body or a metric: one of the sentinels
// above, or "" for anything else.
func Reason(err error) string {
	switch {
	case errors.Is(err, ErrInvalidAddress):
		return "invalid-address"
	case errors.Is(err, ErrUnknown):
		return "unknown-recipient"
	case errors.Is(err, ErrIneligible):
		return "ineligible"
	case errors.Is(err, ErrNotInstalled):
		return "not-installed"
	default:
		return ""
	}
}

// permissionDenied reports whether Graph refused err because the registration
// lacks a permission, rather than because of the object asked about. The Teams
// endpoints say so with a generic Forbidden code, so only their message tells.
func permissionDenied(err error) bool {
	var apiErr *graph.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		return false
	}
	return apiErr.Code == "Authorization_RequestDenied" || strings.HasPrefix(apiErr.Message, "Missing role permissions")
}

// Store is the part of store.Store this package uses.
type Store interface {
	UpsertDirectoryUser(ctx context.Context, u models.DirectoryUser) error
	GetDirectoryUser(ctx context.Context, aadObjectID string) (models.DirectoryUser, error)
	FindDirectoryUser(ctx context.Context, address string) (models.DirectoryUser, error)
	SetDirectoryUserInstalled(ctx context.Context, aadObjectID, conversationID, serviceURL string, at time.Time) error
	RecordDirectoryInstallFailure(ctx context.Context, aadObjectID string, state models.InstallState, lastError string, next, at time.Time) error
	ListDirectoryUsersDue(ctx context.Context, now, reverifyBefore time.Time, ignoreBackoff bool, limit int) ([]models.DirectoryUser, error)
	MarkDirectoryUsersDeparted(ctx context.Context, seenBefore, at time.Time) (int64, error)
	PurgeDepartedDirectoryUsers(ctx context.Context, before time.Time) (int64, error)

	RequestDirectoryRun(ctx context.Context, r models.DirectoryRun) (models.DirectoryRun, error)
	LatestDirectoryRun(ctx context.Context) (models.DirectoryRun, error)
	ClaimDirectoryRun(ctx context.Context, id, owner string, now, staleBefore time.Time) (bool, error)
	HeartbeatDirectoryRun(ctx context.Context, id, owner string, counts models.RunCounts, now time.Time) (bool, error)
	FinishDirectoryRun(ctx context.Context, id, owner string, state models.RunState, counts models.RunCounts, lastError string, now time.Time) (bool, error)
	PruneDirectoryRuns(ctx context.Context, before time.Time) (int64, error)
}

// Directory is the part of graph.Client this package uses.
type Directory interface {
	GetUser(ctx context.Context, idOrUPN string) (graph.User, error)
	FindUserByMail(ctx context.Context, mail string) (graph.User, error)
	ListMemberUsers(ctx context.Context, fn func([]graph.User) error) error
	ResolveCatalogApp(ctx context.Context, manifestID, botID string) (string, error)
	InstallAppForUser(ctx context.Context, userID, catalogAppID string) (already bool, err error)
}

// Chats opens the bot's 1:1 chat with a person; bot.Client does.
type Chats interface {
	CreatePersonalConversation(ctx context.Context, serviceURL, tenantID, botID, userObjectID string) (string, error)
}

// Recorder counts what this package does; metrics.Metrics implements it.
type Recorder interface {
	AppInstall(ctx context.Context, outcome string)
	DirectoryLookup(ctx context.Context, result string)
	ReconcileRun(ctx context.Context, kind, outcome string)
}

// Install outcomes, as counted by a run and by Recorder.AppInstall.
const (
	OutcomeInstalled  = "installed"
	OutcomeAlready    = "already"
	OutcomeFailed     = "failed"
	OutcomeIneligible = "ineligible"
)

// Lookup results, as counted by Recorder.DirectoryLookup.
const (
	LookupStore    = "store"
	LookupGraph    = "graph"
	LookupNegative = "negative-cache"
	LookupUnknown  = "unknown"
)

// eligible is who ADR 0059 installs for: enabled members, not guests.
func eligible(u graph.User) bool {
	return u.AccountEnabled && u.UserType == "Member"
}

func directoryUserOf(u graph.User, tenantID string, seen time.Time) models.DirectoryUser {
	return models.DirectoryUser{
		AADObjectID:       u.ID,
		TenantID:          tenantID,
		UserPrincipalName: u.UserPrincipalName,
		Mail:              u.Mail,
		DisplayName:       u.DisplayName,
		GivenName:         u.GivenName,
		Surname:           u.Surname,
		Eligible:          eligible(u),
		DirectorySeenAt:   seen,
	}
}

// Finder resolves an address and makes sure the bot can reach the person,
// which is what delivering an addressed message needs.
type Finder struct {
	*Resolver
	*Installer
}

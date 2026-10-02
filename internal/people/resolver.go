package people

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// objectID is an Entra object id: a GUID.
var objectID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const (
	// negativeTTL is how long an address Graph does not know is not asked
	// about again, so a sender's typo is not a Graph call per message.
	negativeTTL = 10 * time.Minute
	// negativeSize bounds that memory.
	negativeSize = 1024
)

// Resolver turns an address into a directory user, from the store while the
// row is fresh and from Graph otherwise.
type Resolver struct {
	store    Store
	graph    Directory
	rec      Recorder
	tenantID string
	ttl      time.Duration
	now      func() time.Time

	mu      sync.Mutex
	unknown map[string]time.Time
}

// NewResolver builds a resolver. tenantID is the directory's tenant, ttl how
// long a stored row is trusted.
func NewResolver(st Store, dir Directory, rec Recorder, tenantID string, ttl time.Duration) *Resolver {
	return &Resolver{
		store: st, graph: dir, rec: rec, tenantID: tenantID, ttl: ttl,
		now:     func() time.Time { return time.Now().UTC() },
		unknown: map[string]time.Time{},
	}
}

// Resolve finds the person an object id, UPN or mail address names. It fails
// with ErrInvalidAddress, ErrUnknown or ErrIneligible for a permanent reason,
// and with any other error for one a retry may fix.
func (r *Resolver) Resolve(ctx context.Context, address string) (models.DirectoryUser, error) {
	address = strings.TrimSpace(address)
	key := strings.ToLower(address)
	isID := objectID.MatchString(address)
	if !isID && !strings.Contains(address, "@") {
		return models.DirectoryUser{}, fmt.Errorf("%q: %w", address, ErrInvalidAddress)
	}

	var stored models.DirectoryUser
	var err error
	if isID {
		stored, err = r.store.GetDirectoryUser(ctx, key)
	} else {
		stored, err = r.store.FindDirectoryUser(ctx, key)
	}
	switch {
	case err == nil && r.now().Sub(stored.DirectorySeenAt) < r.ttl:
		r.rec.DirectoryLookup(ctx, LookupStore)
		return checkEligible(address, stored)
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return models.DirectoryUser{}, err
	}

	if r.knownUnknown(key) {
		r.rec.DirectoryLookup(ctx, LookupNegative)
		return models.DirectoryUser{}, fmt.Errorf("%q: %w", address, ErrUnknown)
	}

	u, err := r.fromGraph(ctx, address, isID)
	if errors.Is(err, graph.ErrAmbiguous) {
		// Permanent until the directory changes: a retry names nobody either.
		r.rec.DirectoryLookup(ctx, LookupGraph)
		return models.DirectoryUser{}, fmt.Errorf("%q: %w", address, ErrAmbiguous)
	}
	if errors.Is(err, graph.ErrNotFound) {
		r.rememberUnknown(key)
		r.rec.DirectoryLookup(ctx, LookupUnknown)
		return models.DirectoryUser{}, fmt.Errorf("%q: %w", address, ErrUnknown)
	}
	if err != nil {
		return models.DirectoryUser{}, err
	}
	r.rec.DirectoryLookup(ctx, LookupGraph)

	if err := r.store.UpsertDirectoryUser(ctx, directoryUserOf(u, r.tenantID, r.now())); err != nil {
		return models.DirectoryUser{}, err
	}
	fresh, err := r.store.GetDirectoryUser(ctx, u.ID)
	if err != nil {
		return models.DirectoryUser{}, err
	}
	return checkEligible(address, fresh)
}

// fromGraph asks by object id or UPN, and by mail when the address is not a UPN.
func (r *Resolver) fromGraph(ctx context.Context, address string, isID bool) (graph.User, error) {
	u, err := r.graph.GetUser(ctx, address)
	if isID || !errors.Is(err, graph.ErrNotFound) {
		return u, err
	}
	return r.graph.FindUserByMail(ctx, address)
}

func checkEligible(address string, u models.DirectoryUser) (models.DirectoryUser, error) {
	if !u.Eligible || u.InstallState == models.InstallDeparted {
		return u, fmt.Errorf("%q: %w", address, ErrIneligible)
	}
	return u, nil
}

func (r *Resolver) knownUnknown(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.unknown[key]
	if ok && r.now().After(until) {
		delete(r.unknown, key)
		return false
	}
	return ok
}

func (r *Resolver) rememberUnknown(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if len(r.unknown) >= negativeSize {
		for k, until := range r.unknown {
			if now.After(until) {
				delete(r.unknown, k)
			}
		}
		// Still full of live entries: forgetting one early costs a Graph call.
		for k := range r.unknown {
			if len(r.unknown) < negativeSize {
				break
			}
			delete(r.unknown, k)
		}
	}
	r.unknown[key] = now.Add(negativeTTL)
}

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// The rules in this file are the ones a second backend would otherwise be
// tempted to re-derive. They are small enough to look obvious and each one is
// a decision: what an empty id means, which clock a row is stamped from, and
// when a row that exists still counts as missing. Two implementations that
// disagreed about any of them would differ in ways no type checks.

// newID lets a caller choose the key — an import carries the ids from the
// bundle it is restoring — and generates one otherwise.
func newID(id string) string {
	if id != "" {
		return id
	}
	return uuid.NewString()
}

// nowUTC is the one clock rows are stamped from. It is UTC because a database
// that travels between machines should not carry their offsets with it.
func nowUTC() time.Time {
	return time.Now().UTC()
}

// notFound maps the driver's empty result onto the store's own error, so no
// caller has to know what database/sql calls it.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// liveSession reports an expired session as missing. Checking the time here
// rather than in every caller is what stops one of them from forgetting.
func liveSession(session models.Session) (models.Session, error) {
	if !session.ExpiresAt.After(time.Now()) {
		return models.Session{}, ErrNotFound
	}
	return session, nil
}

// liveFlow does the same for a login flow. Note the order it is used in: the
// row is deleted first and judged afterwards, so an expired state is spent
// rather than left for a second attempt.
func liveFlow(flow models.LoginFlow) (models.LoginFlow, error) {
	if !flow.ExpiresAt.After(time.Now()) {
		return models.LoginFlow{}, ErrNotFound
	}
	return flow, nil
}

// liveLinkFlow is liveFlow for the one-time code that binds a chat to a person,
// and is used the same way round.
func liveLinkFlow(flow models.LinkFlow) (models.LinkFlow, error) {
	if !flow.ExpiresAt.After(time.Now()) {
		return models.LinkFlow{}, ErrNotFound
	}
	return flow, nil
}

// A selector is stored as JSON text. Corruption reads back as "matches
// everything", which is the same thing an empty selector means, because a
// route that silently stops matching is harder to notice than one that matches
// too much.
func serializeSelector(selector map[string]string) string {
	if selector == nil {
		return "{}"
	}
	data, err := json.Marshal(selector)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func parseSelector(raw string) map[string]string {
	if raw == "" {
		return map[string]string{}
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]string{}
	}
	if out == nil {
		return map[string]string{}
	}
	return out
}

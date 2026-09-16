package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store/sqlitedb"
)

// dialectQueries is the query surface a backend provides. It is spelled as
// sqlitedb.Querier because that interface is generated from the .sql files and
// is therefore, by construction, exactly the set of statements this service
// runs -- naming it by hand would be one more thing to keep in step.
//
// Postgres reaches it through pgQueries, which converts the parameter and row
// structs. sqlc emits them field for field identical from the same queries, so
// the conversions are free and the compiler refuses them the moment the two
// stop matching.
type dialectQueries = sqlitedb.Querier

// queryAdapter is everything that only needs somewhere to run a statement: the
// mapping between the generated rows and internal/models, written once and
// shared by both backends and by the transaction-bound view of each.
type queryAdapter struct {
	q dialectQueries
}

func (s queryAdapter) ListTemplates(ctx context.Context) ([]models.Template, error) {
	rows, err := s.q.ListTemplates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.Template, 0, len(rows))
	for _, row := range rows {
		out = append(out, templateOf(row))
	}
	return out, nil
}

func (s queryAdapter) CreateTemplate(ctx context.Context, t models.Template) (models.Template, error) {
	t.ID = newID(t.ID)
	t.CreatedAt = nowUTC()
	t.UpdatedAt = t.CreatedAt

	err := s.q.CreateTemplate(ctx, sqlitedb.CreateTemplateParams{
		ID:          t.ID,
		Name:        t.Name,
		Title:       t.Title,
		MessageText: t.Text,
		Body:        t.Body,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	})
	if err != nil {
		return models.Template{}, fmt.Errorf("create template: %w", err)
	}
	return t, nil
}

func (s queryAdapter) UpdateTemplate(ctx context.Context, t models.Template) (models.Template, error) {
	if t.ID == "" {
		return models.Template{}, errors.New("template id is required")
	}
	t.UpdatedAt = nowUTC()

	err := s.q.UpdateTemplate(ctx, sqlitedb.UpdateTemplateParams{
		Name:        t.Name,
		Title:       t.Title,
		MessageText: t.Text,
		Body:        t.Body,
		UpdatedAt:   t.UpdatedAt,
		ID:          t.ID,
	})
	if err != nil {
		return models.Template{}, fmt.Errorf("update template: %w", err)
	}
	return t, nil
}

func (s queryAdapter) DeleteTemplate(ctx context.Context, id string) error {
	if err := s.q.DeleteTemplate(ctx, id); err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}

func (s queryAdapter) GetTemplate(ctx context.Context, id string) (models.Template, error) {
	row, err := s.q.GetTemplate(ctx, id)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Template{}, err
		}
		return models.Template{}, fmt.Errorf("get template: %w", err)
	}
	return templateOf(row), nil
}

func templateOf(row sqlitedb.Template) models.Template {
	return models.Template{
		ID:        row.ID,
		Name:      row.Name,
		Title:     row.Title,
		Text:      row.MessageText,
		Body:      row.Body,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func (s queryAdapter) ListDestinations(ctx context.Context) ([]models.Destination, error) {
	rows, err := s.q.ListDestinations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list destinations: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.Destination, 0, len(rows))
	for _, row := range rows {
		out = append(out, destinationOf(row))
	}
	return out, nil
}

func (s queryAdapter) CreateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	d.ID = newID(d.ID)
	d.CreatedAt = nowUTC()
	d.UpdatedAt = d.CreatedAt

	err := s.q.CreateDestination(ctx, sqlitedb.CreateDestinationParams{
		ID:        d.ID,
		Name:      d.Name,
		TeamID:    d.TeamID,
		ChannelID: d.ChannelID,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	})
	if err != nil {
		return models.Destination{}, fmt.Errorf("create destination: %w", err)
	}
	return d, nil
}

func (s queryAdapter) UpdateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	if d.ID == "" {
		return models.Destination{}, errors.New("destination id is required")
	}
	d.UpdatedAt = nowUTC()

	err := s.q.UpdateDestination(ctx, sqlitedb.UpdateDestinationParams{
		Name:      d.Name,
		TeamID:    d.TeamID,
		ChannelID: d.ChannelID,
		UpdatedAt: d.UpdatedAt,
		ID:        d.ID,
	})
	if err != nil {
		return models.Destination{}, fmt.Errorf("update destination: %w", err)
	}
	return d, nil
}

func (s queryAdapter) DeleteDestination(ctx context.Context, id string) error {
	if err := s.q.DeleteDestination(ctx, id); err != nil {
		return fmt.Errorf("delete destination: %w", err)
	}
	return nil
}

func (s queryAdapter) GetDestination(ctx context.Context, id string) (models.Destination, error) {
	row, err := s.q.GetDestination(ctx, id)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Destination{}, err
		}
		return models.Destination{}, fmt.Errorf("get destination: %w", err)
	}
	return destinationOf(row), nil
}

func destinationOf(row sqlitedb.Destination) models.Destination {
	return models.Destination{
		ID:        row.ID,
		Name:      row.Name,
		TeamID:    row.TeamID,
		ChannelID: row.ChannelID,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func (s queryAdapter) ListRecipients(ctx context.Context) ([]models.Recipient, error) {
	rows, err := s.q.ListRecipients(ctx)
	if err != nil {
		return nil, fmt.Errorf("list recipients: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.Recipient, 0, len(rows))
	for _, row := range rows {
		out = append(out, recipientOf(row))
	}
	return out, nil
}

func (s queryAdapter) CreateRecipient(ctx context.Context, r models.Recipient) (models.Recipient, error) {
	r.ID = newID(r.ID)
	r.CreatedAt = nowUTC()
	r.UpdatedAt = r.CreatedAt

	err := s.q.CreateRecipient(ctx, sqlitedb.CreateRecipientParams{
		ID:             r.ID,
		Subject:        r.Subject,
		Name:           r.Name,
		AadObjectID:    r.AADObjectID,
		ConversationID: r.ConversationID,
		ServiceUrl:     r.ServiceURL,
		BotChannelID:   r.BotChannelID,
		TenantID:       r.TenantID,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	})
	if err != nil {
		return models.Recipient{}, fmt.Errorf("create recipient: %w", err)
	}
	return r, nil
}

// The subject is not among the columns written: it is who the binding belongs
// to, and a re-link changes the conversation, never the person.
func (s queryAdapter) UpdateRecipient(ctx context.Context, r models.Recipient) (models.Recipient, error) {
	if r.ID == "" {
		return models.Recipient{}, errors.New("recipient id is required")
	}
	r.UpdatedAt = nowUTC()

	err := s.q.UpdateRecipient(ctx, sqlitedb.UpdateRecipientParams{
		Name:           r.Name,
		AadObjectID:    r.AADObjectID,
		ConversationID: r.ConversationID,
		ServiceUrl:     r.ServiceURL,
		BotChannelID:   r.BotChannelID,
		TenantID:       r.TenantID,
		UpdatedAt:      r.UpdatedAt,
		ID:             r.ID,
	})
	if err != nil {
		return models.Recipient{}, fmt.Errorf("update recipient: %w", err)
	}
	return r, nil
}

func (s queryAdapter) DeleteRecipient(ctx context.Context, id string) error {
	if err := s.q.DeleteRecipient(ctx, id); err != nil {
		return fmt.Errorf("delete recipient: %w", err)
	}
	return nil
}

func (s queryAdapter) GetRecipient(ctx context.Context, id string) (models.Recipient, error) {
	row, err := s.q.GetRecipient(ctx, id)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Recipient{}, err
		}
		return models.Recipient{}, fmt.Errorf("get recipient: %w", err)
	}
	return recipientOf(row), nil
}

func (s queryAdapter) GetRecipientBySubject(ctx context.Context, subject string) (models.Recipient, error) {
	row, err := s.q.GetRecipientBySubject(ctx, subject)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Recipient{}, err
		}
		return models.Recipient{}, fmt.Errorf("get recipient by subject: %w", err)
	}
	return recipientOf(row), nil
}

func recipientOf(row sqlitedb.Recipient) models.Recipient {
	return models.Recipient{
		ID:             row.ID,
		Subject:        row.Subject,
		Name:           row.Name,
		AADObjectID:    row.AadObjectID,
		ConversationID: row.ConversationID,
		ServiceURL:     row.ServiceUrl,
		BotChannelID:   row.BotChannelID,
		TenantID:       row.TenantID,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func (s queryAdapter) ListRoutes(ctx context.Context) ([]models.Route, error) {
	rows, err := s.q.ListRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.Route, 0, len(rows))
	for _, row := range rows {
		out = append(out, routeOf(row))
	}
	return out, nil
}

func (s queryAdapter) CreateRoute(ctx context.Context, r models.Route) (models.Route, error) {
	r.ID = newID(r.ID)
	r.CreatedAt = nowUTC()
	r.UpdatedAt = r.CreatedAt

	err := s.q.CreateRoute(ctx, sqlitedb.CreateRouteParams{
		ID:            r.ID,
		Name:          r.Name,
		ParentID:      r.ParentID,
		Greedy:        r.Greedy,
		LabelSelector: serializeSelector(r.LabelSelector),
		DestinationID: r.DestinationID,
		TemplateID:    r.TemplateID,
		IsDefault:     r.IsDefault,
		Priority:      int64(r.Priority),
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	})
	if err != nil {
		return models.Route{}, fmt.Errorf("create route: %w", err)
	}
	return r, nil
}

func (s queryAdapter) UpdateRoute(ctx context.Context, r models.Route) (models.Route, error) {
	if r.ID == "" {
		return models.Route{}, errors.New("route id is required")
	}
	r.UpdatedAt = nowUTC()

	err := s.q.UpdateRoute(ctx, sqlitedb.UpdateRouteParams{
		Name:          r.Name,
		ParentID:      r.ParentID,
		Greedy:        r.Greedy,
		LabelSelector: serializeSelector(r.LabelSelector),
		DestinationID: r.DestinationID,
		TemplateID:    r.TemplateID,
		IsDefault:     r.IsDefault,
		Priority:      int64(r.Priority),
		UpdatedAt:     r.UpdatedAt,
		ID:            r.ID,
	})
	if err != nil {
		return models.Route{}, fmt.Errorf("update route: %w", err)
	}
	return r, nil
}

func (s queryAdapter) DeleteRoute(ctx context.Context, id string) error {
	if err := s.q.DeleteRoute(ctx, id); err != nil {
		return fmt.Errorf("delete route: %w", err)
	}
	return nil
}

func (s queryAdapter) GetRoute(ctx context.Context, id string) (models.Route, error) {
	row, err := s.q.GetRoute(ctx, id)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Route{}, err
		}
		return models.Route{}, fmt.Errorf("get route: %w", err)
	}
	return routeOf(row), nil
}

func routeOf(row sqlitedb.Route) models.Route {
	return models.Route{
		ID:            row.ID,
		Name:          row.Name,
		ParentID:      row.ParentID,
		Greedy:        row.Greedy,
		LabelSelector: parseSelector(row.LabelSelector),
		DestinationID: row.DestinationID,
		TemplateID:    row.TemplateID,
		IsDefault:     row.IsDefault,
		Priority:      int(row.Priority),
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

// ClaimActiveAlert reaps an abandoned claim and then takes the row, both in
// one transaction. Two statements rather than one upsert with a WHERE, because
// the two need different timestamps -- the cutoff and this claim's own -- and
// sqlc folds two parameters that infer the same column name into one. Reading
// the row afterwards is what turns "the insert did nothing" into a reason.
func (s queryAdapter) ClaimActiveAlert(ctx context.Context, claim models.AlertClaim) (models.ActiveAlert, ClaimOutcome, error) {
	reaped, err := s.q.ReapStaleClaim(ctx, sqlitedb.ReapStaleClaimParams{
		Fingerprint: claim.Fingerprint,
		TeamID:      claim.TeamID,
		ChannelID:   claim.ChannelID,
		ClaimedAt:   sql.NullTime{Time: claim.StaleBefore, Valid: true},
	})
	if err != nil {
		return models.ActiveAlert{}, ClaimHeld, fmt.Errorf("reap stale claim: %w", err)
	}

	row, err := s.q.ClaimActiveAlert(ctx, sqlitedb.ClaimActiveAlertParams{
		Fingerprint: claim.Fingerprint,
		TeamID:      claim.TeamID,
		ChannelID:   claim.ChannelID,
		Status:      claim.Status,
		ClaimOwner:  claim.Owner,
		ClaimedAt:   sql.NullTime{Time: claim.At, Valid: true},
		LastUpdate:  claim.At,
	})
	switch {
	case err == nil:
		if reaped > 0 {
			return activeAlertOf(row), ClaimRecovered, nil
		}
		return activeAlertOf(row), ClaimAcquired, nil
	case !errors.Is(notFound(err), ErrNotFound):
		return models.ActiveAlert{}, ClaimHeld, fmt.Errorf("claim active alert: %w", err)
	}

	// The insert conflicted, so somebody was there first. Which of the two
	// answers it is depends on whether they got as far as posting.
	existing, err := s.GetActiveAlert(ctx, claim.Fingerprint, claim.TeamID, claim.ChannelID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Gone between the two statements: a resolve deleted it. Treat it
			// as held rather than looping, and let the sender retry.
			return models.ActiveAlert{}, ClaimHeld, nil
		}
		return models.ActiveAlert{}, ClaimHeld, err
	}
	if existing.Posted() {
		return existing, ClaimPosted, nil
	}
	return existing, ClaimHeld, nil
}

func (s queryAdapter) CompleteActiveAlertClaim(ctx context.Context, claim models.AlertClaim, messageID string, at time.Time) error {
	_, err := s.q.CompleteActiveAlertClaim(ctx, sqlitedb.CompleteActiveAlertClaimParams{
		Fingerprint: claim.Fingerprint,
		TeamID:      claim.TeamID,
		ChannelID:   claim.ChannelID,
		Status:      claim.Status,
		MessageID:   messageID,
		ClaimOwner:  claim.Owner,
		ClaimedAt:   sql.NullTime{Time: claim.At, Valid: true},
		PostedAt:    sql.NullTime{Time: at, Valid: true},
		LastUpdate:  at,
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(notFound(err), ErrNotFound):
		// No row was written, so the row under this key is somebody else's
		// card. The message just posted has nothing pointing at it.
		return ErrClaimLost
	default:
		return fmt.Errorf("complete active alert claim: %w", err)
	}
}

func (s queryAdapter) ReleaseActiveAlertClaim(ctx context.Context, claim models.AlertClaim) error {
	err := s.q.ReleaseActiveAlertClaim(ctx, sqlitedb.ReleaseActiveAlertClaimParams{
		Fingerprint: claim.Fingerprint,
		TeamID:      claim.TeamID,
		ChannelID:   claim.ChannelID,
		ClaimOwner:  claim.Owner,
	})
	if err != nil {
		return fmt.Errorf("release active alert claim: %w", err)
	}
	return nil
}

func (s queryAdapter) TouchActiveAlert(ctx context.Context, card models.ActiveAlert, status string, at time.Time) error {
	err := s.q.TouchActiveAlert(ctx, sqlitedb.TouchActiveAlertParams{
		Status:      status,
		LastUpdate:  at,
		Fingerprint: card.Fingerprint,
		TeamID:      card.TeamID,
		ChannelID:   card.ChannelID,
		MessageID:   card.MessageID,
	})
	if err != nil {
		return fmt.Errorf("touch active alert: %w", err)
	}
	return nil
}

func (s queryAdapter) ListActiveAlerts(ctx context.Context, fingerprint string) ([]models.ActiveAlert, error) {
	rows, err := s.q.ListActiveAlerts(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("list active alerts: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.ActiveAlert, 0, len(rows))
	for _, row := range rows {
		out = append(out, activeAlertOf(row))
	}
	return out, nil
}

func (s queryAdapter) CountActiveAlerts(ctx context.Context) (int64, error) {
	count, err := s.q.CountActiveAlerts(ctx)
	if err != nil {
		return 0, fmt.Errorf("count active alerts: %w", err)
	}
	return count, nil
}

func (s queryAdapter) GetActiveAlert(ctx context.Context, fingerprint, teamID, channelID string) (models.ActiveAlert, error) {
	row, err := s.q.GetActiveAlert(ctx, sqlitedb.GetActiveAlertParams{
		Fingerprint: fingerprint,
		TeamID:      teamID,
		ChannelID:   channelID,
	})
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.ActiveAlert{}, err
		}
		return models.ActiveAlert{}, fmt.Errorf("get active alert: %w", err)
	}
	return activeAlertOf(row), nil
}

func (s queryAdapter) DeleteActiveAlertCard(ctx context.Context, fingerprint, teamID, channelID, messageID string) error {
	err := s.q.DeleteActiveAlertCard(ctx, sqlitedb.DeleteActiveAlertCardParams{
		Fingerprint: fingerprint,
		TeamID:      teamID,
		ChannelID:   channelID,
		MessageID:   messageID,
	})
	if err != nil {
		return fmt.Errorf("delete active alert card: %w", err)
	}
	return nil
}

func activeAlertOf(row sqlitedb.ActiveAlert) models.ActiveAlert {
	return models.ActiveAlert{
		Fingerprint: row.Fingerprint,
		Status:      row.Status,
		TeamID:      row.TeamID,
		ChannelID:   row.ChannelID,
		MessageID:   row.MessageID,
		ClaimOwner:  row.ClaimOwner,
		ClaimedAt:   row.ClaimedAt.Time,
		PostedAt:    row.PostedAt.Time,
		LastUpdate:  row.LastUpdate,
	}
}

func (s queryAdapter) ListGrants(ctx context.Context) ([]models.Grant, error) {
	rows, err := s.q.ListGrants(ctx)
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.Grant, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.Grant{
			ID:        row.ID,
			Role:      row.Role,
			TeamID:    row.TeamID,
			ChannelID: row.ChannelID,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		})
	}
	return out, nil
}

func (s queryAdapter) CreateGrant(ctx context.Context, g models.Grant) (models.Grant, error) {
	g.ID = newID(g.ID)
	g.CreatedAt = nowUTC()
	g.UpdatedAt = g.CreatedAt

	err := s.q.CreateGrant(ctx, sqlitedb.CreateGrantParams{
		ID:        g.ID,
		Role:      g.Role,
		TeamID:    g.TeamID,
		ChannelID: g.ChannelID,
		CreatedAt: g.CreatedAt,
		UpdatedAt: g.UpdatedAt,
	})
	if err != nil {
		return models.Grant{}, fmt.Errorf("create grant: %w", err)
	}
	return g, nil
}

func (s queryAdapter) DeleteGrantsForRole(ctx context.Context, role string) error {
	if err := s.q.DeleteGrantsForRole(ctx, role); err != nil {
		return fmt.Errorf("delete grants for role: %w", err)
	}
	return nil
}

func (s queryAdapter) DeleteGrant(ctx context.Context, id string) error {
	if err := s.q.DeleteGrant(ctx, id); err != nil {
		return fmt.Errorf("delete grant: %w", err)
	}
	return nil
}

func (s queryAdapter) CreateSession(ctx context.Context, session models.Session) error {
	err := s.q.CreateSession(ctx, sqlitedb.CreateSessionParams{
		ID:        session.ID,
		Subject:   session.Subject,
		Name:      session.Name,
		Source:    session.Source,
		Role:      session.Roles,
		CreatedAt: session.CreatedAt,
		ExpiresAt: session.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// An expired session is reported as missing, so a caller cannot accidentally
// honour one by forgetting to check the time.
func (s queryAdapter) GetSession(ctx context.Context, id string) (models.Session, error) {
	row, err := s.q.GetSession(ctx, id)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Session{}, err
		}
		return models.Session{}, fmt.Errorf("get session: %w", err)
	}
	return liveSession(models.Session{
		ID:        row.ID,
		Subject:   row.Subject,
		Name:      row.Name,
		Source:    row.Source,
		Roles:     row.Role,
		CreatedAt: row.CreatedAt,
		ExpiresAt: row.ExpiresAt,
	})
}

func (s queryAdapter) DeleteSession(ctx context.Context, id string) error {
	if err := s.q.DeleteSession(ctx, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s queryAdapter) DeleteExpiredSessions(ctx context.Context) error {
	now := time.Now()
	if err := s.q.DeleteExpiredSessions(ctx, now); err != nil {
		return fmt.Errorf("sweep sessions: %w", err)
	}
	if err := s.q.DeleteExpiredLoginFlows(ctx, now); err != nil {
		return fmt.Errorf("sweep login flows: %w", err)
	}
	if err := s.q.DeleteExpiredLinkFlows(ctx, now); err != nil {
		return fmt.Errorf("sweep link flows: %w", err)
	}
	return nil
}

func (s queryAdapter) CreateLoginFlow(ctx context.Context, flow models.LoginFlow) error {
	err := s.q.CreateLoginFlow(ctx, sqlitedb.CreateLoginFlowParams{
		State:     flow.State,
		Verifier:  flow.Verifier,
		Nonce:     flow.Nonce,
		ExpiresAt: flow.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("create login flow: %w", err)
	}
	return nil
}

// Taking the flow deletes it: a state may be redeemed once, so a replayed
// callback finds nothing.
func (s queryAdapter) TakeLoginFlow(ctx context.Context, state string) (models.LoginFlow, error) {
	row, err := s.q.TakeLoginFlow(ctx, state)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.LoginFlow{}, err
		}
		return models.LoginFlow{}, fmt.Errorf("take login flow: %w", err)
	}
	return liveFlow(models.LoginFlow{
		State:     row.State,
		Verifier:  row.Verifier,
		Nonce:     row.Nonce,
		ExpiresAt: row.ExpiresAt,
	})
}

// CreateLinkFlow reports ErrConflict on a colliding code rather than a raw
// constraint violation, so a caller minting one can retry with a new code
// instead of treating a one-in-however-many collision as a server error.
func (s queryAdapter) CreateLinkFlow(ctx context.Context, flow models.LinkFlow) error {
	err := s.q.CreateLinkFlow(ctx, sqlitedb.CreateLinkFlowParams{
		Code:      flow.Code,
		Subject:   flow.Subject,
		ExpiresAt: flow.ExpiresAt,
	})
	if err != nil {
		if isPrimaryKeyConflict(err) {
			return ErrConflict
		}
		return fmt.Errorf("create link flow: %w", err)
	}
	return nil
}

func (s queryAdapter) DeleteLinkFlowsForSubject(ctx context.Context, subject string) error {
	if err := s.q.DeleteLinkFlowsForSubject(ctx, subject); err != nil {
		return fmt.Errorf("delete link flows for subject: %w", err)
	}
	return nil
}

// Taking the code spends it, in the same order TakeLoginFlow does: the row is
// deleted first and judged afterwards, so an expired code cannot be retried
// until it is guessed right.
func (s queryAdapter) TakeLinkFlow(ctx context.Context, code string) (models.LinkFlow, error) {
	row, err := s.q.TakeLinkFlow(ctx, code)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.LinkFlow{}, err
		}
		return models.LinkFlow{}, fmt.Errorf("take link flow: %w", err)
	}
	return liveLinkFlow(models.LinkFlow{
		Code:      row.Code,
		Subject:   row.Subject,
		ExpiresAt: row.ExpiresAt,
	})
}

package store

import (
	"context"
	"time"

	"github.com/pflege-de-labs/teamster/internal/store/pgdb"
	"github.com/pflege-de-labs/teamster/internal/store/sqlitedb"
)

// pgQueries presents pgdb's queries as the dialectQueries the adapter speaks.
//
// sqlc emits one package per dialect and Go has no structural satisfaction
// across packages, so the two Querier interfaces can never be one type even
// though they name the same statements. The parameter and row structs are
// generated from the same queries and are field for field identical, which
// makes them convertible -- so this shim is the whole cost of the second
// backend, and the compiler rejects it the moment the two shapes stop
// matching.
//
// It is written by hand, in the order sqlitedb.Querier declares. A statement
// added to the queries needs a method here too; the compiler says so.
type pgQueries struct {
	q *pgdb.Queries
}

func (p pgQueries) ClearDefaultDestination(ctx context.Context) error {
	return p.q.ClearDefaultDestination(ctx)
}

func (p pgQueries) ClaimActiveAlert(ctx context.Context, arg sqlitedb.ClaimActiveAlertParams) (sqlitedb.ActiveAlert, error) {
	row, err := p.q.ClaimActiveAlert(ctx, pgdb.ClaimActiveAlertParams(arg))
	return sqlitedb.ActiveAlert(row), err
}

func (p pgQueries) ClaimActiveAlertRecipient(ctx context.Context, arg sqlitedb.ClaimActiveAlertRecipientParams) (sqlitedb.ActiveAlertRecipient, error) {
	row, err := p.q.ClaimActiveAlertRecipient(ctx, pgdb.ClaimActiveAlertRecipientParams(arg))
	return sqlitedb.ActiveAlertRecipient(row), err
}

func (p pgQueries) CompleteActiveAlertClaim(ctx context.Context, arg sqlitedb.CompleteActiveAlertClaimParams) (string, error) {
	return p.q.CompleteActiveAlertClaim(ctx, pgdb.CompleteActiveAlertClaimParams(arg))
}

func (p pgQueries) CompleteActiveAlertRecipientClaim(ctx context.Context, arg sqlitedb.CompleteActiveAlertRecipientClaimParams) (string, error) {
	return p.q.CompleteActiveAlertRecipientClaim(ctx, pgdb.CompleteActiveAlertRecipientClaimParams(arg))
}

func (p pgQueries) CountActiveAlerts(ctx context.Context) (int64, error) {
	return p.q.CountActiveAlerts(ctx)
}

func (p pgQueries) CountDestinations(ctx context.Context) (int64, error) {
	return p.q.CountDestinations(ctx)
}

func (p pgQueries) CreateBrokerToken(ctx context.Context, arg sqlitedb.CreateBrokerTokenParams) error {
	return p.q.CreateBrokerToken(ctx, pgdb.CreateBrokerTokenParams(arg))
}

func (p pgQueries) CreateDestination(ctx context.Context, arg sqlitedb.CreateDestinationParams) error {
	return p.q.CreateDestination(ctx, pgdb.CreateDestinationParams(arg))
}

func (p pgQueries) CreateGrant(ctx context.Context, arg sqlitedb.CreateGrantParams) error {
	return p.q.CreateGrant(ctx, pgdb.CreateGrantParams(arg))
}

func (p pgQueries) CreateLinkFlow(ctx context.Context, arg sqlitedb.CreateLinkFlowParams) error {
	return p.q.CreateLinkFlow(ctx, pgdb.CreateLinkFlowParams(arg))
}

func (p pgQueries) CreateLoginFlow(ctx context.Context, arg sqlitedb.CreateLoginFlowParams) error {
	return p.q.CreateLoginFlow(ctx, pgdb.CreateLoginFlowParams(arg))
}

func (p pgQueries) CreateRecipient(ctx context.Context, arg sqlitedb.CreateRecipientParams) error {
	return p.q.CreateRecipient(ctx, pgdb.CreateRecipientParams(arg))
}

func (p pgQueries) CreateRoute(ctx context.Context, arg sqlitedb.CreateRouteParams) error {
	return p.q.CreateRoute(ctx, pgdb.CreateRouteParams(arg))
}

func (p pgQueries) CreateSession(ctx context.Context, arg sqlitedb.CreateSessionParams) error {
	return p.q.CreateSession(ctx, pgdb.CreateSessionParams(arg))
}

func (p pgQueries) CreateTemplate(ctx context.Context, arg sqlitedb.CreateTemplateParams) error {
	return p.q.CreateTemplate(ctx, pgdb.CreateTemplateParams(arg))
}

func (p pgQueries) CreateWebhookEndpoint(ctx context.Context, arg sqlitedb.CreateWebhookEndpointParams) error {
	return p.q.CreateWebhookEndpoint(ctx, pgdb.CreateWebhookEndpointParams(arg))
}

func (p pgQueries) DeleteActiveAlertCard(ctx context.Context, arg sqlitedb.DeleteActiveAlertCardParams) error {
	return p.q.DeleteActiveAlertCard(ctx, pgdb.DeleteActiveAlertCardParams(arg))
}

func (p pgQueries) DeleteActiveAlertRecipientCard(ctx context.Context, arg sqlitedb.DeleteActiveAlertRecipientCardParams) error {
	return p.q.DeleteActiveAlertRecipientCard(ctx, pgdb.DeleteActiveAlertRecipientCardParams(arg))
}

func (p pgQueries) DeleteActiveAlertRecipientsFor(ctx context.Context, recipientID string) error {
	return p.q.DeleteActiveAlertRecipientsFor(ctx, recipientID)
}

func (p pgQueries) DeleteBrokerToken(ctx context.Context, sessionID string) error {
	return p.q.DeleteBrokerToken(ctx, sessionID)
}

func (p pgQueries) DeleteDestination(ctx context.Context, id string) error {
	return p.q.DeleteDestination(ctx, id)
}

func (p pgQueries) DeleteExpiredLinkFlows(ctx context.Context, expiresAt time.Time) error {
	return p.q.DeleteExpiredLinkFlows(ctx, expiresAt)
}

func (p pgQueries) DeleteExpiredLoginFlows(ctx context.Context, expiresAt time.Time) error {
	return p.q.DeleteExpiredLoginFlows(ctx, expiresAt)
}

func (p pgQueries) DeleteExpiredSessions(ctx context.Context, expiresAt time.Time) error {
	return p.q.DeleteExpiredSessions(ctx, expiresAt)
}

func (p pgQueries) DeleteGrant(ctx context.Context, id string) error {
	return p.q.DeleteGrant(ctx, id)
}

func (p pgQueries) DeleteGrantsForRole(ctx context.Context, role string) error {
	return p.q.DeleteGrantsForRole(ctx, role)
}

func (p pgQueries) DeleteLinkFlowsForSubject(ctx context.Context, subject string) error {
	return p.q.DeleteLinkFlowsForSubject(ctx, subject)
}

func (p pgQueries) DeleteRecipient(ctx context.Context, id string) error {
	return p.q.DeleteRecipient(ctx, id)
}

func (p pgQueries) DeleteRoute(ctx context.Context, id string) error {
	return p.q.DeleteRoute(ctx, id)
}

func (p pgQueries) DeleteSession(ctx context.Context, id string) error {
	return p.q.DeleteSession(ctx, id)
}

func (p pgQueries) DeleteTemplate(ctx context.Context, id string) error {
	return p.q.DeleteTemplate(ctx, id)
}

func (p pgQueries) DeleteWebhookEndpoint(ctx context.Context, id string) error {
	return p.q.DeleteWebhookEndpoint(ctx, id)
}

func (p pgQueries) GetActiveAlert(ctx context.Context, arg sqlitedb.GetActiveAlertParams) (sqlitedb.ActiveAlert, error) {
	row, err := p.q.GetActiveAlert(ctx, pgdb.GetActiveAlertParams(arg))
	return sqlitedb.ActiveAlert(row), err
}

func (p pgQueries) GetActiveAlertRecipient(ctx context.Context, arg sqlitedb.GetActiveAlertRecipientParams) (sqlitedb.ActiveAlertRecipient, error) {
	row, err := p.q.GetActiveAlertRecipient(ctx, pgdb.GetActiveAlertRecipientParams(arg))
	return sqlitedb.ActiveAlertRecipient(row), err
}

func (p pgQueries) GetBrokerToken(ctx context.Context, sessionID string) (sqlitedb.BrokerToken, error) {
	row, err := p.q.GetBrokerToken(ctx, sessionID)
	return sqlitedb.BrokerToken(row), err
}

func (p pgQueries) GetDefaultDestination(ctx context.Context) (sqlitedb.Destination, error) {
	row, err := p.q.GetDefaultDestination(ctx)
	return sqlitedb.Destination(row), err
}

func (p pgQueries) GetDestination(ctx context.Context, id string) (sqlitedb.Destination, error) {
	row, err := p.q.GetDestination(ctx, id)
	return sqlitedb.Destination(row), err
}

func (p pgQueries) GetRecipient(ctx context.Context, id string) (sqlitedb.Recipient, error) {
	row, err := p.q.GetRecipient(ctx, id)
	return sqlitedb.Recipient(row), err
}

func (p pgQueries) GetRecipientByConversation(ctx context.Context, conversationID string) (sqlitedb.Recipient, error) {
	row, err := p.q.GetRecipientByConversation(ctx, conversationID)
	return sqlitedb.Recipient(row), err
}

func (p pgQueries) GetRecipientBySubject(ctx context.Context, subject string) (sqlitedb.Recipient, error) {
	row, err := p.q.GetRecipientBySubject(ctx, subject)
	return sqlitedb.Recipient(row), err
}

func (p pgQueries) GetRoute(ctx context.Context, id string) (sqlitedb.Route, error) {
	row, err := p.q.GetRoute(ctx, id)
	return sqlitedb.Route(row), err
}

func (p pgQueries) GetSession(ctx context.Context, id string) (sqlitedb.Session, error) {
	row, err := p.q.GetSession(ctx, id)
	return sqlitedb.Session(row), err
}

func (p pgQueries) GetTemplate(ctx context.Context, id string) (sqlitedb.Template, error) {
	row, err := p.q.GetTemplate(ctx, id)
	return sqlitedb.Template(row), err
}

func (p pgQueries) GetWebhookEndpoint(ctx context.Context, id string) (sqlitedb.WebhookEndpoint, error) {
	row, err := p.q.GetWebhookEndpoint(ctx, id)
	return sqlitedb.WebhookEndpoint(row), err
}

func (p pgQueries) GetWebhookEndpointBySlug(ctx context.Context, arg sqlitedb.GetWebhookEndpointBySlugParams) (sqlitedb.WebhookEndpoint, error) {
	row, err := p.q.GetWebhookEndpointBySlug(ctx, pgdb.GetWebhookEndpointBySlugParams(arg))
	return sqlitedb.WebhookEndpoint(row), err
}

func (p pgQueries) ListActiveAlerts(ctx context.Context, fingerprint string) ([]sqlitedb.ActiveAlert, error) {
	rows, err := p.q.ListActiveAlerts(ctx, fingerprint)
	if err != nil {
		return nil, err
	}
	out := make([]sqlitedb.ActiveAlert, 0, len(rows))
	for _, row := range rows {
		out = append(out, sqlitedb.ActiveAlert(row))
	}
	return out, nil
}

func (p pgQueries) ListActiveAlertRecipients(ctx context.Context, fingerprint string) ([]sqlitedb.ActiveAlertRecipient, error) {
	rows, err := p.q.ListActiveAlertRecipients(ctx, fingerprint)
	if err != nil {
		return nil, err
	}
	out := make([]sqlitedb.ActiveAlertRecipient, 0, len(rows))
	for _, row := range rows {
		out = append(out, sqlitedb.ActiveAlertRecipient(row))
	}
	return out, nil
}

func (p pgQueries) ListDestinations(ctx context.Context) ([]sqlitedb.Destination, error) {
	rows, err := p.q.ListDestinations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sqlitedb.Destination, 0, len(rows))
	for _, row := range rows {
		out = append(out, sqlitedb.Destination(row))
	}
	return out, nil
}

func (p pgQueries) ListGrants(ctx context.Context) ([]sqlitedb.Grant, error) {
	rows, err := p.q.ListGrants(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sqlitedb.Grant, 0, len(rows))
	for _, row := range rows {
		out = append(out, sqlitedb.Grant(row))
	}
	return out, nil
}

func (p pgQueries) ListRecipients(ctx context.Context) ([]sqlitedb.Recipient, error) {
	rows, err := p.q.ListRecipients(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sqlitedb.Recipient, 0, len(rows))
	for _, row := range rows {
		out = append(out, sqlitedb.Recipient(row))
	}
	return out, nil
}

func (p pgQueries) ListRoutes(ctx context.Context) ([]sqlitedb.Route, error) {
	rows, err := p.q.ListRoutes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sqlitedb.Route, 0, len(rows))
	for _, row := range rows {
		out = append(out, sqlitedb.Route(row))
	}
	return out, nil
}

func (p pgQueries) ListTemplates(ctx context.Context) ([]sqlitedb.Template, error) {
	rows, err := p.q.ListTemplates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sqlitedb.Template, 0, len(rows))
	for _, row := range rows {
		out = append(out, sqlitedb.Template(row))
	}
	return out, nil
}

func (p pgQueries) ListWebhookEndpoints(ctx context.Context) ([]sqlitedb.WebhookEndpoint, error) {
	rows, err := p.q.ListWebhookEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sqlitedb.WebhookEndpoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, sqlitedb.WebhookEndpoint(row))
	}
	return out, nil
}

func (p pgQueries) MarkDefaultDestination(ctx context.Context, id string) (int64, error) {
	return p.q.MarkDefaultDestination(ctx, id)
}

func (p pgQueries) ReapStaleClaim(ctx context.Context, arg sqlitedb.ReapStaleClaimParams) (int64, error) {
	return p.q.ReapStaleClaim(ctx, pgdb.ReapStaleClaimParams(arg))
}

func (p pgQueries) ReapStaleClaimRecipient(ctx context.Context, arg sqlitedb.ReapStaleClaimRecipientParams) (int64, error) {
	return p.q.ReapStaleClaimRecipient(ctx, pgdb.ReapStaleClaimRecipientParams(arg))
}

func (p pgQueries) ReleaseActiveAlertClaim(ctx context.Context, arg sqlitedb.ReleaseActiveAlertClaimParams) error {
	return p.q.ReleaseActiveAlertClaim(ctx, pgdb.ReleaseActiveAlertClaimParams(arg))
}

func (p pgQueries) ReleaseActiveAlertRecipientClaim(ctx context.Context, arg sqlitedb.ReleaseActiveAlertRecipientClaimParams) error {
	return p.q.ReleaseActiveAlertRecipientClaim(ctx, pgdb.ReleaseActiveAlertRecipientClaimParams(arg))
}

func (p pgQueries) RotateWebhookEndpointToken(ctx context.Context, arg sqlitedb.RotateWebhookEndpointTokenParams) error {
	return p.q.RotateWebhookEndpointToken(ctx, pgdb.RotateWebhookEndpointTokenParams(arg))
}

func (p pgQueries) TakeLinkFlow(ctx context.Context, code string) (sqlitedb.LinkFlow, error) {
	row, err := p.q.TakeLinkFlow(ctx, code)
	return sqlitedb.LinkFlow(row), err
}

func (p pgQueries) TakeLoginFlow(ctx context.Context, state string) (sqlitedb.LoginFlow, error) {
	row, err := p.q.TakeLoginFlow(ctx, state)
	return sqlitedb.LoginFlow(row), err
}

func (p pgQueries) TouchActiveAlert(ctx context.Context, arg sqlitedb.TouchActiveAlertParams) error {
	return p.q.TouchActiveAlert(ctx, pgdb.TouchActiveAlertParams(arg))
}

func (p pgQueries) TouchActiveAlertRecipient(ctx context.Context, arg sqlitedb.TouchActiveAlertRecipientParams) error {
	return p.q.TouchActiveAlertRecipient(ctx, pgdb.TouchActiveAlertRecipientParams(arg))
}

func (p pgQueries) UpdateBrokerToken(ctx context.Context, arg sqlitedb.UpdateBrokerTokenParams) error {
	return p.q.UpdateBrokerToken(ctx, pgdb.UpdateBrokerTokenParams(arg))
}

func (p pgQueries) UpdateDestination(ctx context.Context, arg sqlitedb.UpdateDestinationParams) error {
	return p.q.UpdateDestination(ctx, pgdb.UpdateDestinationParams(arg))
}

func (p pgQueries) UpdateRecipient(ctx context.Context, arg sqlitedb.UpdateRecipientParams) error {
	return p.q.UpdateRecipient(ctx, pgdb.UpdateRecipientParams(arg))
}

func (p pgQueries) UpdateRoute(ctx context.Context, arg sqlitedb.UpdateRouteParams) error {
	return p.q.UpdateRoute(ctx, pgdb.UpdateRouteParams(arg))
}

func (p pgQueries) UpdateTemplate(ctx context.Context, arg sqlitedb.UpdateTemplateParams) error {
	return p.q.UpdateTemplate(ctx, pgdb.UpdateTemplateParams(arg))
}

func (p pgQueries) UpdateWebhookEndpoint(ctx context.Context, arg sqlitedb.UpdateWebhookEndpointParams) error {
	return p.q.UpdateWebhookEndpoint(ctx, pgdb.UpdateWebhookEndpointParams(arg))
}

func (p pgQueries) ClearRecipientBlocked(ctx context.Context, id string) error {
	return p.q.ClearRecipientBlocked(ctx, id)
}

func (p pgQueries) MarkRecipientBlocked(ctx context.Context, arg sqlitedb.MarkRecipientBlockedParams) error {
	return p.q.MarkRecipientBlocked(ctx, pgdb.MarkRecipientBlockedParams(arg))
}

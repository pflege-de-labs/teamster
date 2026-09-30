package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

	sources, err := models.NormalizeSources(t.Sources)
	if err != nil {
		return models.Template{}, err
	}
	t.Sources = sources
	err = s.q.CreateTemplate(ctx, sqlitedb.CreateTemplateParams{
		ID:          t.ID,
		Name:        t.Name,
		Title:       t.Title,
		MessageText: t.Text,
		Body:        t.Body,
		Sources:     strings.Join(t.Sources, ","),
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

	sources, err := models.NormalizeSources(t.Sources)
	if err != nil {
		return models.Template{}, err
	}
	t.Sources = sources
	err = s.q.UpdateTemplate(ctx, sqlitedb.UpdateTemplateParams{
		Name:        t.Name,
		Title:       t.Title,
		MessageText: t.Text,
		Body:        t.Body,
		Sources:     strings.Join(t.Sources, ","),
		UpdatedAt:   t.UpdatedAt,
		ID:          t.ID,
	})
	if err != nil {
		return models.Template{}, fmt.Errorf("update template: %w", err)
	}
	return t, nil
}

func (s queryAdapter) DeleteTemplate(ctx context.Context, id string) error {
	// A catch-all left naming a deleted template would fail every message it
	// catches, so it falls back to the built-in one instead.
	err := s.q.ClearSettingValue(ctx, sqlitedb.ClearSettingValueParams{Key: settingGlobalDefaultTemplate, Value: id})
	if err != nil {
		return fmt.Errorf("forget global default template: %w", err)
	}
	for _, source := range models.AllSources() {
		err := s.q.ClearSettingValue(ctx, sqlitedb.ClearSettingValueParams{Key: sourceDefaultKey(source), Value: id})
		if err != nil {
			return fmt.Errorf("forget %s default template: %w", source, err)
		}
	}
	if err := s.q.DeleteTemplate(ctx, id); err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}

// settingGlobalDefaultTemplate is the settings key for the catch-all route's
// template (ADR 0050).
const settingGlobalDefaultTemplate = "global_default.template_id"

func (s queryAdapter) GetGlobalDefaultTemplate(ctx context.Context) (string, error) {
	value, err := s.q.GetSetting(ctx, settingGlobalDefaultTemplate)
	if err != nil {
		if errors.Is(notFound(err), ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("get global default template: %w", err)
	}
	return value, nil
}

func (s queryAdapter) SetGlobalDefaultTemplate(ctx context.Context, templateID string) error {
	if templateID == "" {
		if err := s.q.DeleteSetting(ctx, settingGlobalDefaultTemplate); err != nil {
			return fmt.Errorf("clear global default template: %w", err)
		}
		return nil
	}
	if _, err := s.GetTemplate(ctx, templateID); err != nil {
		return err
	}
	err := s.q.UpsertSetting(ctx, sqlitedb.UpsertSettingParams{
		Key: settingGlobalDefaultTemplate, Value: templateID, UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("set global default template: %w", err)
	}
	return nil
}

// settingPresetsSeeded records that SeedTemplates ran, whatever became of
// the templates it created since.
const settingPresetsSeeded = "presets.seeded"

// sourceDefaultKey is the settings key for a source's default template.
func sourceDefaultKey(source string) string {
	return "default_template." + source
}

func knownSource(source string) error {
	if _, err := models.NormalizeSources([]string{source}); err != nil {
		return err
	}
	return nil
}

func (s queryAdapter) GetSourceDefaultTemplate(ctx context.Context, source string) (string, error) {
	if err := knownSource(source); err != nil {
		return "", err
	}
	value, err := s.q.GetSetting(ctx, sourceDefaultKey(source))
	if err != nil {
		if errors.Is(notFound(err), ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("get %s default template: %w", source, err)
	}
	return value, nil
}

func (s queryAdapter) SourceDefaultTemplates(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	for _, source := range models.AllSources() {
		id, err := s.GetSourceDefaultTemplate(ctx, source)
		if err != nil {
			return nil, err
		}
		if id != "" {
			out[source] = id
		}
	}
	return out, nil
}

func (s queryAdapter) SetSourceDefaultTemplate(ctx context.Context, source, templateID string) error {
	if err := knownSource(source); err != nil {
		return err
	}
	if templateID == "" {
		if err := s.q.DeleteSetting(ctx, sourceDefaultKey(source)); err != nil {
			return fmt.Errorf("clear %s default template: %w", source, err)
		}
		return nil
	}
	t, err := s.GetTemplate(ctx, templateID)
	if err != nil {
		return err
	}
	if !t.Handles(source) {
		return fmt.Errorf("%w: %s", ErrTemplateSource, source)
	}
	err = s.q.UpsertSetting(ctx, sqlitedb.UpsertSettingParams{
		Key: sourceDefaultKey(source), Value: templateID, UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("set %s default template: %w", source, err)
	}
	return nil
}

func (s queryAdapter) SeedTemplates(ctx context.Context, templates []models.Template) (bool, error) {
	claimed, err := s.q.InsertSettingIfAbsent(ctx, sqlitedb.InsertSettingIfAbsentParams{
		Key: settingPresetsSeeded, Value: "1", UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		return false, fmt.Errorf("claim template seeding: %w", err)
	}
	if claimed == 0 {
		return false, nil
	}
	for _, t := range templates {
		created, err := s.CreateTemplate(ctx, t)
		if err != nil {
			return false, fmt.Errorf("seed template %q: %w", t.Name, err)
		}
		if len(created.Sources) != 1 {
			continue
		}
		current, err := s.GetSourceDefaultTemplate(ctx, created.Sources[0])
		if err != nil {
			return false, err
		}
		if current != "" {
			continue
		}
		if err := s.SetSourceDefaultTemplate(ctx, created.Sources[0], created.ID); err != nil {
			return false, err
		}
	}
	return true, nil
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
		Sources:   splitSources(row.Sources),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

// splitSources reads the stored list back; empty is any source (ADR 0053).
func splitSources(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, ",")
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
	// The statement decided IsDefault; read back what it chose.
	return s.GetDestination(ctx, d.ID)
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
	// An update never moves the default, whatever d.IsDefault said.
	d.IsDefault = false
	if current, err := s.GetDestination(ctx, d.ID); err == nil {
		d.IsDefault = current.IsDefault
	}
	return d, nil
}

func (s queryAdapter) DeleteDestination(ctx context.Context, id string) error {
	d, err := s.GetDestination(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if d.IsDefault {
		n, err := s.q.CountDestinations(ctx)
		if err != nil {
			return fmt.Errorf("count destinations: %w", err)
		}
		if n > 1 {
			return ErrDefaultDestination
		}
	}
	if err := s.q.DeleteDestination(ctx, id); err != nil {
		return fmt.Errorf("delete destination: %w", err)
	}
	return nil
}

func (s queryAdapter) GetDefaultDestination(ctx context.Context) (models.Destination, error) {
	row, err := s.q.GetDefaultDestination(ctx)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Destination{}, err
		}
		return models.Destination{}, fmt.Errorf("get default destination: %w", err)
	}
	return destinationOf(row), nil
}

// SetDefaultDestination clears before it marks: the unique index is checked
// row by row, so a single UPDATE could trip over the old default.
func (s queryAdapter) SetDefaultDestination(ctx context.Context, id string) error {
	if err := s.q.ClearDefaultDestination(ctx); err != nil {
		return fmt.Errorf("clear default destination: %w", err)
	}
	n, err := s.q.MarkDefaultDestination(ctx, id)
	if err != nil {
		return fmt.Errorf("mark default destination: %w", err)
	}
	if n == 0 {
		return ErrNotFound
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
		IsDefault: row.IsDefault,
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
		BlockedAt:      nullTime(r.BlockedAt),
		BlockedReason:  r.BlockedReason,
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
		BlockedAt:      nullTime(r.BlockedAt),
		BlockedReason:  r.BlockedReason,
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

// deleteRecipientCascade is the store-level DeleteRecipient both SQLiteStore
// and PostgresStore expose, overriding queryAdapter's single-statement one:
// an event claimed or posted to this recipient must not survive the person it
// was claimed for, or a close would fail against a row nothing will ever
// clear again (see internal/httpserver/webhooks.go's handling of a recipient
// GetRecipient can no longer find, which is the defence for a row stranded
// some other way). Running inside one transaction is what makes "unlinked but
// still owes an alert" a state that cannot happen, rather than a race between
// two statements.
func deleteRecipientCascade(ctx context.Context, s interface {
	WithTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error
}, id string) error {
	return s.WithTx(ctx, func(ctx context.Context, tx Store) error {
		if err := tx.DeleteActiveEventRecipientsFor(ctx, id); err != nil {
			return err
		}
		return tx.DeleteRecipient(ctx, id)
	})
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
		BlockedAt:      row.BlockedAt.Time,
		BlockedReason:  row.BlockedReason,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func (s queryAdapter) ListWebhookEndpoints(ctx context.Context) ([]models.WebhookEndpoint, error) {
	rows, err := s.q.ListWebhookEndpoints(ctx)
	if err != nil {
		return nil, fmt.Errorf("list webhook endpoints: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.WebhookEndpoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, webhookEndpointOf(row))
	}
	return out, nil
}

func (s queryAdapter) CreateWebhookEndpoint(ctx context.Context, e models.WebhookEndpoint) (models.WebhookEndpoint, error) {
	e.ID = newID(e.ID)
	e.CreatedAt = nowUTC()
	e.UpdatedAt = e.CreatedAt

	err := s.q.CreateWebhookEndpoint(ctx, sqlitedb.CreateWebhookEndpointParams{
		ID:            e.ID,
		TeamSlug:      e.TeamSlug,
		ChannelSlug:   e.ChannelSlug,
		DestinationID: e.DestinationID,
		TokenHash:     e.TokenHash,
		CreatedAt:     e.CreatedAt,
		UpdatedAt:     e.UpdatedAt,
		TemplateID:    e.TemplateID,
	})
	if err != nil {
		return models.WebhookEndpoint{}, fmt.Errorf("create webhook endpoint: %w", err)
	}
	return e, nil
}

// The token hash is not among the columns written: a sender's secret changes
// only when somebody asks for it to, through RotateWebhookEndpointToken.
func (s queryAdapter) UpdateWebhookEndpoint(ctx context.Context, e models.WebhookEndpoint) (models.WebhookEndpoint, error) {
	if e.ID == "" {
		return models.WebhookEndpoint{}, errors.New("webhook endpoint id is required")
	}
	e.UpdatedAt = nowUTC()

	err := s.q.UpdateWebhookEndpoint(ctx, sqlitedb.UpdateWebhookEndpointParams{
		TeamSlug:      e.TeamSlug,
		ChannelSlug:   e.ChannelSlug,
		DestinationID: e.DestinationID,
		UpdatedAt:     e.UpdatedAt,
		TemplateID:    e.TemplateID,
		ID:            e.ID,
	})
	if err != nil {
		return models.WebhookEndpoint{}, fmt.Errorf("update webhook endpoint: %w", err)
	}
	return e, nil
}

func (s queryAdapter) RotateWebhookEndpointToken(ctx context.Context, id, tokenHash string) error {
	if id == "" {
		return errors.New("webhook endpoint id is required")
	}
	err := s.q.RotateWebhookEndpointToken(ctx, sqlitedb.RotateWebhookEndpointTokenParams{
		TokenHash: tokenHash,
		UpdatedAt: nowUTC(),
		ID:        id,
	})
	if err != nil {
		return fmt.Errorf("rotate webhook endpoint token: %w", err)
	}
	return nil
}

func (s queryAdapter) DeleteWebhookEndpoint(ctx context.Context, id string) error {
	if err := s.q.DeleteWebhookEndpoint(ctx, id); err != nil {
		return fmt.Errorf("delete webhook endpoint: %w", err)
	}
	return nil
}

func (s queryAdapter) GetWebhookEndpoint(ctx context.Context, id string) (models.WebhookEndpoint, error) {
	row, err := s.q.GetWebhookEndpoint(ctx, id)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.WebhookEndpoint{}, err
		}
		return models.WebhookEndpoint{}, fmt.Errorf("get webhook endpoint: %w", err)
	}
	return webhookEndpointOf(row), nil
}

func (s queryAdapter) GetWebhookEndpointBySlug(ctx context.Context, teamSlug, channelSlug string) (models.WebhookEndpoint, error) {
	row, err := s.q.GetWebhookEndpointBySlug(ctx, sqlitedb.GetWebhookEndpointBySlugParams{
		TeamSlug:    teamSlug,
		ChannelSlug: channelSlug,
	})
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.WebhookEndpoint{}, err
		}
		return models.WebhookEndpoint{}, fmt.Errorf("get webhook endpoint by slug: %w", err)
	}
	return webhookEndpointOf(row), nil
}

func webhookEndpointOf(row sqlitedb.WebhookEndpoint) models.WebhookEndpoint {
	return models.WebhookEndpoint{
		ID:            row.ID,
		TeamSlug:      row.TeamSlug,
		ChannelSlug:   row.ChannelSlug,
		DestinationID: row.DestinationID,
		TemplateID:    row.TemplateID,
		TokenHash:     row.TokenHash,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func (s queryAdapter) UpsertBotTeam(ctx context.Context, t models.BotTeam) error {
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = nowUTC()
	}
	err := s.q.UpsertBotTeam(ctx, sqlitedb.UpsertBotTeamParams{
		TeamID:     t.TeamID,
		TenantID:   t.TenantID,
		ServiceUrl: t.ServiceURL,
		UpdatedAt:  t.UpdatedAt.UTC(),
	})
	if err != nil {
		return fmt.Errorf("upsert bot team: %w", err)
	}
	return nil
}

func (s queryAdapter) GetBotTeam(ctx context.Context, teamID string) (models.BotTeam, error) {
	row, err := s.q.GetBotTeam(ctx, teamID)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.BotTeam{}, err
		}
		return models.BotTeam{}, fmt.Errorf("get bot team: %w", err)
	}
	return botTeamOf(row), nil
}

func (s queryAdapter) ListBotTeams(ctx context.Context) ([]models.BotTeam, error) {
	rows, err := s.q.ListBotTeams(ctx)
	if err != nil {
		return nil, fmt.Errorf("list bot teams: %w", err)
	}
	out := make([]models.BotTeam, 0, len(rows))
	for _, row := range rows {
		out = append(out, botTeamOf(row))
	}
	return out, nil
}

func (s queryAdapter) DeleteBotTeam(ctx context.Context, teamID string) error {
	if err := s.q.DeleteBotTeam(ctx, teamID); err != nil {
		return fmt.Errorf("delete bot team: %w", err)
	}
	return nil
}

func (s queryAdapter) CountDestinationsWithoutBotTeam(ctx context.Context) (int64, error) {
	n, err := s.q.CountDestinationsWithoutBotTeam(ctx)
	if err != nil {
		return 0, fmt.Errorf("count destinations without bot team: %w", err)
	}
	return n, nil
}

func botTeamOf(row sqlitedb.BotTeam) models.BotTeam {
	return models.BotTeam{TeamID: row.TeamID, TenantID: row.TenantID, ServiceURL: row.ServiceUrl, UpdatedAt: row.UpdatedAt}
}

func (s queryAdapter) ListAccessTokens(ctx context.Context) ([]models.AccessToken, error) {
	rows, err := s.q.ListAccessTokens(ctx)
	if err != nil {
		return nil, fmt.Errorf("list access tokens: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.AccessToken, 0, len(rows))
	for _, row := range rows {
		out = append(out, accessTokenOf(row))
	}
	return out, nil
}

func (s queryAdapter) CreateAccessToken(ctx context.Context, t models.AccessToken) (models.AccessToken, error) {
	t.ID = newID(t.ID)
	t.CreatedAt = nowUTC()
	t.LastUsedAt = time.Time{}

	err := s.q.CreateAccessToken(ctx, sqlitedb.CreateAccessTokenParams{
		ID:        t.ID,
		Name:      t.Name,
		TokenHash: t.TokenHash,
		CreatedBy: t.CreatedBy,
		CreatedAt: t.CreatedAt,
	})
	if err != nil {
		if isPrimaryKeyConflict(err) {
			return models.AccessToken{}, ErrConflict
		}
		return models.AccessToken{}, fmt.Errorf("create access token: %w", err)
	}
	return t, nil
}

func (s queryAdapter) GetAccessTokenByHash(ctx context.Context, tokenHash string) (models.AccessToken, error) {
	row, err := s.q.GetAccessTokenByHash(ctx, tokenHash)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.AccessToken{}, err
		}
		return models.AccessToken{}, fmt.Errorf("get access token: %w", err)
	}
	return accessTokenOf(row), nil
}

func (s queryAdapter) TouchAccessToken(ctx context.Context, id string, at time.Time) error {
	err := s.q.TouchAccessToken(ctx, sqlitedb.TouchAccessTokenParams{
		LastUsedAt: nullTime(at.UTC()),
		ID:         id,
	})
	if err != nil {
		return fmt.Errorf("touch access token: %w", err)
	}
	return nil
}

func (s queryAdapter) DeleteAccessToken(ctx context.Context, id string) error {
	if err := s.q.DeleteAccessToken(ctx, id); err != nil {
		return fmt.Errorf("delete access token: %w", err)
	}
	return nil
}

func accessTokenOf(row sqlitedb.AccessToken) models.AccessToken {
	return models.AccessToken{
		ID:         row.ID,
		Name:       row.Name,
		TokenHash:  row.TokenHash,
		CreatedBy:  row.CreatedBy,
		CreatedAt:  row.CreatedAt,
		LastUsedAt: row.LastUsedAt.Time,
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
		RecipientID:   r.RecipientID,
		Addressed:     r.Addressed,
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
		RecipientID:   r.RecipientID,
		Addressed:     r.Addressed,
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
		RecipientID:   row.RecipientID,
		Addressed:     row.Addressed,
		TemplateID:    row.TemplateID,
		IsDefault:     row.IsDefault,
		Priority:      int(row.Priority),
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

// ClaimActiveEvent reaps an abandoned claim and then takes the row, both in
// one transaction. Two statements rather than one upsert with a WHERE, because
// the two need different timestamps -- the cutoff and this claim's own -- and
// sqlc folds two parameters that infer the same column name into one. Reading
// the row afterwards is what turns "the insert did nothing" into a reason.
func (s queryAdapter) ClaimActiveEvent(ctx context.Context, claim models.EventClaim) (models.ActiveEvent, ClaimOutcome, error) {
	reaped, err := s.q.ReapStaleClaim(ctx, sqlitedb.ReapStaleClaimParams{
		EventKey:  claim.Key,
		TeamID:    claim.TeamID,
		ChannelID: claim.ChannelID,
		ClaimedAt: sql.NullTime{Time: claim.StaleBefore, Valid: true},
	})
	if err != nil {
		return models.ActiveEvent{}, ClaimHeld, fmt.Errorf("reap stale claim: %w", err)
	}

	row, err := s.q.ClaimActiveEvent(ctx, sqlitedb.ClaimActiveEventParams{
		EventKey:   claim.Key,
		TeamID:     claim.TeamID,
		ChannelID:  claim.ChannelID,
		State:      string(claim.State),
		ClaimOwner: claim.Owner,
		ClaimedAt:  sql.NullTime{Time: claim.At, Valid: true},
		LastUpdate: claim.At,
	})
	switch {
	case err == nil:
		if reaped > 0 {
			return activeEventOf(row), ClaimRecovered, nil
		}
		return activeEventOf(row), ClaimAcquired, nil
	case !errors.Is(notFound(err), ErrNotFound):
		return models.ActiveEvent{}, ClaimHeld, fmt.Errorf("claim active event: %w", err)
	}

	// The insert conflicted, so somebody was there first. Which of the two
	// answers it is depends on whether they got as far as posting.
	existing, err := s.GetActiveEvent(ctx, claim.Key, claim.TeamID, claim.ChannelID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Gone between the two statements: a close deleted it. Treat it
			// as held rather than looping, and let the sender retry.
			return models.ActiveEvent{}, ClaimHeld, nil
		}
		return models.ActiveEvent{}, ClaimHeld, err
	}
	if existing.Posted() {
		return existing, ClaimPosted, nil
	}
	return existing, ClaimHeld, nil
}

func (s queryAdapter) CompleteActiveEventClaim(ctx context.Context, claim models.EventClaim, messageID, conversationID string, at time.Time) error {
	_, err := s.q.CompleteActiveEventClaim(ctx, sqlitedb.CompleteActiveEventClaimParams{
		EventKey:       claim.Key,
		TeamID:         claim.TeamID,
		ChannelID:      claim.ChannelID,
		State:          string(claim.State),
		MessageID:      messageID,
		ClaimOwner:     claim.Owner,
		ClaimedAt:      sql.NullTime{Time: claim.At, Valid: true},
		PostedAt:       sql.NullTime{Time: at, Valid: true},
		LastUpdate:     at,
		ConversationID: conversationID,
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(notFound(err), ErrNotFound):
		// No row was written, so the row under this key is somebody else's
		// card. The message just posted has nothing pointing at it.
		return ErrClaimLost
	default:
		return fmt.Errorf("complete active event claim: %w", err)
	}
}

func (s queryAdapter) ReleaseActiveEventClaim(ctx context.Context, claim models.EventClaim) error {
	err := s.q.ReleaseActiveEventClaim(ctx, sqlitedb.ReleaseActiveEventClaimParams{
		EventKey:   claim.Key,
		TeamID:     claim.TeamID,
		ChannelID:  claim.ChannelID,
		ClaimOwner: claim.Owner,
	})
	if err != nil {
		return fmt.Errorf("release active event claim: %w", err)
	}
	return nil
}

func (s queryAdapter) TouchActiveEvent(ctx context.Context, card models.ActiveEvent, state models.EventState, at time.Time) error {
	err := s.q.TouchActiveEvent(ctx, sqlitedb.TouchActiveEventParams{
		State:      string(state),
		LastUpdate: at,
		EventKey:   card.Key,
		TeamID:     card.TeamID,
		ChannelID:  card.ChannelID,
		MessageID:  card.MessageID,
	})
	if err != nil {
		return fmt.Errorf("touch active event: %w", err)
	}
	return nil
}

func (s queryAdapter) ListActiveEvents(ctx context.Context, key string) ([]models.ActiveEvent, error) {
	rows, err := s.q.ListActiveEvents(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("list active events: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.ActiveEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, activeEventOf(row))
	}
	return out, nil
}

func (s queryAdapter) CountActiveEvents(ctx context.Context) (int64, error) {
	count, err := s.q.CountActiveEvents(ctx)
	if err != nil {
		return 0, fmt.Errorf("count active events: %w", err)
	}
	return count, nil
}

func (s queryAdapter) GetActiveEvent(ctx context.Context, key, teamID, channelID string) (models.ActiveEvent, error) {
	row, err := s.q.GetActiveEvent(ctx, sqlitedb.GetActiveEventParams{
		EventKey:  key,
		TeamID:    teamID,
		ChannelID: channelID,
	})
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.ActiveEvent{}, err
		}
		return models.ActiveEvent{}, fmt.Errorf("get active event: %w", err)
	}
	return activeEventOf(row), nil
}

func (s queryAdapter) DeleteActiveEventCard(ctx context.Context, key, teamID, channelID, messageID string) error {
	err := s.q.DeleteActiveEventCard(ctx, sqlitedb.DeleteActiveEventCardParams{
		EventKey:  key,
		TeamID:    teamID,
		ChannelID: channelID,
		MessageID: messageID,
	})
	if err != nil {
		return fmt.Errorf("delete active event card: %w", err)
	}
	return nil
}

// ClaimActiveEventRecipient is ClaimActiveEvent mirrored for a chat delivery;
// see that method's comment for why reaping and claiming are two statements
// rather than one.
func (s queryAdapter) ClaimActiveEventRecipient(ctx context.Context, claim models.RecipientClaim) (models.ActiveEventRecipient, ClaimOutcome, error) {
	reaped, err := s.q.ReapStaleClaimRecipient(ctx, sqlitedb.ReapStaleClaimRecipientParams{
		EventKey:    claim.Key,
		RecipientID: claim.RecipientID,
		ClaimedAt:   sql.NullTime{Time: claim.StaleBefore, Valid: true},
	})
	if err != nil {
		return models.ActiveEventRecipient{}, ClaimHeld, fmt.Errorf("reap stale claim recipient: %w", err)
	}

	row, err := s.q.ClaimActiveEventRecipient(ctx, sqlitedb.ClaimActiveEventRecipientParams{
		EventKey:    claim.Key,
		RecipientID: claim.RecipientID,
		State:       string(claim.State),
		ClaimOwner:  claim.Owner,
		ClaimedAt:   sql.NullTime{Time: claim.At, Valid: true},
		LastUpdate:  claim.At,
	})
	switch {
	case err == nil:
		if reaped > 0 {
			return activeEventRecipientOf(row), ClaimRecovered, nil
		}
		return activeEventRecipientOf(row), ClaimAcquired, nil
	case !errors.Is(notFound(err), ErrNotFound):
		return models.ActiveEventRecipient{}, ClaimHeld, fmt.Errorf("claim active event recipient: %w", err)
	}

	existing, err := s.getActiveEventRecipient(ctx, claim.Key, claim.RecipientID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return models.ActiveEventRecipient{}, ClaimHeld, nil
		}
		return models.ActiveEventRecipient{}, ClaimHeld, err
	}
	if existing.Posted() {
		return existing, ClaimPosted, nil
	}
	return existing, ClaimHeld, nil
}

func (s queryAdapter) CompleteActiveEventRecipientClaim(ctx context.Context, claim models.RecipientClaim, messageID string, at time.Time) error {
	_, err := s.q.CompleteActiveEventRecipientClaim(ctx, sqlitedb.CompleteActiveEventRecipientClaimParams{
		EventKey:    claim.Key,
		RecipientID: claim.RecipientID,
		State:       string(claim.State),
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
		return ErrClaimLost
	default:
		return fmt.Errorf("complete active event recipient claim: %w", err)
	}
}

func (s queryAdapter) ReleaseActiveEventRecipientClaim(ctx context.Context, claim models.RecipientClaim) error {
	err := s.q.ReleaseActiveEventRecipientClaim(ctx, sqlitedb.ReleaseActiveEventRecipientClaimParams{
		EventKey:    claim.Key,
		RecipientID: claim.RecipientID,
		ClaimOwner:  claim.Owner,
	})
	if err != nil {
		return fmt.Errorf("release active event recipient claim: %w", err)
	}
	return nil
}

func (s queryAdapter) TouchActiveEventRecipient(ctx context.Context, card models.ActiveEventRecipient, state models.EventState, at time.Time) error {
	err := s.q.TouchActiveEventRecipient(ctx, sqlitedb.TouchActiveEventRecipientParams{
		State:       string(state),
		LastUpdate:  at,
		EventKey:    card.Key,
		RecipientID: card.RecipientID,
		MessageID:   card.MessageID,
	})
	if err != nil {
		return fmt.Errorf("touch active event recipient: %w", err)
	}
	return nil
}

func (s queryAdapter) ListActiveEventRecipients(ctx context.Context, key string) ([]models.ActiveEventRecipient, error) {
	rows, err := s.q.ListActiveEventRecipients(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("list active event recipients: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]models.ActiveEventRecipient, 0, len(rows))
	for _, row := range rows {
		out = append(out, activeEventRecipientOf(row))
	}
	return out, nil
}

// getActiveEventRecipient is not part of Store: it exists only to let
// ClaimActiveEventRecipient read the row an insert conflicted against, the
// same role GetActiveEvent plays for ClaimActiveEvent.
func (s queryAdapter) getActiveEventRecipient(ctx context.Context, key, recipientID string) (models.ActiveEventRecipient, error) {
	row, err := s.q.GetActiveEventRecipient(ctx, sqlitedb.GetActiveEventRecipientParams{
		EventKey:    key,
		RecipientID: recipientID,
	})
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.ActiveEventRecipient{}, err
		}
		return models.ActiveEventRecipient{}, fmt.Errorf("get active event recipient: %w", err)
	}
	return activeEventRecipientOf(row), nil
}

func (s queryAdapter) DeleteActiveEventRecipientCard(ctx context.Context, key, recipientID, messageID string) error {
	err := s.q.DeleteActiveEventRecipientCard(ctx, sqlitedb.DeleteActiveEventRecipientCardParams{
		EventKey:    key,
		RecipientID: recipientID,
		MessageID:   messageID,
	})
	if err != nil {
		return fmt.Errorf("delete active event recipient card: %w", err)
	}
	return nil
}

func (s queryAdapter) DeleteActiveEventRecipientsFor(ctx context.Context, recipientID string) error {
	if err := s.q.DeleteActiveEventRecipientsFor(ctx, recipientID); err != nil {
		return fmt.Errorf("delete active event recipients for: %w", err)
	}
	return nil
}

func activeEventRecipientOf(row sqlitedb.ActiveEventRecipient) models.ActiveEventRecipient {
	return models.ActiveEventRecipient{
		Key:         row.EventKey,
		State:       models.EventState(row.State),
		RecipientID: row.RecipientID,
		MessageID:   row.MessageID,
		ClaimOwner:  row.ClaimOwner,
		ClaimedAt:   row.ClaimedAt.Time,
		PostedAt:    row.PostedAt.Time,
		LastUpdate:  row.LastUpdate,
	}
}

func activeEventOf(row sqlitedb.ActiveEvent) models.ActiveEvent {
	return models.ActiveEvent{
		Key:            row.EventKey,
		State:          models.EventState(row.State),
		TeamID:         row.TeamID,
		ChannelID:      row.ChannelID,
		MessageID:      row.MessageID,
		ConversationID: row.ConversationID,
		ClaimOwner:     row.ClaimOwner,
		ClaimedAt:      row.ClaimedAt.Time,
		PostedAt:       row.PostedAt.Time,
		LastUpdate:     row.LastUpdate,
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
	identity, err := encodeIdentity(session.Identity)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	err = s.q.CreateSession(ctx, sqlitedb.CreateSessionParams{
		ID:        session.ID,
		Subject:   session.Subject,
		Name:      session.Name,
		Source:    session.Source,
		Role:      session.Roles,
		Identity:  identity,
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
		Identity:  decodeIdentity(row.Identity),
		CreatedAt: row.CreatedAt,
		ExpiresAt: row.ExpiresAt,
	})
}

// encodeIdentity stores an empty identity as ”, the column default a session
// the previous release wrote carries too.
func encodeIdentity(identity models.Identity) (string, error) {
	encoded, err := json.Marshal(identity)
	if err != nil || string(encoded) == "{}" {
		return "", err
	}
	return string(encoded), nil
}

// decodeIdentity reads an unreadable identity as none: it only feeds a page
// that explains a sign-in, and must not cost anybody their session.
func decodeIdentity(stored string) models.Identity {
	var identity models.Identity
	if stored != "" {
		_ = json.Unmarshal([]byte(stored), &identity)
	}
	return identity
}

func (s queryAdapter) DeleteSession(ctx context.Context, id string) error {
	if err := s.q.DeleteSession(ctx, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// deleteSessionCascade is DeleteRecipient's cascade pattern mirrored for a
// session: a broker_tokens row is a live Keycloak credential, and it must not
// survive the session it was fetched for. Both deletes run in one
// transaction, and this repo declares no SQL foreign keys (see the sqlite
// migration for why), so the cascade lives here rather than in the schema.
func deleteSessionCascade(ctx context.Context, s interface {
	WithTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error
}, id string) error {
	return s.WithTx(ctx, func(ctx context.Context, tx Store) error {
		if err := tx.DeleteBrokerToken(ctx, id); err != nil {
			return err
		}
		return tx.DeleteSession(ctx, id)
	})
}

func (s queryAdapter) CreateBrokerToken(ctx context.Context, t models.BrokerToken) error {
	err := s.q.CreateBrokerToken(ctx, sqlitedb.CreateBrokerTokenParams{
		SessionID:    t.SessionID,
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    t.ExpiresAt,
		UpdatedAt:    t.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("create broker token: %w", err)
	}
	return nil
}

func (s queryAdapter) GetBrokerToken(ctx context.Context, sessionID string) (models.BrokerToken, error) {
	row, err := s.q.GetBrokerToken(ctx, sessionID)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.BrokerToken{}, err
		}
		return models.BrokerToken{}, fmt.Errorf("get broker token: %w", err)
	}
	return models.BrokerToken{
		SessionID:    row.SessionID,
		AccessToken:  row.AccessToken,
		RefreshToken: row.RefreshToken,
		ExpiresAt:    row.ExpiresAt,
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

func (s queryAdapter) UpdateBrokerToken(ctx context.Context, t models.BrokerToken) error {
	err := s.q.UpdateBrokerToken(ctx, sqlitedb.UpdateBrokerTokenParams{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    t.ExpiresAt,
		UpdatedAt:    t.UpdatedAt,
		SessionID:    t.SessionID,
	})
	if err != nil {
		return fmt.Errorf("update broker token: %w", err)
	}
	return nil
}

func (s queryAdapter) DeleteBrokerToken(ctx context.Context, sessionID string) error {
	if err := s.q.DeleteBrokerToken(ctx, sessionID); err != nil {
		return fmt.Errorf("delete broker token: %w", err)
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

// MarkRecipientBlocked and ClearRecipientBlocked write only the two blocked
// columns, never the rest of the row: delivery -- the only caller of either --
// has a conversation reference it read for sending, not one it is safe to
// write back over whatever an admin or a re-link may have changed since.
func (s queryAdapter) GetRecipientByConversation(ctx context.Context, conversationID string) (models.Recipient, error) {
	row, err := s.q.GetRecipientByConversation(ctx, conversationID)
	if err != nil {
		if err := notFound(err); errors.Is(err, ErrNotFound) {
			return models.Recipient{}, err
		}
		return models.Recipient{}, fmt.Errorf("get recipient by conversation: %w", err)
	}
	return recipientOf(row), nil
}

func (s queryAdapter) MarkRecipientBlocked(ctx context.Context, id string, at time.Time, reason string) error {
	if err := s.q.MarkRecipientBlocked(ctx, sqlitedb.MarkRecipientBlockedParams{
		BlockedAt:     sql.NullTime{Time: at, Valid: true},
		BlockedReason: reason,
		ID:            id,
	}); err != nil {
		return fmt.Errorf("mark recipient blocked: %w", err)
	}
	return nil
}

func (s queryAdapter) ClearRecipientBlocked(ctx context.Context, id string) error {
	if err := s.q.ClearRecipientBlocked(ctx, id); err != nil {
		return fmt.Errorf("clear recipient blocked: %w", err)
	}
	return nil
}

// nullTime is the zero-value convention every nullable timestamp in this
// adapter follows: a zero time.Time means "unset," which is NULL in the
// database, and any other value is a real stamp.
func nullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: !t.IsZero()}
}

func (s queryAdapter) RecordEventSamples(ctx context.Context, samples []models.EventSample) error {
	for _, sample := range samples {
		err := s.q.UpsertEventSample(ctx, sqlitedb.UpsertEventSampleParams{
			Kind:      string(sample.Kind),
			Key:       sample.Key,
			Value:     sample.Value,
			SeenCount: sample.SeenCount,
			FirstSeen: sample.FirstSeen,
			LastSeen:  sample.LastSeen,
		})
		if err != nil {
			return fmt.Errorf("record event sample %s %q: %w", sample.Kind, sample.Key, err)
		}
	}
	return nil
}

// recordEventSamplesInTx is the store-level RecordEventSamples: one commit per
// batch rather than per row, which on SQLite is one fsync instead of dozens.
func recordEventSamplesInTx(ctx context.Context, s interface {
	WithTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error
}, samples []models.EventSample) error {
	if len(samples) == 0 {
		return nil
	}
	return s.WithTx(ctx, func(ctx context.Context, tx Store) error {
		return tx.RecordEventSamples(ctx, samples)
	})
}

func (s queryAdapter) ListEventSamples(ctx context.Context, limit int) ([]models.EventSample, error) {
	rows, err := s.q.ListEventSamples(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list event samples: %w", err)
	}
	out := make([]models.EventSample, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.EventSample{
			Kind:      models.SampleKind(row.Kind),
			Key:       row.Key,
			Value:     row.Value,
			SeenCount: row.SeenCount,
			FirstSeen: row.FirstSeen,
			LastSeen:  row.LastSeen,
		})
	}
	return out, nil
}

func (s queryAdapter) PruneEventSamples(ctx context.Context, cutoff time.Time, keepPerKey int) (int64, error) {
	expired, err := s.q.DeleteEventSamplesSeenBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune expired event samples: %w", err)
	}
	excess, err := s.q.DeleteExcessEventSampleValues(ctx, int64(keepPerKey))
	if err != nil {
		return expired, fmt.Errorf("prune excess event sample values: %w", err)
	}
	return expired + excess, nil
}

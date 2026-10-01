package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// Resource types, as Cedar names them, and the settings that are not records.
const (
	TypeTemplate        = "Template"
	TypeDestination     = "Destination"
	TypeRoute           = "Route"
	TypeWebhookEndpoint = "WebhookEndpoint"
	TypeAccessToken     = "AccessToken"
	TypeGrant           = "Grant"
	TypeRecipient       = "Recipient"
	TypeSetting         = "Setting"
	TypeDirectoryRun    = "DirectoryRun"
	TypeConfig          = "Config"
	TypeUser            = "User"
	TypeGroup           = "Group"
)

// Setting ids in the trail; stable names rather than the store's keys.
const (
	SettingGlobalDefaultTemplate = "global-default-template"
	SettingSourceDefaultPrefix   = "source-default-template:"
)

// Wrap audits every configuration write made through the returned store, in
// one place, so the UI, the API and an import cannot disagree about it.
func Wrap(inner store.Store, rec *Recorder) store.Store {
	if rec == nil {
		return inner
	}
	return &auditedStore{Store: inner, rec: rec}
}

type auditedStore struct {
	store.Store
	rec *Recorder
	// pending is set inside a transaction: events wait for the commit, so a
	// rolled-back change leaves no record of something that never happened.
	pending *[]queued
}

// queued is an event waiting for its commit; written ones are already in the trail.
type queued struct {
	event   models.AuditEvent
	written bool
}

func (s *auditedStore) emit(ctx context.Context, e models.AuditEvent) {
	e = s.rec.Stamp(ctx, e)
	if s.pending != nil {
		*s.pending = append(*s.pending, queued{event: e})
		return
	}
	s.rec.deliver(ctx, e, true)
}

func (s *auditedStore) inTx(ctx context.Context, run func(context.Context, func(context.Context, store.Store) error) error, fn func(context.Context, store.Store) error) error {
	var pending []queued
	err := run(ctx, func(ctx context.Context, tx store.Store) error {
		// A serializable transaction may run fn again; only the last run counts.
		pending = pending[:0]
		return fn(ctx, &auditedStore{Store: tx, rec: s.rec, pending: &pending})
	})
	if err != nil {
		return err
	}
	for _, q := range pending {
		s.rec.deliver(ctx, q.event, !q.written)
	}
	return nil
}

// critical makes the change and writes its record in one transaction, so a
// permission change without a record cannot happen (ADR 0074). change returns
// the event, or false when nothing changed. Without a database trail it is an
// ordinary emit.
func (s *auditedStore) critical(ctx context.Context, change func(ctx context.Context, tx store.Store) (models.AuditEvent, bool, error)) error {
	if !s.rec.trail() {
		e, ok, err := change(ctx, s.Store)
		if err == nil && ok {
			s.emit(ctx, e)
		}
		return err
	}
	record := func(ctx context.Context, tx store.Store, pending *[]queued) error {
		e, ok, err := change(ctx, tx)
		if err != nil || !ok {
			return err
		}
		e = s.rec.Stamp(ctx, e)
		if err := tx.InsertAuditEvent(ctx, e); err != nil {
			return fmt.Errorf("audit %s: %w", e.Action, err)
		}
		*pending = append(*pending, queued{event: e, written: true})
		return nil
	}
	if s.pending != nil {
		return record(ctx, s.Store, s.pending)
	}
	var pending []queued
	err := s.Store.WithSerializableTx(ctx, func(ctx context.Context, tx store.Store) error {
		pending = pending[:0]
		return record(ctx, tx, &pending)
	})
	if err != nil {
		return err
	}
	for _, q := range pending {
		s.rec.deliver(ctx, q.event, false)
	}
	return nil
}

func (s *auditedStore) WithTx(ctx context.Context, fn func(context.Context, store.Store) error) error {
	return s.inTx(ctx, s.Store.WithTx, fn)
}

func (s *auditedStore) WithSerializableTx(ctx context.Context, fn func(context.Context, store.Store) error) error {
	return s.inTx(ctx, s.Store.WithSerializableTx, fn)
}

// snapshot is the JSON a record is kept as; the models' tags already leave
// secrets such as token digests out.
func snapshot(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

// found is the snapshot of a record read before a change, or nothing when it
// could not be read, which the change itself will then report.
func found[T any](v T, err error) json.RawMessage {
	if err != nil {
		return nil
	}
	return snapshot(v)
}

// A prior is a record as it was before an update or delete.
type prior struct {
	raw json.RawMessage
	// missing: the store accepts writes to absent rows; those changed nothing.
	missing bool
}

func lookup[T any](v T, err error) prior {
	return prior{raw: found(v, err), missing: errors.Is(err, store.ErrNotFound)}
}

func event(action, typ, id string, before, after json.RawMessage) models.AuditEvent {
	return models.AuditEvent{Action: action, ResourceType: typ, ResourceID: id, Before: before, After: after}
}

// Templates.

func (s *auditedStore) CreateTemplate(ctx context.Context, t models.Template) (models.Template, error) {
	created, err := s.Store.CreateTemplate(ctx, t)
	if err == nil {
		s.emit(ctx, event("template.create", TypeTemplate, created.ID, nil, snapshot(created)))
	}
	return created, err
}

func (s *auditedStore) UpdateTemplate(ctx context.Context, t models.Template) (models.Template, error) {
	before := lookup(s.GetTemplate(ctx, t.ID))
	updated, err := s.Store.UpdateTemplate(ctx, t)
	if err == nil && !before.missing {
		s.emit(ctx, event("template.update", TypeTemplate, updated.ID, before.raw, snapshot(updated)))
	}
	return updated, err
}

func (s *auditedStore) DeleteTemplate(ctx context.Context, id string) error {
	before := lookup(s.GetTemplate(ctx, id))
	err := s.Store.DeleteTemplate(ctx, id)
	if err == nil && !before.missing {
		s.emit(ctx, event("template.delete", TypeTemplate, id, before.raw, nil))
	}
	return err
}

func (s *auditedStore) SeedTemplates(ctx context.Context, templates []models.Template) (bool, error) {
	seeded, err := s.Store.SeedTemplates(ctx, templates)
	if err == nil && seeded {
		names := make([]string, 0, len(templates))
		for _, t := range templates {
			names = append(names, t.Name)
		}
		s.emit(ctx, event("template.seed", TypeTemplate, "", nil, snapshot(map[string]any{"names": names})))
	}
	return seeded, err
}

func templateSetting(templateID string) json.RawMessage {
	return snapshot(map[string]string{"template_id": templateID})
}

func (s *auditedStore) SetGlobalDefaultTemplate(ctx context.Context, templateID string) error {
	before, beforeErr := s.GetGlobalDefaultTemplate(ctx)
	err := s.Store.SetGlobalDefaultTemplate(ctx, templateID)
	if err == nil {
		s.emit(ctx, event("setting.update", TypeSetting, SettingGlobalDefaultTemplate, found(templateSetting(before), beforeErr), templateSetting(templateID)))
	}
	return err
}

func (s *auditedStore) SetSourceDefaultTemplate(ctx context.Context, source, templateID string) error {
	before, beforeErr := s.GetSourceDefaultTemplate(ctx, source)
	err := s.Store.SetSourceDefaultTemplate(ctx, source, templateID)
	if err == nil {
		s.emit(ctx, event("setting.update", TypeSetting, SettingSourceDefaultPrefix+source, found(templateSetting(before), beforeErr), templateSetting(templateID)))
	}
	return err
}

// Destinations.

func (s *auditedStore) CreateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	created, err := s.Store.CreateDestination(ctx, d)
	if err == nil {
		s.emit(ctx, event("destination.create", TypeDestination, created.ID, nil, snapshot(created)))
	}
	return created, err
}

func (s *auditedStore) UpdateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	before := lookup(s.GetDestination(ctx, d.ID))
	updated, err := s.Store.UpdateDestination(ctx, d)
	if err == nil && !before.missing {
		s.emit(ctx, event("destination.update", TypeDestination, updated.ID, before.raw, snapshot(updated)))
	}
	return updated, err
}

func (s *auditedStore) DeleteDestination(ctx context.Context, id string) error {
	before := lookup(s.GetDestination(ctx, id))
	err := s.Store.DeleteDestination(ctx, id)
	if err == nil && !before.missing {
		s.emit(ctx, event("destination.delete", TypeDestination, id, before.raw, nil))
	}
	return err
}

func (s *auditedStore) SetDefaultDestination(ctx context.Context, id string) error {
	var before json.RawMessage
	if current, err := s.GetDefaultDestination(ctx); err == nil {
		before = snapshot(map[string]string{"destination_id": current.ID})
	}
	err := s.Store.SetDefaultDestination(ctx, id)
	if err == nil {
		s.emit(ctx, event("destination.default", TypeDestination, id, before, snapshot(map[string]string{"destination_id": id})))
	}
	return err
}

// Routes.

func (s *auditedStore) CreateRoute(ctx context.Context, r models.Route) (models.Route, error) {
	created, err := s.Store.CreateRoute(ctx, r)
	if err == nil {
		s.emit(ctx, event("route.create", TypeRoute, created.ID, nil, snapshot(created)))
	}
	return created, err
}

func (s *auditedStore) UpdateRoute(ctx context.Context, r models.Route) (models.Route, error) {
	before := lookup(s.GetRoute(ctx, r.ID))
	updated, err := s.Store.UpdateRoute(ctx, r)
	if err == nil && !before.missing {
		s.emit(ctx, event("route.update", TypeRoute, updated.ID, before.raw, snapshot(updated)))
	}
	return updated, err
}

func (s *auditedStore) DeleteRoute(ctx context.Context, id string) error {
	before := lookup(s.GetRoute(ctx, id))
	err := s.Store.DeleteRoute(ctx, id)
	if err == nil && !before.missing {
		s.emit(ctx, event("route.delete", TypeRoute, id, before.raw, nil))
	}
	return err
}

// Webhook endpoints.

func (s *auditedStore) CreateWebhookEndpoint(ctx context.Context, e models.WebhookEndpoint) (models.WebhookEndpoint, error) {
	created, err := s.Store.CreateWebhookEndpoint(ctx, e)
	if err == nil {
		s.emit(ctx, event("webhook_endpoint.create", TypeWebhookEndpoint, created.ID, nil, snapshot(created)))
	}
	return created, err
}

func (s *auditedStore) UpdateWebhookEndpoint(ctx context.Context, e models.WebhookEndpoint) (models.WebhookEndpoint, error) {
	before := lookup(s.GetWebhookEndpoint(ctx, e.ID))
	updated, err := s.Store.UpdateWebhookEndpoint(ctx, e)
	if err == nil && !before.missing {
		s.emit(ctx, event("webhook_endpoint.update", TypeWebhookEndpoint, updated.ID, before.raw, snapshot(updated)))
	}
	return updated, err
}

func (s *auditedStore) RotateWebhookEndpointToken(ctx context.Context, id, tokenHash string) error {
	err := s.Store.RotateWebhookEndpointToken(ctx, id, tokenHash)
	if err == nil {
		s.emit(ctx, event("webhook_endpoint.rotate", TypeWebhookEndpoint, id, nil, nil))
	}
	return err
}

func (s *auditedStore) DeleteWebhookEndpoint(ctx context.Context, id string) error {
	before := lookup(s.GetWebhookEndpoint(ctx, id))
	err := s.Store.DeleteWebhookEndpoint(ctx, id)
	if err == nil && !before.missing {
		s.emit(ctx, event("webhook_endpoint.delete", TypeWebhookEndpoint, id, before.raw, nil))
	}
	return err
}

// Access tokens.

func (s *auditedStore) CreateAccessToken(ctx context.Context, t models.AccessToken) (models.AccessToken, error) {
	created, err := s.Store.CreateAccessToken(ctx, t)
	if err == nil {
		s.emit(ctx, event("access_token.create", TypeAccessToken, created.ID, nil, snapshot(created)))
	}
	return created, err
}

func (s *auditedStore) DeleteAccessToken(ctx context.Context, id string) error {
	var before json.RawMessage
	if tokens, err := s.ListAccessTokens(ctx); err == nil {
		if i := slices.IndexFunc(tokens, func(t models.AccessToken) bool { return t.ID == id }); i >= 0 {
			before = snapshot(tokens[i])
		}
	}
	err := s.Store.DeleteAccessToken(ctx, id)
	if err == nil {
		s.emit(ctx, event("access_token.delete", TypeAccessToken, id, before, nil))
	}
	return err
}

// Grants.

func (s *auditedStore) grants(ctx context.Context, keep func(models.Grant) bool) []models.Grant {
	all, err := s.ListGrants(ctx)
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(all, func(g models.Grant) bool { return !keep(g) })
}

func (s *auditedStore) CreateGrant(ctx context.Context, g models.Grant) (models.Grant, error) {
	created, err := s.Store.CreateGrant(ctx, g)
	if err == nil {
		s.emit(ctx, event("grant.create", TypeGrant, created.ID, nil, snapshot(created)))
	}
	return created, err
}

func (s *auditedStore) DeleteGrant(ctx context.Context, id string) error {
	var before json.RawMessage
	if matched := s.grants(ctx, func(g models.Grant) bool { return g.ID == id }); len(matched) == 1 {
		before = snapshot(matched[0])
	}
	err := s.Store.DeleteGrant(ctx, id)
	if err == nil {
		s.emit(ctx, event("grant.delete", TypeGrant, id, before, nil))
	}
	return err
}

func (s *auditedStore) DeleteGrantsForRole(ctx context.Context, role string) error {
	matched := s.grants(ctx, func(g models.Grant) bool { return g.Role == role })
	err := s.Store.DeleteGrantsForRole(ctx, role)
	if err == nil && len(matched) > 0 {
		for _, g := range matched {
			s.emit(ctx, event("grant.delete", TypeGrant, g.ID, snapshot(g), nil))
		}
	}
	return err
}

// Recipients: linking and unlinking a chat, not the bot's bookkeeping on it.

func (s *auditedStore) CreateRecipient(ctx context.Context, r models.Recipient) (models.Recipient, error) {
	created, err := s.Store.CreateRecipient(ctx, r)
	if err == nil {
		s.emit(ctx, event("recipient.create", TypeRecipient, created.ID, nil, snapshot(created)))
	}
	return created, err
}

func (s *auditedStore) DeleteRecipient(ctx context.Context, id string) error {
	before := lookup(s.GetRecipient(ctx, id))
	err := s.Store.DeleteRecipient(ctx, id)
	if err == nil && !before.missing {
		s.emit(ctx, event("recipient.delete", TypeRecipient, id, before.raw, nil))
	}
	return err
}

// Directory runs: only the ones a person asked for; periodic runs are routine.

func (s *auditedStore) RequestDirectoryRun(ctx context.Context, r models.DirectoryRun) (models.DirectoryRun, error) {
	requested, err := s.Store.RequestDirectoryRun(ctx, r)
	if err == nil && requested.Kind == models.RunManual {
		s.emit(ctx, event("directory_run.request", TypeDirectoryRun, requested.ID, nil, snapshot(requested)))
	}
	return requested, err
}

// Users: disabling and enabling; a sign-in is not a configuration change.

func (s *auditedStore) DisableUser(ctx context.Context, subject, by string) error {
	before := lookup(s.GetUser(ctx, subject))
	err := s.Store.DisableUser(ctx, subject, by)
	if err == nil {
		s.emit(ctx, event("user.disable", TypeUser, subject, before.raw, found(s.GetUser(ctx, subject))))
	}
	return err
}

func (s *auditedStore) EnableUser(ctx context.Context, subject string) error {
	before := lookup(s.GetUser(ctx, subject))
	err := s.Store.EnableUser(ctx, subject)
	if err == nil {
		s.emit(ctx, event("user.enable", TypeUser, subject, before.raw, found(s.GetUser(ctx, subject))))
	}
	return err
}

// Groups: who is in what decides access, so each change and its record commit
// together or not at all.

func (s *auditedStore) CreateGroup(ctx context.Context, g models.Group) (models.Group, error) {
	var created models.Group
	err := s.critical(ctx, func(ctx context.Context, tx store.Store) (models.AuditEvent, bool, error) {
		var err error
		created, err = tx.CreateGroup(ctx, g)
		return event("group.create", TypeGroup, created.ID, nil, snapshot(created)), err == nil, err
	})
	return created, err
}

func (s *auditedStore) UpdateGroup(ctx context.Context, g models.Group) (models.Group, error) {
	var updated models.Group
	err := s.critical(ctx, func(ctx context.Context, tx store.Store) (models.AuditEvent, bool, error) {
		before := lookup(tx.GetGroup(ctx, g.ID))
		var err error
		updated, err = tx.UpdateGroup(ctx, g)
		return event("group.update", TypeGroup, g.ID, before.raw, snapshot(updated)), err == nil, err
	})
	return updated, err
}

func (s *auditedStore) DeleteGroup(ctx context.Context, id string) error {
	return s.critical(ctx, func(ctx context.Context, tx store.Store) (models.AuditEvent, bool, error) {
		group, err := tx.GetGroup(ctx, id)
		if err != nil {
			return models.AuditEvent{}, false, err
		}
		members, err := tx.ListGroupMembers(ctx, id)
		if err != nil {
			return models.AuditEvent{}, false, err
		}
		if err := tx.DeleteGroup(ctx, id); err != nil {
			return models.AuditEvent{}, false, err
		}
		before := snapshot(map[string]any{"group": group, "members": members})
		return event("group.delete", TypeGroup, id, before, nil), true, nil
	})
}

func (s *auditedStore) AddGroupMember(ctx context.Context, m models.GroupMember) error {
	return s.critical(ctx, func(ctx context.Context, tx store.Store) (models.AuditEvent, bool, error) {
		err := tx.AddGroupMember(ctx, m)
		return event("group.member.add", TypeGroup, m.GroupID, nil, snapshot(m)), err == nil, err
	})
}

func (s *auditedStore) RemoveGroupMember(ctx context.Context, m models.GroupMember) error {
	return s.critical(ctx, func(ctx context.Context, tx store.Store) (models.AuditEvent, bool, error) {
		err := tx.RemoveGroupMember(ctx, m)
		return event("group.member.remove", TypeGroup, m.GroupID, snapshot(m), nil), err == nil, err
	})
}

// Permissions: recorded against the resource they are on, in the change's
// transaction, so a resource's trail shows who could reach it and when.

func (s *auditedStore) PutPermission(ctx context.Context, p models.Permission) (models.Permission, error) {
	var saved models.Permission
	err := s.critical(ctx, func(ctx context.Context, tx store.Store) (models.AuditEvent, bool, error) {
		var before json.RawMessage
		if rows, err := tx.ListPermissionsFor(ctx, p.ResourceType, p.ResourceID); err == nil {
			for _, existing := range rows {
				if existing.PrincipalType == p.PrincipalType && existing.PrincipalID == p.PrincipalID {
					before = snapshot(existing)
				}
			}
		}
		var err error
		saved, err = tx.PutPermission(ctx, p)
		if err != nil {
			return models.AuditEvent{}, false, err
		}
		if len(p.Actions) == 0 {
			return event("permission.revoke", p.ResourceType, p.ResourceID, before, nil), before != nil, nil
		}
		return event("permission.grant", p.ResourceType, p.ResourceID, before, snapshot(saved)), true, nil
	})
	return saved, err
}

func (s *auditedStore) DeletePermission(ctx context.Context, id string) error {
	return s.critical(ctx, func(ctx context.Context, tx store.Store) (models.AuditEvent, bool, error) {
		existing, err := tx.GetPermission(ctx, id)
		if err != nil {
			return models.AuditEvent{}, false, err
		}
		if err := tx.DeletePermission(ctx, id); err != nil {
			return models.AuditEvent{}, false, err
		}
		return event("permission.revoke", existing.ResourceType, existing.ResourceID, snapshot(existing), nil), true, nil
	})
}

func (s *auditedStore) DeletePermissionsFor(ctx context.Context, resourceType, resourceID string) error {
	return s.critical(ctx, func(ctx context.Context, tx store.Store) (models.AuditEvent, bool, error) {
		existing, err := tx.ListPermissionsFor(ctx, resourceType, resourceID)
		if err != nil {
			return models.AuditEvent{}, false, err
		}
		if err := tx.DeletePermissionsFor(ctx, resourceType, resourceID); err != nil {
			return models.AuditEvent{}, false, err
		}
		return event("permission.revoke", resourceType, resourceID, snapshot(map[string]any{"permissions": existing}), nil), len(existing) > 0, nil
	})
}

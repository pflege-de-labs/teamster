-- +goose Up
-- The SQLite 0019 change, in this dialect. See migrations/sqlite/0019_events.sql
-- for why it breaks the previous release and how templates are rewritten.
ALTER TABLE active_alerts RENAME TO active_events;
ALTER TABLE active_events RENAME COLUMN fingerprint TO event_key;
ALTER TABLE active_events RENAME COLUMN status TO state;
ALTER TABLE active_events RENAME CONSTRAINT active_alerts_pkey TO active_events_pkey;
ALTER TABLE active_events RENAME CONSTRAINT active_alerts_check TO active_events_check;
UPDATE active_events SET state = CASE state WHEN 'firing' THEN 'open' WHEN 'resolved' THEN 'closed' ELSE state END;

ALTER TABLE active_alert_recipients RENAME TO active_event_recipients;
ALTER TABLE active_event_recipients RENAME COLUMN fingerprint TO event_key;
ALTER TABLE active_event_recipients RENAME COLUMN status TO state;
ALTER TABLE active_event_recipients RENAME CONSTRAINT active_alert_recipients_pkey TO active_event_recipients_pkey;
ALTER TABLE active_event_recipients RENAME CONSTRAINT active_alert_recipients_check TO active_event_recipients_check;
UPDATE active_event_recipients SET state = CASE state WHEN 'firing' THEN 'open' WHEN 'resolved' THEN 'closed' ELSE state END;

ALTER TABLE alert_samples RENAME TO event_samples;
ALTER TABLE event_samples RENAME CONSTRAINT alert_samples_pkey TO event_samples_pkey;
ALTER TABLE event_samples DROP CONSTRAINT alert_samples_kind_check;
UPDATE event_samples SET kind = 'attribute' WHERE kind = 'annotation';
ALTER TABLE event_samples ADD CONSTRAINT event_samples_kind_check CHECK (kind IN ('label', 'attribute'));
ALTER INDEX alert_samples_last_seen RENAME TO event_samples_last_seen;
ALTER INDEX alert_samples_key_recency RENAME TO event_samples_key_recency;

-- Stored templates move from .Alert to .Event. The root goes first, so the
-- field moves below cannot catch the Alertmanager extension's own name.
UPDATE templates SET
	title        = REPLACE(REPLACE(REPLACE(REPLACE(title,        '.Alert.', '.Event.'), '.Alert ', '.Event '), '.Alert)', '.Event)'), '.Alert}', '.Event}'),
	message_text = REPLACE(REPLACE(REPLACE(REPLACE(message_text, '.Alert.', '.Event.'), '.Alert ', '.Event '), '.Alert)', '.Event)'), '.Alert}', '.Event}'),
	body         = REPLACE(REPLACE(REPLACE(REPLACE(body,         '.Alert.', '.Event.'), '.Alert ', '.Event '), '.Alert)', '.Event)'), '.Alert}', '.Event}');

-- Status comparisons, before the bare field is renamed.
UPDATE templates SET
	title        = REPLACE(REPLACE(REPLACE(REPLACE(title,        'eq .Event.Status "resolved"', 'eq .Event.State "closed"'), 'eq .Event.Status "firing"', 'eq .Event.State "open"'), 'ne .Event.Status "resolved"', 'ne .Event.State "closed"'), 'ne .Event.Status "firing"', 'ne .Event.State "open"'),
	message_text = REPLACE(REPLACE(REPLACE(REPLACE(message_text, 'eq .Event.Status "resolved"', 'eq .Event.State "closed"'), 'eq .Event.Status "firing"', 'eq .Event.State "open"'), 'ne .Event.Status "resolved"', 'ne .Event.State "closed"'), 'ne .Event.Status "firing"', 'ne .Event.State "open"'),
	body         = REPLACE(REPLACE(REPLACE(REPLACE(body,         'eq .Event.Status "resolved"', 'eq .Event.State "closed"'), 'eq .Event.Status "firing"', 'eq .Event.State "open"'), 'ne .Event.Status "resolved"', 'ne .Event.State "closed"'), 'ne .Event.Status "firing"', 'ne .Event.State "open"');

UPDATE templates SET
	title        = REPLACE(REPLACE(title,        '.Event.Status', '.Event.State'), '.Event.Fingerprint', '.Event.Key'),
	message_text = REPLACE(REPLACE(message_text, '.Event.Status', '.Event.State'), '.Event.Fingerprint', '.Event.Key'),
	body         = REPLACE(REPLACE(body,         '.Event.Status', '.Event.State'), '.Event.Fingerprint', '.Event.Key');

-- The source-specific fields move into their webhook's extension. A template
-- handling only the universal webhook gets that one; any other gets
-- Alertmanager's, the only webhook the old fields ever fully described.
UPDATE templates SET
	title        = REPLACE(REPLACE(REPLACE(title,        '.Event.Annotations', '.Event.Universal.Attributes'), '.Event.Generator', '.Event.Universal.URL'), '.Event.StartsAt', '.Event.Universal.Time'),
	message_text = REPLACE(REPLACE(REPLACE(message_text, '.Event.Annotations', '.Event.Universal.Attributes'), '.Event.Generator', '.Event.Universal.URL'), '.Event.StartsAt', '.Event.Universal.Time'),
	body         = REPLACE(REPLACE(REPLACE(body,         '.Event.Annotations', '.Event.Universal.Attributes'), '.Event.Generator', '.Event.Universal.URL'), '.Event.StartsAt', '.Event.Universal.Time')
WHERE sources = 'universal';

UPDATE templates SET
	title        = REPLACE(REPLACE(REPLACE(REPLACE(title,        '.Event.Annotations', '.Event.Alertmanager.Annotations'), '.Event.Generator', '.Event.Alertmanager.GeneratorURL'), '.Event.StartsAt', '.Event.Alertmanager.StartsAt'), '.Event.EndsAt', '.Event.Alertmanager.EndsAt'),
	message_text = REPLACE(REPLACE(REPLACE(REPLACE(message_text, '.Event.Annotations', '.Event.Alertmanager.Annotations'), '.Event.Generator', '.Event.Alertmanager.GeneratorURL'), '.Event.StartsAt', '.Event.Alertmanager.StartsAt'), '.Event.EndsAt', '.Event.Alertmanager.EndsAt'),
	body         = REPLACE(REPLACE(REPLACE(REPLACE(body,         '.Event.Annotations', '.Event.Alertmanager.Annotations'), '.Event.Generator', '.Event.Alertmanager.GeneratorURL'), '.Event.StartsAt', '.Event.Alertmanager.StartsAt'), '.Event.EndsAt', '.Event.Alertmanager.EndsAt')
WHERE sources <> 'universal';

-- +goose Down
UPDATE templates SET
	title        = REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(title,        '.Event.Alertmanager.Annotations', '.Event.Annotations'), '.Event.Alertmanager.GeneratorURL', '.Event.Generator'), '.Event.Alertmanager.StartsAt', '.Event.StartsAt'), '.Event.Alertmanager.EndsAt', '.Event.EndsAt'), '.Event.Universal.Attributes', '.Event.Annotations'), '.Event.Universal.URL', '.Event.Generator'), '.Event.Universal.Time', '.Event.StartsAt'),
	message_text = REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(message_text, '.Event.Alertmanager.Annotations', '.Event.Annotations'), '.Event.Alertmanager.GeneratorURL', '.Event.Generator'), '.Event.Alertmanager.StartsAt', '.Event.StartsAt'), '.Event.Alertmanager.EndsAt', '.Event.EndsAt'), '.Event.Universal.Attributes', '.Event.Annotations'), '.Event.Universal.URL', '.Event.Generator'), '.Event.Universal.Time', '.Event.StartsAt'),
	body         = REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(body,         '.Event.Alertmanager.Annotations', '.Event.Annotations'), '.Event.Alertmanager.GeneratorURL', '.Event.Generator'), '.Event.Alertmanager.StartsAt', '.Event.StartsAt'), '.Event.Alertmanager.EndsAt', '.Event.EndsAt'), '.Event.Universal.Attributes', '.Event.Annotations'), '.Event.Universal.URL', '.Event.Generator'), '.Event.Universal.Time', '.Event.StartsAt');

UPDATE templates SET
	title        = REPLACE(REPLACE(REPLACE(REPLACE(title,        'eq .Event.State "closed"', 'eq .Event.Status "resolved"'), 'eq .Event.State "open"', 'eq .Event.Status "firing"'), 'ne .Event.State "closed"', 'ne .Event.Status "resolved"'), 'ne .Event.State "open"', 'ne .Event.Status "firing"'),
	message_text = REPLACE(REPLACE(REPLACE(REPLACE(message_text, 'eq .Event.State "closed"', 'eq .Event.Status "resolved"'), 'eq .Event.State "open"', 'eq .Event.Status "firing"'), 'ne .Event.State "closed"', 'ne .Event.Status "resolved"'), 'ne .Event.State "open"', 'ne .Event.Status "firing"'),
	body         = REPLACE(REPLACE(REPLACE(REPLACE(body,         'eq .Event.State "closed"', 'eq .Event.Status "resolved"'), 'eq .Event.State "open"', 'eq .Event.Status "firing"'), 'ne .Event.State "closed"', 'ne .Event.Status "resolved"'), 'ne .Event.State "open"', 'ne .Event.Status "firing"');

UPDATE templates SET
	title        = REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(title,        '.Event.State', '.Event.Status'), '.Event.Key', '.Event.Fingerprint'), '.Event.', '.Alert.'), '.Event ', '.Alert '), '.Event)', '.Alert)'), '.Event}', '.Alert}'),
	message_text = REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(message_text, '.Event.State', '.Event.Status'), '.Event.Key', '.Event.Fingerprint'), '.Event.', '.Alert.'), '.Event ', '.Alert '), '.Event)', '.Alert)'), '.Event}', '.Alert}'),
	body         = REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(body,         '.Event.State', '.Event.Status'), '.Event.Key', '.Event.Fingerprint'), '.Event.', '.Alert.'), '.Event ', '.Alert '), '.Event)', '.Alert)'), '.Event}', '.Alert}');

ALTER INDEX event_samples_key_recency RENAME TO alert_samples_key_recency;
ALTER INDEX event_samples_last_seen RENAME TO alert_samples_last_seen;
ALTER TABLE event_samples DROP CONSTRAINT event_samples_kind_check;
UPDATE event_samples SET kind = 'annotation' WHERE kind = 'attribute';
ALTER TABLE event_samples ADD CONSTRAINT alert_samples_kind_check CHECK (kind IN ('label', 'annotation'));
ALTER TABLE event_samples RENAME CONSTRAINT event_samples_pkey TO alert_samples_pkey;
ALTER TABLE event_samples RENAME TO alert_samples;

UPDATE active_event_recipients SET state = CASE state WHEN 'open' THEN 'firing' WHEN 'closed' THEN 'resolved' ELSE state END;
ALTER TABLE active_event_recipients RENAME CONSTRAINT active_event_recipients_check TO active_alert_recipients_check;
ALTER TABLE active_event_recipients RENAME CONSTRAINT active_event_recipients_pkey TO active_alert_recipients_pkey;
ALTER TABLE active_event_recipients RENAME COLUMN state TO status;
ALTER TABLE active_event_recipients RENAME COLUMN event_key TO fingerprint;
ALTER TABLE active_event_recipients RENAME TO active_alert_recipients;

UPDATE active_events SET state = CASE state WHEN 'open' THEN 'firing' WHEN 'closed' THEN 'resolved' ELSE state END;
ALTER TABLE active_events RENAME CONSTRAINT active_events_check TO active_alerts_check;
ALTER TABLE active_events RENAME CONSTRAINT active_events_pkey TO active_alerts_pkey;
ALTER TABLE active_events RENAME COLUMN state TO status;
ALTER TABLE active_events RENAME COLUMN event_key TO fingerprint;
ALTER TABLE active_events RENAME TO active_alerts;

-- +goose Up
-- Alerts become events (ADR 0056). This is the one migration that breaks the
-- previous release on purpose: the tables, their columns and the template API
-- are renamed together, and there are no installations yet to carry across.
--
-- The claim tables keep their shape. Their CHECKs never mention fingerprint or
-- status, so renaming in place is enough and no rebuild is needed.
ALTER TABLE active_alerts RENAME TO active_events;
ALTER TABLE active_events RENAME COLUMN fingerprint TO event_key;
ALTER TABLE active_events RENAME COLUMN status TO state;
UPDATE active_events SET state = CASE state WHEN 'firing' THEN 'open' WHEN 'resolved' THEN 'closed' ELSE state END;

ALTER TABLE active_alert_recipients RENAME TO active_event_recipients;
ALTER TABLE active_event_recipients RENAME COLUMN fingerprint TO event_key;
ALTER TABLE active_event_recipients RENAME COLUMN status TO state;
UPDATE active_event_recipients SET state = CASE state WHEN 'firing' THEN 'open' WHEN 'resolved' THEN 'closed' ELSE state END;

-- The sample kind's CHECK changes, and SQLite cannot alter a CHECK, so the
-- table is rebuilt. It is only a cache, but copying keeps the editor's
-- completions across the upgrade.
CREATE TABLE event_samples (
	kind       TEXT NOT NULL CHECK (kind IN ('label', 'attribute')),
	key        TEXT NOT NULL,
	value      TEXT NOT NULL,
	seen_count INTEGER NOT NULL,
	first_seen DATETIME NOT NULL,
	last_seen  DATETIME NOT NULL,
	PRIMARY KEY (kind, key, value)
);
INSERT INTO event_samples (kind, key, value, seen_count, first_seen, last_seen)
SELECT CASE kind WHEN 'annotation' THEN 'attribute' ELSE kind END, key, value, seen_count, first_seen, last_seen
FROM alert_samples;
DROP TABLE alert_samples;
CREATE INDEX event_samples_last_seen ON event_samples (last_seen);
CREATE INDEX event_samples_key_recency ON event_samples (kind, key, last_seen, value);

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
-- The reverse, step for step. Fields that exist only since the Up -- the
-- Alertmanager group fields -- have no old name and are left as they are.
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

CREATE TABLE alert_samples (
	kind       TEXT NOT NULL CHECK (kind IN ('label', 'annotation')),
	key        TEXT NOT NULL,
	value      TEXT NOT NULL,
	seen_count INTEGER NOT NULL,
	first_seen DATETIME NOT NULL,
	last_seen  DATETIME NOT NULL,
	PRIMARY KEY (kind, key, value)
);
INSERT INTO alert_samples (kind, key, value, seen_count, first_seen, last_seen)
SELECT CASE kind WHEN 'attribute' THEN 'annotation' ELSE kind END, key, value, seen_count, first_seen, last_seen
FROM event_samples;
DROP TABLE event_samples;
CREATE INDEX alert_samples_last_seen ON alert_samples (last_seen);
CREATE INDEX alert_samples_key_recency ON alert_samples (kind, key, last_seen, value);

UPDATE active_event_recipients SET state = CASE state WHEN 'open' THEN 'firing' WHEN 'closed' THEN 'resolved' ELSE state END;
ALTER TABLE active_event_recipients RENAME COLUMN state TO status;
ALTER TABLE active_event_recipients RENAME COLUMN event_key TO fingerprint;
ALTER TABLE active_event_recipients RENAME TO active_alert_recipients;

UPDATE active_events SET state = CASE state WHEN 'open' THEN 'firing' WHEN 'closed' THEN 'resolved' ELSE state END;
ALTER TABLE active_events RENAME COLUMN state TO status;
ALTER TABLE active_events RENAME COLUMN event_key TO fingerprint;
ALTER TABLE active_events RENAME TO active_alerts;

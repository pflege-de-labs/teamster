# 0067. Show times in the admin UI in UTC or the browser's zone

* Status: Accepted
* Date: 2026-09-30

## Context

The admin UI printed times as they were stored, in UTC, with no zone. An operator in Germany reading
"next attempt 19:48" took it for local time and misjudged when a retry was due by two hours. Logs
and Graph's errors are in UTC too, so UTC has to stay available. Operators want local time as well.

The pages are rendered on the server, and most times sit inside translated sentences
("next attempt {0}"). The server does not know the browser's zone. Only a script can read it,
through `Intl.DateTimeFormat().resolvedOptions().timeZone`.

## Decision

We will render every time in the admin UI on the server, in a zone taken from a
`teamster_timezone` cookie, and always print the zone's abbreviation (`2026-09-30 21:48 CEST`).

* Without the cookie, times are in UTC.
* The user menu offers **UTC** and **Browser time**. `web/timezone.js` puts the browser's IANA zone
  into the form, and `POST /admin/timezone` stores it after `time.LoadLocation` accepts it.
  `Local`, the server's zone, is refused. Without JavaScript only UTC is offered.
* The cookie follows the language cookie: `HttpOnly`, `SameSite=Lax`, a year, the same origin
  check. It is not tied to a session.
* The binary embeds `time/tzdata`, so the zone names resolve in any image or host.

Alternatives considered:

* **Rewrite times in the browser** from `<time datetime>` elements. That breaks times inside
  translated sentences, flashes UTC before the script runs, and leaves a page without scripts in
  UTC with no label.
* **The server's zone as "local"**: containers run in UTC, and the server's zone says nothing about
  the person reading.
* **Store "browser" and re-read the zone on every page**: needs a round trip or a script-readable
  cookie. Storing the zone name is enough; someone who travels picks again.

## Consequences

* A time on any page names its zone, so it can't be misread.
* The binary grows by about 450 KB for the zone database.
* API responses and logs are unchanged: RFC 3339 and UTC.
* A new page must format times with `stamp(ctx, t)`, not `Format`, or it falls back to showing
  them without a zone.

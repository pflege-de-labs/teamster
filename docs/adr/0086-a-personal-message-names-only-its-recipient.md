# 0086. A personal message names only its recipient and carries their profile

* Status: Accepted
* Date: 2026-10-03

## Context

A route that delivers to the people a message names expands into one delivery per person, and each
person's message is rendered on its own with `.Recipient` set to them
([ADR 0063](0063-a-message-names-its-recipients.md)). Two things still keep one template from
serving any number of people well:

* Every person's render still sees the whole event: `.Event.Universal.Recipients`, the
  `teamster_recipient` label, and for an Alertmanager group every alert, including alerts that
  name somebody else. A template that prints any of these shows each person who else got the
  message, and what was sent to them.
* `.Recipient` holds a name, a UPN and a mail address. A letter-like message ("Dear Ms Smith, your
  laptop is due back at the Berlin office") needs more, and Entra already holds it: job title,
  department, office, business address, phones and preferred language.

## Decision

We will render each person's message from their own copy of the event, and give `.Recipient` their
Entra profile.

* **Unrolling.** `personalEvent` copies the event for one person:
  * `Universal.Recipients` and the `teamster_recipient` label keep only the entries that name them.
    A label that names only other people is dropped.
  * An Alertmanager group keeps the alerts that name them, and the alerts that name nobody. Each
    kept alert's recipient label is narrowed the same way. The flat `Annotations`, `StartsAt`,
    `EndsAt` and `GeneratorURL` are filled when exactly one alert is left, as for a group of one
    ([ADR 0084](0084-an-alertmanager-notification-is-one-event.md)).
  * An entry names the person when it is their object id, UPN or mail address, or an address the
    event named them by. `expandAddressed` records those addresses on the delivery (`Addresses`),
    which is never stored.
* **Where.** Unrolling covers the deliveries an addressed route was expanded to, their closes, and
  broadcasts. A route to one linked chat is a subscription to whatever its selector matches, so it
  still gets the whole event.
* **Keys.** The event key is derived before unrolling, and claim rows stay
  `(event_key, aad:<oid>)`. Cards already posted are edited and closed as before.
* **Profile.** Graph's user reads select `jobTitle`, `department`, `companyName`,
  `officeLocation`, `employeeId`, `streetAddress`, `postalCode`, `city`, `state`, `country`,
  `businessPhones`, `mobilePhone`, `preferredLanguage` and `usageLocation`. All of them fall
  under `User.Read.All`, which the directory already needs. They are stored as JSON in one new
  column, `directory_users.profile`. Nothing filters on them, and a field added later then needs
  no migration. `.Recipient` gains them as `JobTitle`, `Department`, `CompanyName`,
  `OfficeLocation`, `EmployeeID`, `Address` (`Street`, `PostalCode`, `City`, `State`, `Country`),
  `BusinessPhones`, `MobilePhone`, `PreferredLanguage` and `UsageLocation`. A linked chat whose
  person the directory knows gets them too.

Alternatives considered:

* **Split the event at intake, one event per recipient, each routed on its own.** A selector could
  then match the person, but a channel route that matches would post once per recipient. Every
  person would also need a key of their own, which would orphan the cards already posted under
  the shared key.
* **One column per profile field.** These columns are queryable, but nothing queries them, and
  every new field would cost a migration in both dialects.

## Consequences

* A template can greet, address and localise a message per person, and cannot leak the other
  recipients through the event.
* An Alertmanager group sent to several people shows each person only their own alerts, plus the
  alerts addressed to nobody.
* A close renders without the addresses the open was named by, so an alert addressed to a person
  by an alias they hold only in `proxyAddresses` is left out of their close. It is not shown to
  anybody else.
* Existing directory rows have an empty profile until the next reconcile (`bot.reconcile-interval`)
  or until a lookup after `bot.directory-ttl` reads the person again.
* The migration only adds a column with a default (`0031` in SQLite, `0028` in Postgres), so the
  previous release runs against it unchanged.

<p align="center">
  <img src="images/teamster-header.png" alt="" width="500" />
</p>

# Teamster

Go service that accepts Alertmanager or universal webhooks, routes alerts to Teams channels, and
posts Adaptive Cards through Microsoft Graph.

## Quick start

1. Copy config example:

```bash
cp config.example.yaml config.yaml
```

2. Start the server:

```bash
make run                                     # picks up ./config.yaml
go run ./cmd/teamster serve                    # same thing, command named
go run ./cmd/teamster -c /etc/teamster.yaml    # or point at any file
```

`serve` is the default command, so it runs when no command is given. `teamster --help` lists the
others.

The server stops on SIGINT or SIGTERM: it stops accepting connections and drains in-flight
requests for up to `server.shutdown-timeout` (default 15s) before closing the database. A second
signal kills it immediately.

3. Open the admin UI at `http://localhost:8080/admin` (basic auth from config) and configure:

- Templates (Adaptive Card JSON with Go templating)
- Destinations (Team ID, Channel ID)
- Routes (label selector -> destination + template)

## Configuration

Configuration comes from YAML files, environment variables and flags. `teamster --help` lists
every setting.

Config files are searched in this order, following the XDG Base Directory Specification:

1. `$XDG_CONFIG_DIRS/teamster/config.yaml` (default `/etc/xdg/teamster/config.yaml`)
2. `$XDG_CONFIG_HOME/teamster/config.yaml` (default `~/.config/teamster/config.yaml`)
3. `./config.yaml`

Later files override earlier ones. `--config FILE` (`-c`, `$TEAMSTER_CONFIG`) overrides the whole
search, and command line flags override everything. Environment variables named after the flags —
`TEAMSTER_SERVER_ADDR`, `TEAMSTER_GRAPH_CLIENT_SECRET`, and so on — apply only when no config file
sets the value.

YAML keys match the flag names, so nested keys are hyphenated:

```yaml
graph:
  tenant-id: "your-tenant-id"
  client-secret: "your-client-secret"
```

Unknown keys are ignored silently, so a misspelled key shows up as a startup failure in
validation. See [config.example.yaml](config.example.yaml) for the full set.

## Webhooks

### Alertmanager (0.31)

`POST /webhook/alertmanager` with `X-Teamster-Token` header.

### Universal webhook

`POST /webhook/universal` with `X-Teamster-Token` header.

Payload shape:

```json
{
  "status": "firing",
  "labels": {"alertname": "HighCPU", "severity": "critical"},
  "annotations": {"summary": "CPU spiking"},
  "starts_at": "2025-12-07T20:07:00Z",
  "ends_at": "0001-01-01T00:00:00Z",
  "generator": "custom",
  "fingerprint": "optional-stable-id"
}
```

## Templates

A template is three optional parts, and needs at least one of them:

| Part | What it is | Where it shows |
| --- | --- | --- |
| Title | A template rendering to one line of plain text | The Teams activity feed preview |
| Message text | A template rendering to formatted text | The message body |
| Adaptive Card JSON | The card, as before | Below the text |

A message that is only a card previews in the activity feed as `Card`, which is why the title
exists. A card without a title keeps the summary line this service always sent —
`annotations.summary`, then `labels.alertname`, then `Alert update` — so existing templates are
unaffected. Text without a title is left alone: the feed previews the text itself.

Message text is HTML, and it is sanitized before it is sent: `p`, `br`, `b`, `strong`, `i`, `em`,
`u`, `s`, `code`, `pre`, `blockquote`, `ul`, `ol`, `li`, `h1`–`h3` and `a` survive, `script` and
`style` are dropped with their contents, anything else is unwrapped to its text, and a link keeps
its `href` only for `http`, `https` and `mailto`. An alert annotation ends up in that text, so it
cannot be trusted to be markup-free.

## Template data

Templates receive:

- `Alert` (normalized alert payload)
- `Now` (RFC3339 string)

Helper functions:

- `toJSON` to JSON-encode structures
- `default` to provide fallbacks, including for a key an alert did not set

## Notes

- Route selection matches label selectors exactly; highest priority wins.
- Routes nest. A child refines its parent's match and sends the alert somewhere else — *as well as*
  its parent, or *instead of* it when marked greedy. A child that names no destination or template
  inherits the nearest ancestor's, so "the same card, one more channel" is a one-field route.
- A route with children cannot be deleted; remove or reparent them first, because an orphan becomes
  a root that matches alerts its parent used to filter out.
- A database file written before the `DATETIME` timestamp fix cannot be read. The server refuses
  to start against one and names the file; delete it and restart to recreate the schema.
- A default route is used if no labels match.
- Active alerts are tracked in SQLite per channel, so an alert that fans out updates and resolves
  every card it posted. One channel failing does not stop the others; the response is a `502` and
  the sender's retry updates what already landed rather than duplicating it.
- A database written before templates had a title gains the columns on the next start; nothing
  needs to be deleted.

## Sample payloads

See the JSON examples in [samples](samples):

- [samples/alertmanager-firing.json](samples/alertmanager-firing.json)
- [samples/alertmanager-resolved.json](samples/alertmanager-resolved.json)
- [samples/universal-firing.json](samples/universal-firing.json)
- [samples/universal-resolved.json](samples/universal-resolved.json)

## Signing in

The admin UI needs a session. Configure either or both:

- **An OIDC provider.** Point `auth.oidc-discovery-url` at the provider's
  `/.well-known/openid-configuration`, and set `auth.oidc-client-id` and `auth.oidc-redirect-url`,
  then name the claim that carries the roles. For Keycloak that is usually `realm_access.roles` for
  a realm role, or `resource_access.<client>.roles` for a client role. A public client using PKCE
  needs no secret.
- **Local credentials.** `admin.username` and `admin.password` are accepted at `/admin/login`.

The claim is looked for in the ID token, then at the userinfo endpoint, then in the access token,
and the first hit wins. Keycloak's built-in role mappers populate the **access token** and leave
roles out of the ID token, so a realm needs no mapper changes — and if you would rather the roles
travel in the ID token, they are used in preference. [Configuring Keycloak](docs/keycloak.md) covers
the client, the role and the mapper.

Signing in is no longer the place access is decided — the role is, and a user whose claim names none
gets `auth.default-role` or, with none configured, nothing at all. Whether everyone in the realm can
reach this client is therefore the provider's decision to make; leaving `auth.default-role` empty is
the safe setting when they can. Keep the local credentials configured either way: they are the way
back in if the provider is unreachable or the claim is wrong.

`/api` accepts either a session or those same credentials as HTTP basic auth, so existing automation
keeps working and the admin UI's own fetches work for a session that never saw the local password.

## Roles

Three roles, in order: `admin`, `editor`, `viewer`.

| Role | May |
| --- | --- |
| `admin` | everything, including whatever later releases add |
| `editor` | read the configuration and change it |
| `viewer` | read it, and nothing else |

The claim names the role directly: a provider role called `admin`, `editor` or `viewer` **is** that
role here, with nothing to configure in between. Where several are named, the most privileged wins.

`auth.default-role` is what someone gets when the claim names none of them. Set it to `viewer` and
everyone the provider authenticates may read the configuration; leave it empty and they sign in with
no access at all and are shown a page telling them to ask an administrator for a role. The local
credentials administer, because they are the way back in when the provider is wrong.

A role is decided at sign-in and travels with the session, so a change at the provider applies the
next time that person signs in. The admin UI hides the controls a role may not use and says why;
the server refuses the request either way. A refusal is a `403` naming the role and the resource.

The rules are three policies in
[`internal/authz/policies.cedar`](internal/authz/policies.cedar), evaluated in-process by
[Cedar](https://www.cedarpolicy.com/) — see [ADR 0012](docs/adr/0012-role-based-authorization.md).

## Routing visualization

`/admin/routing` draws the path an alert takes — webhook, the routes in the order they are
evaluated with child routes hanging off their parents, then the channels they deliver to — and
answers "which route would this alert take?": paste `key=value` labels and every route that
delivers is named, explained, and the paths are picked out in colour while the rest dims.

Route nodes show the labels they filter for and name the template they render with, marked when it
is inherited. A dashed arrow between two routes is a refinement, labelled *as well as* or *instead
of* depending on whether the child is greedy. A second graph below pairs templates with the routes
that use them; a template with nothing beside it is used by no route. A route pointing at a deleted
destination shows up as a missing node rather than disappearing.

## Container

```bash
make image                       # builds teamster:<version>
```

CI publishes images to `ghcr.io/pflege-de-labs/teamster`:

| Tag | Points at |
| --- | --- |
| `latest` | the newest stable release |
| `1.2.3`, `1.2`, `1` | that release |
| `<short-sha>` | the build of that commit, never moves |
| `main` | the newest build of `main` |
| `pr-<n>` | the newest build of that pull request |

A release rebuilds from its tag, so the release tags share one digest of their own and the binary
inside reports the version rather than a commit sha. Use a `<short-sha>` tag to deploy an exact
CI build.

### Verifying a release

Images are signed with cosign using the release workflow's own identity — there is no public key
to distribute:

```bash
cosign verify \
  --certificate-identity-regexp '^https://github.com/pflege-de-labs/teamster/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/pflege-de-labs/teamster:1.2.3
```

The image carries an SPDX SBOM and SLSA provenance as attestations:

```bash
docker buildx imagetools inspect ghcr.io/pflege-de-labs/teamster:1.2.3 --format '{{ json .SBOM }}'
docker buildx imagetools inspect ghcr.io/pflege-de-labs/teamster:1.2.3 --format '{{ json .Provenance }}'
```

Release binaries come with an SPDX SBOM each and a signed `checksums.txt`, which covers the
binaries and their SBOMs:

```bash
cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp '^https://github.com/pflege-de-labs/teamster/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum -c checksums.txt
```

Run it with the configuration mounted at the system-wide XDG location the binary searches, and
the database on a volume:

```bash
docker run --rm -p 8080:8080 --read-only \
  -v "$PWD/config.yaml:/etc/xdg/teamster/config.yaml:ro" \
  -v teamster-data:/data \
  teamster:latest
```

Connections are bounded by `server.read-timeout`, `server.write-timeout` and
`server.idle-timeout`. If you raise `graph.timeout-sec`, raise the write timeout past it, or a
handler waiting on Microsoft Graph is cut off first.

`--read-only` works because `/data` is the only path the service writes; see
[SECURITY.md](SECURITY.md) for the rest of the deployment expectations.

`make image-run` does exactly that. Settings can also come from `TEAMSTER_*` variables instead of
a mounted file, though a mounted file wins over them.

The image runs as uid 65532 with no shell or package manager, so there is nothing to exec into;
diagnose through the container logs. `/data` is the only writable path, and
`TEAMSTER_DATABASE_PATH` already points the database at it.

## Development

```bash
make hooks          # install the git hooks, once per clone
make generate       # regenerate the templ components and the stylesheet
make test           # go test ./...
make coverage       # coverage report, fails below 75%
make coverage-html  # writes coverage.html
make lint           # golangci-lint
make build          # builds bin/teamster
```

Contributions must meet the definition of done in [AGENTS.md](AGENTS.md): 75% coverage, clean
lint, and current documentation.

The hooks format Go with the same settings CI checks, lint Markdown, and reject commit messages
that are not [Conventional Commits](https://www.conventionalcommits.org). They need
[pre-commit](https://pre-commit.com) on your PATH.

Destinations are configured by picking a Team and a channel by name once Microsoft Graph answers;
the fields fall back to accepting ids typed by hand. Listing requires the `Team.ReadBasic.All` and
`Channel.ReadBasic.All` application permissions with tenant admin consent.

The template form has a Preview button: the server renders the template against a sample alert and
the browser draws the resulting Adaptive Card, so a template can be checked before any alert
arrives. The renderer is vendored in `internal/httpserver/web/vendor`; see the README there to
refresh it.

The admin UI is rendered from [templ](https://github.com/a-h/templ) components in
`internal/httpserver/views`, styled with Tailwind. Both generators run through `make generate`,
and their output is committed, so building or testing the service needs neither of them —
only changing the UI does. `make tools` fetches the pinned Tailwind binary; templ comes from
`go.mod`. `air` runs the generators before each rebuild, so editing a `.templ` file reloads the
running server.

Dependencies are kept current by Renovate, which groups Go and Actions updates and merges the
routine ones itself once CI and branch protection allow it.

## Documentation

- [Architecture](docs/architecture.md)
- [Configuring Keycloak for the admin login](docs/keycloak.md)
- [Roadmap](docs/roadmap.md)
- [Architecture Decision Records](docs/adr/)
- [Contribution rules and definition of done](AGENTS.md)
- [Security policy](SECURITY.md)

## License

[Apache License 2.0](LICENSE).

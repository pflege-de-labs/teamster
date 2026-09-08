# Teamster

Go service that accepts Alertmanager or universal webhooks, routes alerts to Teams channels, and posts Adaptive Cards through Microsoft Graph.

## Quick start

1. Copy config example:

```bash
cp config.example.yaml config.yaml
```

2. Start the server:

```bash
make run                                     # picks up ./config.yaml
go run ./cmd/server serve                    # same thing, command named
go run ./cmd/server -c /etc/teamster.yaml    # or point at any file
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

## Template data

Templates receive:

- `Alert` (normalized alert payload)
- `Now` (RFC3339 string)

Helper functions:

- `toJSON` to JSON-encode structures
- `default` to provide fallbacks

## Notes

- Route selection matches label selectors exactly; highest priority wins.
- A database file written before the `DATETIME` timestamp fix cannot be read. The server refuses
  to start against one and names the file; delete it and restart to recreate the schema.
- A default route is used if no labels match.
- Active alerts are tracked in SQLite to update or resolve cards.

## Sample payloads

See the JSON examples in [samples](samples):

- [samples/alertmanager-firing.json](samples/alertmanager-firing.json)
- [samples/alertmanager-resolved.json](samples/alertmanager-resolved.json)
- [samples/universal-firing.json](samples/universal-firing.json)
- [samples/universal-resolved.json](samples/universal-resolved.json)

## Container

```bash
make image                       # builds teamster:<version>
```

CI publishes images to `ghcr.io/pflege-de/teamster`:

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
  --certificate-identity-regexp '^https://github.com/pflege-de/teamster/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/pflege-de/teamster:1.2.3
```

The image carries an SPDX SBOM and SLSA provenance as attestations:

```bash
docker buildx imagetools inspect ghcr.io/pflege-de/teamster:1.2.3 --format '{{ json .SBOM }}'
docker buildx imagetools inspect ghcr.io/pflege-de/teamster:1.2.3 --format '{{ json .Provenance }}'
```

Release binaries come with an SPDX SBOM each and a signed `checksums.txt`, which covers the
binaries and their SBOMs:

```bash
cosign verify-blob \
  --certificate checksums.txt.pem --signature checksums.txt.sig \
  --certificate-identity-regexp '^https://github.com/pflege-de/teamster/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum -c checksums.txt
```

Run it with the configuration mounted at the system-wide XDG location the binary searches, and
the database on a volume:

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD/config.yaml:/etc/xdg/teamster/config.yaml:ro" \
  -v teamster-data:/data \
  teamster:latest
```

`make image-run` does exactly that. Settings can also come from `TEAMSTER_*` variables instead of
a mounted file, though a mounted file wins over them.

The image runs as uid 65532 with no shell or package manager, so there is nothing to exec into;
diagnose through the container logs. `/data` is the only writable path, and
`TEAMSTER_DATABASE_PATH` already points the database at it.

## Development

```bash
make test           # go test ./...
make coverage       # coverage report, fails below 75%
make coverage-html  # writes coverage.html
make lint           # golangci-lint
make build          # builds bin/teamster
```

Contributions must meet the definition of done in [AGENTS.md](AGENTS.md): 75% coverage, clean
lint, and current documentation.

## Documentation

- [Architecture](docs/architecture.md)
- [Architecture Decision Records](docs/adr/)
- [Contribution rules and definition of done](AGENTS.md)

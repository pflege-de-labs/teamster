# Teamster

Go service that accepts Alertmanager or universal webhooks, routes alerts to Teams channels, and posts Adaptive Cards through Microsoft Graph.

## Quick start

1. Copy config example:

```bash
cp config.example.yaml config.yaml
```

2. Start the server:

```bash
make run                            # picks up ./config.yaml
go run ./cmd/server -c /etc/teamster.yaml   # or point at any file
```

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
- A default route is used if no labels match.
- Active alerts are tracked in SQLite to update or resolve cards.

## Sample payloads

See the JSON examples in [samples](samples):

- [samples/alertmanager-firing.json](samples/alertmanager-firing.json)
- [samples/alertmanager-resolved.json](samples/alertmanager-resolved.json)
- [samples/universal-firing.json](samples/universal-firing.json)
- [samples/universal-resolved.json](samples/universal-resolved.json)

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

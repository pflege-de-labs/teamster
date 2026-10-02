---
title: Container
weight: 2
---

Run Teamster from the published container image, with its configuration mounted from a file and
its database on a volume.

## The image

Images are published to `ghcr.io/pflege-de-labs/teamster` for `linux/amd64` and `linux/arm64`.

| Tag | Points at |
| --- | --- |
| `latest` | the newest stable release |
| `X.Y.Z`, `X.Y` | that release, for example `0.11.0` and `0.11` |
| `<short-sha>` | the build of that commit; never moves |
| `main` | the newest build of `main` |
| `pr-<n>` | the newest build of that pull request |

The release tags of one release share a single digest, and the binary inside reports the release
version. A `main` or pull request image reports `git describe` instead, such as
`v0.10.0-3-gabc1234`: the last release, how many commits past it, and the commit. Deploy a
`<short-sha>` tag when you need an exact build that does not move.

To check a signature, an SBOM or the provenance before you deploy, see
[Verify a release](../../guides/verifying-a-release/).

## Run it

Write a `config.yaml` as in the [quick start]({{< ref "/docs/getting-started/quick-start" >}}),
then mount it at `/etc/xdg/teamster/config.yaml`, the system-wide location the binary searches:

```bash
docker run --rm -p 8080:8080 --read-only \
  -e TEAMSTER_GRAPH_CLIENT_SECRET -e TEAMSTER_BOT_CLIENT_SECRET \
  -v "$PWD/config.yaml:/etc/xdg/teamster/config.yaml:ro" \
  -v teamster-data:/data \
  ghcr.io/pflege-de-labs/teamster:<version>
```

* `/data` is the only path the service writes, so `--read-only` works. `TEAMSTER_DATABASE_PATH`
  already points the SQLite database at `/data/teamster.db`.
* Settings can come from `TEAMSTER_*` environment variables instead of the file. A value in the
  mounted file wins over the variable, so keep secrets out of the file.
* The image runs as uid `65532` and has no shell or package manager. There is nothing to exec
  into: diagnose through the container logs.

Set `server.external-url` (`TEAMSTER_SERVER_EXTERNAL_URL`) to the address people reach the admin
UI at, for example `https://teamster.example.com`. Messages sent without a template link there. It
must be an absolute `http` or `https` URL.

## Health probes

| Path | Answers |
| --- | --- |
| `GET /healthz` | the process is running |
| `GET /readyz` | it can serve: not shutting down, and the database answers |

Both are unauthenticated and answer `HEAD` as well as `GET`.

* Liveness ignores the database. Restarting the process does not bring a database back; it only
  turns an outage into a crash loop.
* Readiness ignores Microsoft Graph. Taking the instance out of rotation when Graph is unreachable
  would close the admin UI exactly when you want to see why delivery fails.
* Readiness fails as soon as a shutdown begins, so a rolling update stops sending traffic before
  the process stops accepting it.

On shutdown the server drains in-flight requests for up to `server.shutdown-timeout` (default
`15s`). A second signal stops it at once.

## Next steps

* [Configure Teamster](../../guides/configuration/) for production settings.
* [Choose and run storage](../../guides/storage/) when you need more than one instance.
* [Install on Kubernetes]({{< ref "/docs/getting-started/kubernetes" >}}) with the Helm chart.

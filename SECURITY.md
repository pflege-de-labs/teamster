# Security policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub, not in a public issue:

**[Report a vulnerability](https://github.com/pflege-de-labs/teamster/security/advisories/new)**
— or the *Security* tab of this repository → *Report a vulnerability*.

Helpful in a report:

* what an attacker can reach or change, and what access they need to start
* the affected version — a release tag, or the commit sha reported by `teamster --version`
* the smallest reproduction you have, ideally the request or configuration that triggers it

Reports are acknowledged as soon as we can pick them up, and we will tell you what we intend to
do about it. Please give us a chance to ship a fix before disclosing publicly.

## Supported versions

Teamster is pre-1.0. Fixes go onto `main` and into the next release; older release tags are not
patched. Run the newest release.

## Handling secrets

The configuration carries a Microsoft Graph client secret, the admin password and the webhook
token. All three are credentials:

* Keep the config file readable only by the service user (`chmod 0600`). Container deployments
  should mount it read-only at `/etc/xdg/teamster/config.yaml`.
* `--help` never prints their values, and neither do the logs.
* Prefer `TEAMSTER_*` environment variables or a mounted file over command line flags; flags are
  visible in the process list to every user on the host.

## Deployment expectations

* The admin UI and the admin API are protected by HTTP basic auth, and the webhook endpoints by a
  shared token in `X-Teamster-Token`. Both send credentials in every request, so **terminate TLS
  in front of Teamster** and never expose it over plain HTTP.
* The admin UI requires a session, obtained by signing in through the configured OIDC provider or
  with the local credentials. The session cookie is a bearer token for the admin UI: it is
  `HttpOnly` and `SameSite=Lax`, and marked `Secure` whenever the request arrives over TLS, which
  is another reason to terminate TLS in front of Teamster.
* The admin form endpoints under `/admin/` additionally require the request to prove it came from
  this origin, via `Sec-Fetch-Site` or a matching `Origin`. A browser attaches basic auth
  credentials to a cross-site post, and there is no session to hold a CSRF token.
* The webhook endpoints are unauthenticated apart from that token. Treat it as a password, give
  each sender its own deployment if you need separate trust boundaries, and rotate it by changing
  the config and restarting.
* The service needs no privileges: the container image runs as uid 65532 and the only path it
  writes is the SQLite database. The base image still ships a world-writable `/tmp`, so run the
  container with a read-only root filesystem and keep `/data` as the one writable mount:

  ```bash
  docker run --read-only -v teamster-data:/data ... ghcr.io/pflege-de-labs/teamster:latest
  ```

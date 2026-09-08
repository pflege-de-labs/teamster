# 0005. Image tags come from metadata-action, releases promote

* Status: Superseded by [0006](0006-release-rebuild-sbom-signing.md)
* Date: 2026-09-08

## Context

ADR 0004 added a container image but CI only proved it compiled; nothing was published, so there
was no way to deploy a specific commit and no tagging scheme at all.

Three properties are wanted. Every build must be addressable by the commit it came from.
Branches and pull requests need a moving tag so a reviewer can pull the change under a
predictable name. And `:latest` must mean "the newest release" — re-running the release of an
older tag, which happens when a build is retried, must not drag `:latest` backwards.

A release must also ship the artifact that was tested. Rebuilding at tag time produces different
bytes from the ones CI checked: dependencies move, base images move, and the build is not
bit-reproducible.

## Decision

Images are published to `ghcr.io/<owner>/<repo>`, which needs no credentials beyond the
`GITHUB_TOKEN` the workflow already has. `docker/metadata-action` generates tags and OCI labels.

CI, on pull requests and pushes to `main`:

* `type=raw` with the short commit sha — the immutable handle on every build. On pull requests
  the head commit is used, not the merge commit, so the tag matches what the author pushed.
* `type=ref,event=branch` and `type=ref,event=pr` — the moving `main` and `pr-<n>` tags.
* `flavor: latest=false`. CI never writes `:latest`.
* Pull requests from forks have no registry credentials, so they build without pushing.

Releases, on a `v*` tag, do not build an image. The `image` job:

1. waits for the CI run of that commit to conclude successfully, so an untested image is never
   promoted;
2. waits for `<image>:<short-sha>` to appear in the registry;
3. asks `metadata-action` for the semver tags (`{{version}}`, `{{major}}.{{minor}}`, `{{major}}`);
4. adds `:latest` only when the tag being released is the highest stable tag in the repository —
   pre-releases and re-releases of older tags are excluded by comparing against
   `git tag --sort=-v:refname`;
5. retags with `docker buildx imagetools create`, then asserts the released tag and the commit tag
   resolve to the same digest.

`imagetools create` copies the manifest index rather than rebuilding, so the multi-arch index
digest is preserved exactly and the release is the same bytes CI tested. The `--annotation` flag
is deliberately not used: annotations rewrite the index and change its digest, which would break
that guarantee. Release-specific metadata therefore lives in the tag and the GitHub release, not
in the image.

The image is built for `linux/amd64` and `linux/arm64`. The build stage runs on
`$BUILDPLATFORM` and cross-compiles via `$TARGETOS`/`$TARGETARCH`, so no QEMU emulation is
involved.

## Consequences

* `ghcr.io/<owner>/<repo>:<short-sha>` addresses any build; `:main` and `:pr-<n>` track branches
  and reviews; `:1.2.3`, `:1.2`, `:1` and `:latest` come from releases.
* Rolling back is retagging or pulling an older sha; the bytes are already in the registry.
* A release tag on a commit that was never pushed to `main` fails, since no image exists for it.
  Release from `main`.
* The release cannot alter image labels. The labels are whatever CI recorded for that commit.
* Pull request builds accumulate `pr-<n>` tags in the registry; they need a retention policy
  eventually.

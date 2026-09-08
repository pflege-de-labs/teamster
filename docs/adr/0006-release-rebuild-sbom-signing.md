# 0006. Releases rebuild, carry an SBOM and are signed

* Status: Accepted
* Date: 2026-09-08

## Context

ADR 0005 had releases promote the image already built from the commit, by retagging it. That kept
the digest identical to the CI build, but it fixed the image metadata at what CI happened to
record: the version baked into the binary was a commit sha, and the OCI labels described a branch
build rather than a release. Adding release metadata during promotion is not possible without
`--annotation`, which rewrites the manifest index and changes its digest — measured, not assumed:
a plain retag preserved `sha256:757ce7b…` while an annotated one produced `sha256:435f0ecc…`.

Separately, the release published no supply-chain evidence. Consumers had no way to tell that an
image or a binary came from this repository, and no inventory of what is inside them.

## Decision

The release builds its own artifacts, and every release tag comes from that single build, so all
of them resolve to one digest.

* A `verify` job runs `golangci-lint` and `make coverage` on the tagged commit, and the image and
  binary jobs depend on it. Rebuilding means the CI run of the commit no longer gates the release,
  so the gate moves into the release itself.
* `VERSION` is the git tag, so `teamster --version` reports `v1.2.3` rather than a commit sha, and
  `metadata-action` labels and annotations describe the release.
* `:latest` still only follows the highest stable tag, unchanged from ADR 0005.

Supply-chain evidence is produced for both artifact kinds:

* The image build passes `sbom: true` and `provenance: mode=max`, so buildx attaches an SPDX SBOM
  and SLSA provenance as in-toto attestations on the index. The workflow asserts both are present.
* The image is signed with cosign **by digest**, which covers every tag pointing at it at once.
* Each released binary gets an SPDX SBOM from syft, generated from the Go build information
  compiled into it.
* `checksums.txt` covers the binaries and their SBOMs, and that one file is signed with
  `cosign sign-blob`, so a single signature authenticates every release asset.

Signing is keyless. The identity is the workflow's OIDC token, so there is no private key to
generate, store or rotate, and `id-token: write` is the only prerequisite.

## Consequences

* A release image is not bit-identical to the `<short-sha>` image built from the same commit.
  The `<short-sha>` tag remains the way to deploy an exact CI build; the semver tags are the
  release.
* Dependencies and the base image are re-resolved at release time, which is why the `verify` job
  exists. A release can fail on code that passed CI earlier — that is the point.
* Keyless signing records the repository, workflow and commit in Sigstore's public transparency
  log. For a repository that must not disclose those, key-based signing with a
  `COSIGN_PRIVATE_KEY` secret is the alternative, at the cost of key custody.
* Verification requires cosign and network access to Sigstore. The commands are in the README.

---
title: Verify a release
weight: 22
---

Check that a Teamster image or binary was built by the project's release workflow and has not been
changed since.

Releases are signed with [cosign](https://docs.sigstore.dev/cosign/) keyless signing. There is no
public key to download. The signing identity is the shared release workflow in
[pflege-de-labs/github-workflows](https://github.com/pflege-de-labs/github-workflows), and the
certificate names `pflege-de-labs/teamster` as the repository that ran it. The commands below check
both. You need `cosign` installed, and Docker with `buildx` to read the attestations.

{{< callout type="info" >}}
The commands accept only release builds: the version tags such as `0.14.0`, `0.14` and `0` on
`ghcr.io/pflege-de-labs/teamster`, and the binaries on the GitHub release. The `main`, `pr-<n>` and
`<short-sha>` images from CI are signed by the CI image workflow, so they fail these checks.
{{< /callout >}}

{{< callout type="warning" >}}
Releases up to and including 0.13.0 were signed by Teamster's own release workflow. For those, use
`--certificate-identity-regexp '^https://github.com/pflege-de-labs/teamster/'` and leave out
`--certificate-github-workflow-repository`.
{{< /callout >}}

## Verify an image

{{% steps %}}

### Check the signature

```bash
cosign verify \
  --certificate-identity-regexp '^https://github\.com/pflege-de-labs/github-workflows/\.github/workflows/image-release\.yml@' \
  --certificate-github-workflow-repository pflege-de-labs/teamster \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/pflege-de-labs/teamster:<version>
```

`<version>` has no `v`, for example `0.14.0`. cosign prints the verified claims, or fails.

### Pin the digest

The output names the digest that was verified. Deploy by that digest,
`ghcr.io/pflege-de-labs/teamster@sha256:<digest>`, so what runs is what you checked.

### Read the SBOM and provenance

The image carries an SPDX SBOM and SLSA provenance as attestations:

```bash
docker buildx imagetools inspect ghcr.io/pflege-de-labs/teamster:<version> --format '{{ json .SBOM }}'
docker buildx imagetools inspect ghcr.io/pflege-de-labs/teamster:<version> --format '{{ json .Provenance }}'
```

{{% /steps %}}

## Verify a binary

Each release on [GitHub](https://github.com/pflege-de-labs/teamster/releases) has binaries for
`linux` and `darwin` on `amd64` and `arm64`, an SPDX SBOM per binary, a `checksums.txt` covering
all of them, and its signature bundle `checksums.txt.bundle`.

{{% steps %}}

### Download

Download the binary you want, for example `teamster-linux-amd64`, with `checksums.txt` and
`checksums.txt.bundle`, into one directory.

### Check the checksum file's signature

```bash
cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp '^https://github\.com/pflege-de-labs/github-workflows/\.github/workflows/go-binaries\.yml@' \
  --certificate-github-workflow-repository pflege-de-labs/teamster \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

### Check the binary against it

```bash
sha256sum --ignore-missing -c checksums.txt
```

`--ignore-missing` skips the assets you did not download. On macOS without GNU coreutils, use
`shasum -a 256 --ignore-missing -c checksums.txt`.

{{% /steps %}}

The binary reports its version with `teamster --version`. See [CLI](../../reference/cli/).

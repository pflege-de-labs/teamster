---
title: Ein Release verifizieren
weight: 22
---

Prüfen Sie, ob ein Image oder Binary von Teamster vom Release-Workflow des Projekts gebaut und
seitdem nicht verändert wurde.

Releases werden mit [cosign](https://docs.sigstore.dev/cosign/) schlüssellos signiert. Es gibt
keinen öffentlichen Schlüssel zum Herunterladen. Die signierende Identität ist der gemeinsame
Release-Workflow in
[pflege-de-labs/github-workflows](https://github.com/pflege-de-labs/github-workflows), und das
Zertifikat nennt `pflege-de-labs/teamster` als das Repository, das ihn ausgeführt hat. Die Befehle
unten prüfen beides. Sie brauchen ein installiertes `cosign` und Docker mit `buildx`, um die
Attestierungen zu lesen.

{{< callout type="info" >}}
Die Befehle akzeptieren nur Release-Builds: die Versions-Tags wie `0.14.0`, `0.14` und `0` auf
`ghcr.io/pflege-de-labs/teamster` und die Binaries im GitHub-Release. Die Images `main`, `pr-<n>`
und `<short-sha>` aus der CI signiert der CI-Image-Workflow, deshalb bestehen sie diese Prüfung
nicht.
{{< /callout >}}

{{< callout type="warning" >}}
Releases bis einschließlich 0.13.0 hat der eigene Release-Workflow von Teamster signiert. Für
diese verwenden Sie `--certificate-identity-regexp '^https://github.com/pflege-de-labs/teamster/'`
und lassen `--certificate-github-workflow-repository` weg.
{{< /callout >}}

## Ein Image verifizieren {#verify-an-image}

{{% steps %}}

### Die Signatur prüfen {#check-the-signature}

```bash
cosign verify \
  --certificate-identity-regexp '^https://github\.com/pflege-de-labs/github-workflows/\.github/workflows/image-release\.yml@' \
  --certificate-github-workflow-repository pflege-de-labs/teamster \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/pflege-de-labs/teamster:<version>
```

`<version>` steht ohne `v`, zum Beispiel `0.14.0`. cosign gibt die verifizierten Claims aus oder
schlägt fehl.

### Den Digest festschreiben {#pin-the-digest}

Die Ausgabe nennt den verifizierten Digest. Deployen Sie über diesen Digest,
`ghcr.io/pflege-de-labs/teamster@sha256:<digest>`, damit läuft, was Sie geprüft haben.

### SBOM und Provenance lesen {#read-the-sbom-and-provenance}

Das Image trägt ein SPDX-SBOM und SLSA-Provenance als Attestierungen:

```bash
docker buildx imagetools inspect ghcr.io/pflege-de-labs/teamster:<version> --format '{{ json .SBOM }}'
docker buildx imagetools inspect ghcr.io/pflege-de-labs/teamster:<version> --format '{{ json .Provenance }}'
```

{{% /steps %}}

## Ein Binary verifizieren {#verify-a-binary}

Jedes Release auf [GitHub](https://github.com/pflege-de-labs/teamster/releases) enthält Binaries
für `linux` und `darwin` auf `amd64` und `arm64`, ein SPDX-SBOM je Binary, eine `checksums.txt`
über alle zusammen und deren Signatur-Bundle `checksums.txt.bundle`.

{{% steps %}}

### Herunterladen {#download}

Laden Sie das gewünschte Binary, zum Beispiel `teamster-linux-amd64`, zusammen mit
`checksums.txt` und `checksums.txt.bundle` in ein Verzeichnis herunter.

### Die Signatur der Prüfsummendatei prüfen {#check-the-checksum-files-signature}

```bash
cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp '^https://github\.com/pflege-de-labs/github-workflows/\.github/workflows/go-binaries\.yml@' \
  --certificate-github-workflow-repository pflege-de-labs/teamster \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

### Das Binary dagegen prüfen {#check-the-binary-against-it}

```bash
sha256sum --ignore-missing -c checksums.txt
```

`--ignore-missing` überspringt die Assets, die Sie nicht heruntergeladen haben. Unter macOS ohne
GNU coreutils verwenden Sie `shasum -a 256 --ignore-missing -c checksums.txt`.

{{% /steps %}}

Das Binary meldet seine Version mit `teamster --version`. Siehe [CLI](../../reference/cli/).

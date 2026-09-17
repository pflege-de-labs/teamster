# Vendored browser libraries

Committed rather than fetched at page load, so the binary stays self-contained.

`manifest.json` is the source of truth for what belongs here: which package and version each file
is, and the SHA-256 it should hash to. `make vendor` re-downloads every library at the version the
manifest pins and refuses to write anything whose checksum does not match what is recorded — a
mismatch means either the version was just bumped and `make vendor-record` has not run yet, or the
package changed under a version that should not have moved. `make vendor-record` is that other
half: it re-downloads, writes the file, and updates the manifest's checksum, which is the part of a
version bump nothing but a person running it can do.

`internal/httpserver/vendor_test.go` fails if a file on disk drifts from its recorded checksum, or
if the directory and the manifest disagree about what should be here, in either direction.

Because the pins live in a JSON file rather than only in prose, Renovate now sees them and opens a
pull request that bumps a version — the checksum still needs `make vendor-record` by hand, which
is why that pull request never merges itself.

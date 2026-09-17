# Vendored browser libraries

Committed rather than fetched at page load, so the binary stays self-contained.

`manifest.json` is the source of truth for what belongs here: the registry to fetch from, and for
each file which package and version it is, where it sits inside that package's tarball, and the
SHA-256 it should hash to.

`make vendor` re-downloads every library at the version the manifest pins. The download is not
simply a file off a CDN: it asks `registry.npmjs.org` for its record of that exact published
version, checks the tarball that record names against the `dist.integrity` it publishes for it —
`sha512` only, because npm's other digest is a legacy `shasum` — and extracts the file only once
that matches. So two separate things have to agree before a byte lands here: the registry's own
integrity for the tarball, and the SHA-256 this manifest records for the file inside it. A
checksum mismatch is reported and nothing is written; it means either the version was just bumped
and `make vendor-record` has not run yet, or the package changed under a version that should not
have moved.

`make vendor-record` is the other half: same download and same integrity check, but it writes the
file and updates the manifest's checksum to match, which is the part of a version bump nothing but
a person running it can do.

`internal/httpserver/vendor_test.go` fails if a file on disk drifts from its recorded checksum, or
if the directory and the manifest disagree about what should be here, in either direction.

Because the pins live in a JSON file rather than only in prose, Renovate now sees them and opens a
pull request that bumps a version — the checksum still needs `make vendor-record` by hand, which
is why that pull request never merges itself.

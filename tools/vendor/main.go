// Command vendor keeps the browser libraries under
// internal/httpserver/web/vendor honest against manifest.json.
//
// The files there are committed rather than fetched at page load, so nothing
// short of a deliberate check proves the bytes on disk are the version the
// manifest claims. Renovate can bump a version number in a regex-matched
// file, but it cannot download a library or compute a checksum, so a bumped
// manifest and a stale file agree with each other right up until something
// runs this tool. "verify" is that check, and doubles as the repair: a
// matching download replaces a corrupted or missing file, a mismatched one is
// reported and left alone. "record" is the half of a version bump a bot
// cannot do — download at the pinned version, write it, and update the
// manifest's checksum to match.
//
// Both modes fetch from the npm registry rather than from a CDN, and check the
// tarball against the integrity the registry publishes for that exact version
// before taking a single byte out of it. A CDN mirrors what the registry has;
// it is not the thing that says what the registry has. Recording a checksum
// from one would make whatever it happened to serve canonical forever.
//
// Go rather than shell and jq: jq is not a dependency of this repository and
// is not on every developer's machine, while Go already is, and
// internal/store/queries/gen is the existing precedent for a small go run
// tool over a shell script.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const vendorDir = "internal/httpserver/web/vendor"

const manifestName = "manifest.json"

// A library is a few hundred kilobytes and its tarball a few megabytes. The
// caps are here because the responses come from a third party and io.ReadAll
// would otherwise believe any length.
const (
	maxLibraryBytes  = 8 << 20
	maxTarballBytes  = 64 << 20
	maxMetadataBytes = 4 << 20
)

// npm packs everything under this prefix, so a manifest path of
// "dist/d3.min.js" is "package/dist/d3.min.js" inside the tarball.
const tarballPrefix = "package"

// Committed files are world readable. os.CreateTemp makes 0600, so without
// this every refresh would quietly narrow the permissions of what it rewrites.
const fileMode = 0o644

const downloadAttempts = 3

// A var so a test can shorten it. Nothing else writes it.
var retryPause = 2 * time.Second

type manifest struct {
	Registry  string    `json:"registry"`
	Libraries []library `json:"libraries"`
}

type library struct {
	File    string `json:"file"`
	Package string `json:"package"`
	Version string `json:"version"`
	License string `json:"license"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func main() {
	mode := "verify"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	var err error
	switch mode {
	case "verify":
		err = verify()
	case "record":
		err = record()
	default:
		err = fmt.Errorf("unknown mode %q, want %q or %q", mode, "verify", "record")
	}
	if err != nil {
		log.Fatalf("vendor: %v", err)
	}
}

// metadataURL is the registry's record of one published version: where its
// tarball is and what it must hash to.
func (l library) metadataURL(registry string) string {
	return fmt.Sprintf("%s/%s/%s", strings.TrimSuffix(registry, "/"), l.Package, l.Version)
}

// versionMetadata is the part of the registry's response this tool reads.
type versionMetadata struct {
	Dist struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"`
	} `json:"dist"`
}

// fetchLibrary returns one library's bytes, having checked the tarball they
// came out of against the integrity the registry publishes for that version.
// Nothing is extracted before that check passes.
func fetchLibrary(registry string, lib library) ([]byte, error) {
	raw, err := download(lib.metadataURL(registry), maxMetadataBytes)
	if err != nil {
		return nil, err
	}

	var meta versionMetadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("%s: %w", lib.metadataURL(registry), err)
	}
	if meta.Dist.Integrity == "" {
		return nil, fmt.Errorf("%s@%s: the registry publishes no integrity for this version", lib.Package, lib.Version)
	}
	// The tarball URL comes out of the response, so it is only as trustworthy
	// as the host that served it -- which must therefore be the same host.
	if err := sameHost(registry, meta.Dist.Tarball); err != nil {
		return nil, fmt.Errorf("%s@%s: %w", lib.Package, lib.Version, err)
	}

	tarball, err := download(meta.Dist.Tarball, maxTarballBytes)
	if err != nil {
		return nil, err
	}
	if err := checkIntegrity(tarball, meta.Dist.Integrity); err != nil {
		return nil, fmt.Errorf("%s: %w", meta.Dist.Tarball, err)
	}
	return extract(tarball, lib.Path)
}

func sameHost(registry, tarball string) error {
	base, err := url.Parse(registry)
	if err != nil {
		return fmt.Errorf("registry %q: %w", registry, err)
	}
	got, err := url.Parse(tarball)
	if err != nil {
		return fmt.Errorf("tarball %q: %w", tarball, err)
	}
	if got.Host != base.Host {
		return fmt.Errorf("tarball %q is not served by %s", tarball, base.Host)
	}
	return nil
}

// checkIntegrity compares a tarball against a Subresource Integrity string.
// Only sha512 is accepted: npm still publishes a sha1 "shasum" beside it, and
// taking that one would be recording a digest nobody should rely on.
func checkIntegrity(tarball []byte, integrity string) error {
	const prefix = "sha512-"
	if !strings.HasPrefix(integrity, prefix) {
		return fmt.Errorf("integrity %q is not %s", integrity, prefix)
	}
	want, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(integrity, prefix))
	if err != nil {
		return fmt.Errorf("integrity %q: %w", integrity, err)
	}

	sum := sha512.Sum512(tarball)
	if !bytes.Equal(sum[:], want) {
		return fmt.Errorf("tarball does not match the published integrity %s", integrity)
	}
	return nil
}

// extract pulls one file out of an npm tarball. It reads rather than writes,
// so a tarball naming a path outside itself has nowhere to escape to.
func extract(tarball []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return nil, fmt.Errorf("tarball: %w", err)
	}
	defer func() { _ = gz.Close() }()

	want := path.Join(tarballPrefix, name)
	archive := tar.NewReader(gz)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("tarball holds no %s", want)
		}
		if err != nil {
			return nil, fmt.Errorf("tarball: %w", err)
		}
		if path.Clean(header.Name) != want {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("%s in the tarball is not a regular file", want)
		}

		body, err := io.ReadAll(io.LimitReader(archive, maxLibraryBytes+1))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", want, err)
		}
		if len(body) > maxLibraryBytes {
			return nil, fmt.Errorf("%s is larger than %d bytes", want, maxLibraryBytes)
		}
		return body, nil
	}
}

// verify downloads each library at its pinned version and compares the
// checksum. A match is written into place, repairing a corrupted or missing
// file; a mismatch is reported and nothing is written for that library. Every
// library is checked before verify reports failure, so one bad pin does not
// hide another.
func verify() error {
	root, m, err := readManifest()
	if err != nil {
		return err
	}

	var failed int
	for _, lib := range m.Libraries {
		body, err := fetchLibrary(m.Registry, lib)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s@%s: %v\n", lib.Package, lib.Version, err)
			failed++
			continue
		}

		actual := checksum(body)
		if actual != lib.SHA256 {
			fmt.Fprintf(os.Stderr,
				"%s@%s: recorded checksum does not describe this version — run `make vendor-record` if the version was just bumped, otherwise the source has changed\n"+
					"  source:   %s\n"+
					"  expected: %s\n"+
					"  actual:   %s\n",
				lib.Package, lib.Version, lib.metadataURL(m.Registry), lib.SHA256, actual)
			failed++
			continue
		}

		if err := writeFileAtomic(filepath.Join(root, vendorDir), lib.File, body); err != nil {
			fmt.Fprintf(os.Stderr, "%s@%s: %v\n", lib.Package, lib.Version, err)
			failed++
			continue
		}
		fmt.Printf("%s@%s: matches %s\n", lib.Package, lib.Version, lib.File)
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d libraries failed verification", failed, len(m.Libraries))
	}
	return nil
}

// record re-downloads every library at its pinned version, writes it, and
// updates the manifest's checksum to match — the step a bumped version number
// alone cannot perform.
//
// Everything is fetched and hashed before anything is written, so a failure on
// the second library cannot leave the first one rewritten on disk with its old
// checksum still in the manifest. That state verifies as broken and reads as
// nobody's fault.
func record() error {
	root, m, err := readManifest()
	if err != nil {
		return err
	}

	bodies := make([][]byte, len(m.Libraries))
	for i, lib := range m.Libraries {
		body, err := fetchLibrary(m.Registry, lib)
		if err != nil {
			return fmt.Errorf("%s@%s: %w", lib.Package, lib.Version, err)
		}
		bodies[i] = body
	}

	changed := false
	for i, lib := range m.Libraries {
		if err := writeFileAtomic(filepath.Join(root, vendorDir), lib.File, bodies[i]); err != nil {
			return fmt.Errorf("%s@%s: %w", lib.Package, lib.Version, err)
		}

		actual := checksum(bodies[i])
		if actual == lib.SHA256 {
			fmt.Printf("%s@%s: sha256 unchanged (%s)\n", lib.Package, lib.Version, actual)
			continue
		}
		fmt.Printf("%s@%s: sha256 %s -> %s\n", lib.Package, lib.Version, lib.SHA256, actual)
		m.Libraries[i].SHA256 = actual
		changed = true
	}

	if !changed {
		return nil
	}
	return writeManifest(root, m)
}

// download retries, because this runs in CI on every pull request and the
// registry having a bad minute must not read as a library that changed under
// us. A status the server will report the same way however often it is
// asked -- a 404 for a version that does not exist -- is returned at once.
func download(rawURL string, max int64) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= downloadAttempts; attempt++ {
		body, err := get(rawURL, max)
		if err == nil {
			return body, nil
		}
		lastErr = err

		var permanent permanentError
		if errors.As(err, &permanent) {
			return nil, err
		}
		if attempt < downloadAttempts {
			time.Sleep(retryPause * time.Duration(attempt))
		}
	}
	return nil, fmt.Errorf("after %d attempts: %w", downloadAttempts, lastErr)
}

// permanentError marks a response that asking again cannot change.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

func get(rawURL string, max int64) ([]byte, error) {
	resp, err := httpClient.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode >= 500, resp.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	default:
		return nil, permanentError{fmt.Errorf("GET %s: %s", rawURL, resp.Status)}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	if int64(len(body)) > max {
		return nil, permanentError{fmt.Errorf("GET %s: larger than %d bytes", rawURL, max)}
	}
	return body, nil
}

func checksum(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// writeFileAtomic writes via a temp file in the same directory so a reader
// never sees a partially written library, then renames it into place.
func writeFileAtomic(dir, name string, body []byte) error {
	tmp, err := os.CreateTemp(dir, ".vendor-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

// readManifest finds the repo root by walking up for go.mod, so the tool works
// from a subdirectory rather than failing with a path nobody typed.
func readManifest() (string, *manifest, error) {
	root, err := repoRoot()
	if err != nil {
		return "", nil, err
	}

	path := filepath.Join(root, vendorDir, manifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}

	m, err := parseManifest(data, path)
	if err != nil {
		return "", nil, err
	}
	return root, m, nil
}

func parseManifest(data []byte, path string) (*manifest, error) {
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// An empty list would otherwise verify successfully and prove nothing,
	// which is the one failure this tool exists to make impossible.
	if len(m.Libraries) == 0 {
		return nil, fmt.Errorf("%s lists no libraries", path)
	}
	for _, lib := range m.Libraries {
		if lib.File == "" || lib.File != filepath.Base(lib.File) {
			return nil, fmt.Errorf("%s: file %q must name a file in %s, with no directory part", path, lib.File, vendorDir)
		}
	}
	return &m, nil
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod in any parent directory: run this from inside the repository")
		}
		dir = parent
	}
}

// Two-space indent and a trailing newline keep the file stable under
// repeated runs, so `make vendor-record` twice in a row produces no diff.
func writeManifest(root string, m *manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(filepath.Join(root, vendorDir), manifestName, data)
}

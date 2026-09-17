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
// Go rather than shell and jq: jq is not a dependency of this repository and
// is not on every developer's machine, while Go already is, and
// internal/store/queries/gen is the existing precedent for a small go run
// tool over a shell script.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Paths are relative to the repo root, since `go run ./tools/vendor` runs
// with the repo root as the working directory.
const (
	vendorDir    = "internal/httpserver/web/vendor"
	manifestPath = "internal/httpserver/web/vendor/manifest.json"
)

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

func (l library) url(registry string) string {
	return fmt.Sprintf("%s/%s@%s/%s", registry, l.Package, l.Version, l.Path)
}

// verify downloads each library at its pinned version and compares the
// checksum. A match is written into place, repairing a corrupted or missing
// file; a mismatch is reported and nothing is written for that library. Every
// library is checked before verify reports failure, so one bad pin does not
// hide another.
func verify() error {
	m, err := readManifest()
	if err != nil {
		return err
	}

	var failed int
	for _, lib := range m.Libraries {
		u := lib.url(m.Registry)
		body, err := download(u)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s@%s: %v\n", lib.Package, lib.Version, err)
			failed++
			continue
		}

		actual := checksum(body)
		if actual != lib.SHA256 {
			fmt.Fprintf(os.Stderr,
				"%s@%s: recorded checksum does not describe this version — run `make vendor-record` if the version was just bumped, otherwise the source has changed\n"+
					"  url:      %s\n"+
					"  expected: %s\n"+
					"  actual:   %s\n",
				lib.Package, lib.Version, u, lib.SHA256, actual)
			failed++
			continue
		}

		if err := writeFileAtomic(vendorDir, lib.File, body); err != nil {
			fmt.Fprintf(os.Stderr, "%s@%s: %v\n", lib.Package, lib.Version, err)
			failed++
			continue
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d libraries failed verification", failed, len(m.Libraries))
	}
	return nil
}

// record re-downloads every library at its pinned version, writes it
// unconditionally, and updates the manifest's checksum to match — the step a
// bumped version number alone cannot perform.
func record() error {
	m, err := readManifest()
	if err != nil {
		return err
	}

	changed := false
	for i, lib := range m.Libraries {
		u := lib.url(m.Registry)
		body, err := download(u)
		if err != nil {
			return fmt.Errorf("%s@%s: %w", lib.Package, lib.Version, err)
		}

		if err := writeFileAtomic(vendorDir, lib.File, body); err != nil {
			return fmt.Errorf("%s@%s: %w", lib.Package, lib.Version, err)
		}

		actual := checksum(body)
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
	return writeManifest(m)
}

func download(rawURL string) ([]byte, error) {
	if _, err := url.Parse(rawURL); err != nil {
		return nil, fmt.Errorf("%s: %w", rawURL, err)
	}

	resp, err := httpClient.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	return io.ReadAll(resp.Body)
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

func readManifest() (*manifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", manifestPath, err)
	}
	return &m, nil
}

// Two-space indent and a trailing newline keep the file stable under
// repeated runs, so `make vendor-record` twice in a row produces no diff.
func writeManifest(m *manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(filepath.Dir(manifestPath), filepath.Base(manifestPath), data)
}

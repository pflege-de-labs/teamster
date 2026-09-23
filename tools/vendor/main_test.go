package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The retry rules decide whether a red CI run means "the library changed" or
// "the registry had a bad minute", so they are worth pinning down.
func TestDownloadRetriesOnlyWhatAskingAgainCouldFix(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		wantAttempts int32
		wantErr      string
	}{
		{name: "a missing version is permanent", status: http.StatusNotFound, wantAttempts: 1, wantErr: "404"},
		{name: "forbidden is permanent", status: http.StatusForbidden, wantAttempts: 1, wantErr: "403"},
		{name: "a server error is retried", status: http.StatusBadGateway, wantAttempts: downloadAttempts, wantErr: "502"},
		{name: "rate limiting is retried", status: http.StatusTooManyRequests, wantAttempts: downloadAttempts, wantErr: "429"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			withFastRetries(t)

			_, err := download(srv.URL+"/lib.js", maxLibraryBytes)
			if err == nil {
				t.Fatal("download() = nil error, want failure")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %s", err, tt.wantErr)
			}
			if got := attempts.Load(); got != tt.wantAttempts {
				t.Errorf("server saw %d requests, want %d", got, tt.wantAttempts)
			}
		})
	}
}

func TestDownloadSucceedsAfterATransientFailure(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("library"))
	}))
	defer srv.Close()

	withFastRetries(t)

	body, err := download(srv.URL+"/lib.js", maxLibraryBytes)
	if err != nil {
		t.Fatalf("download() error = %v", err)
	}
	if string(body) != "library" {
		t.Errorf("body = %q, want the second response", body)
	}
}

// A response that never ends must not be read until the runner runs out of
// memory, so the cap is enforced rather than trusted.
func TestDownloadRefusesAnOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := make([]byte, 1<<16)
		for written := 0; written <= maxLibraryBytes; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	withFastRetries(t)

	_, err := download(srv.URL+"/huge.js", maxLibraryBytes)
	if err == nil {
		t.Fatal("download() = nil error, want the size cap to refuse it")
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("error = %q, want it to name the size cap", err)
	}
	var permanent permanentError
	if !errors.As(err, &permanent) {
		t.Error("an oversized body was retried, but asking again cannot make it smaller")
	}
}

func TestChecksum(t *testing.T) {
	t.Parallel()

	// The empty string's SHA-256, so this pins the encoding as well as the hash.
	const want = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := checksum(nil); got != want {
		t.Errorf("checksum(nil) = %q, want %q", got, want)
	}
}

func TestLibraryMetadataURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		lib  library
		want string
	}{
		{"plain", library{Package: "d3", Version: "7.9.0"}, "https://registry.npmjs.org/d3/7.9.0"},
		// The registry answers a scoped name with its slash left unescaped.
		{"scoped", library{Package: "@codemirror/state", Version: "6.7.6"}, "https://registry.npmjs.org/@codemirror/state/6.7.6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.lib.metadataURL("https://registry.npmjs.org/"); got != tt.want {
				t.Errorf("metadataURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The tool resolves the manifest from the repo root, so it works from a
// subdirectory instead of reporting a path the caller never typed.
func TestRepoRootIsFoundFromASubdirectory(t *testing.T) {
	t.Parallel()

	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot() error = %v", err)
	}
	if root == "" {
		t.Error("repoRoot() = empty")
	}
}

func TestReadManifestRejectsAListItCannotVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "no libraries", body: `{"registry":"https://registry.npmjs.org","libraries":[]}`, wantErr: "lists no libraries"},
		{
			name:    "a file with a directory in it",
			body:    `{"registry":"https://registry.npmjs.org","libraries":[{"file":"../escape.js"}]}`,
			wantErr: "no directory part",
		},
		{
			name:    "a file with no name",
			body:    `{"registry":"https://registry.npmjs.org","libraries":[{"file":""}]}`,
			wantErr: "no directory part",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseManifest([]byte(tt.body), "manifest.json")
			if err == nil {
				t.Fatalf("parseManifest() = nil error, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

// checkIntegrity is the whole trust chain in one function: everything after it
// treats the tarball as the registry's published bytes, so what it accepts is
// what this tool believes.
func TestCheckIntegrity(t *testing.T) {
	t.Parallel()

	body := []byte("a tarball")
	valid := sri(body)

	tests := []struct {
		name      string
		integrity string
		wantErr   string
	}{
		{name: "the published sha512", integrity: valid},
		{
			name:      "a sha512 of something else",
			integrity: sri([]byte("a different tarball")),
			wantErr:   "does not match the published integrity",
		},
		{
			// npm publishes a sha1 "shasum" beside the good digest. Taking it
			// would be recording a digest nobody should rely on.
			name:      "npm's weak sha1 digest",
			integrity: "sha1-" + base64.StdEncoding.EncodeToString([]byte("0123456789abcdefghij")),
			wantErr:   "is not sha512-",
		},
		{
			name:      "a bare digest naming no algorithm",
			integrity: strings.TrimPrefix(valid, "sha512-"),
			wantErr:   "is not sha512-",
		},
		{name: "an empty integrity", integrity: "", wantErr: "is not sha512-"},
		{name: "malformed base64", integrity: "sha512-not!valid!base64", wantErr: "illegal base64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := checkIntegrity(body, tt.integrity)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("checkIntegrity() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("checkIntegrity() = nil error, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestExtract(t *testing.T) {
	t.Parallel()

	const want = "// the library"

	tests := []struct {
		name     string
		entries  []tarEntry
		path     string
		wantBody string
		wantErr  string
	}{
		{
			name: "the named file, out from under npm's package/ prefix",
			entries: []tarEntry{
				{name: "package/package.json", body: "{}"},
				{name: "package/dist/lib.min.js", body: want},
			},
			path:     "dist/lib.min.js",
			wantBody: want,
		},
		{
			name:    "a tarball that does not hold the path",
			entries: []tarEntry{{name: "package/dist/other.js", body: want}},
			path:    "dist/lib.min.js",
			wantErr: "tarball holds no package/dist/lib.min.js",
		},
		{
			name:    "a directory where a file was expected",
			entries: []tarEntry{{name: "package/dist/lib.min.js", typeflag: tar.TypeDir}},
			path:    "dist/lib.min.js",
			wantErr: "is not a regular file",
		},
		{
			name:    "an entry over the size cap",
			entries: []tarEntry{{name: "package/dist/lib.min.js", body: strings.Repeat("a", maxLibraryBytes+1)}},
			path:    "dist/lib.min.js",
			wantErr: "larger than",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body, err := extract(makeTarball(t, tt.entries...), tt.path)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("extract() error = %v", err)
				}
				if string(body) != tt.wantBody {
					t.Errorf("extract() = %q, want %q", body, tt.wantBody)
				}
				return
			}
			if err == nil {
				t.Fatalf("extract() = nil error, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

// The tarball URL is read out of the metadata document, so it is only as
// trustworthy as the host that served it. This is the check that stops a
// compromised metadata document from pointing the download somewhere else.
func TestSameHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tarball string
		wantErr string
	}{
		{name: "the registry's own host", tarball: "https://registry.npmjs.org/d3/-/d3-7.9.0.tgz"},
		{
			name:    "another host entirely",
			tarball: "https://evil.example/d3/-/d3-7.9.0.tgz",
			wantErr: "is not served by registry.npmjs.org",
		},
		{
			name:    "a lookalike subdomain",
			tarball: "https://registry.npmjs.org.evil.example/d3/-/d3-7.9.0.tgz",
			wantErr: "is not served by registry.npmjs.org",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := sameHost("https://registry.npmjs.org", tt.tarball)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("sameHost() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("sameHost() accepted %s: a compromised metadata document could redirect the download", tt.tarball)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestFetchLibrary(t *testing.T) {
	t.Parallel()

	const want = "// the library"
	good := makeTarball(t, tarEntry{name: "package/dist/lib.min.js", body: want})

	tests := []struct {
		name string
		// tarball is what the fake registry serves; integrity is what it
		// publishes for it, which a case is free to make disagree.
		tarball    []byte
		integrity  string
		wantBody   string
		wantErr    string
		wantNotErr string
	}{
		{
			name:      "the published tarball",
			tarball:   good,
			integrity: sri(good),
			wantBody:  want,
		},
		{
			name:      "a tarball that is not the published bytes",
			tarball:   makeTarball(t, tarEntry{name: "package/dist/lib.min.js", body: "// something else"}),
			integrity: sri(good),
			wantErr:   "does not match the published integrity",
		},
		{
			// Not an archive at all. If the error names gzip rather than the
			// integrity, extraction ran before the check that guards it.
			name:       "bytes that never reach the extractor",
			tarball:    []byte("not a tarball"),
			integrity:  sri(good),
			wantErr:    "does not match the published integrity",
			wantNotErr: "gzip",
		},
		{
			name:      "metadata publishing no integrity",
			tarball:   good,
			integrity: "",
			wantErr:   "publishes no integrity",
		},
		{
			name:      "metadata publishing only npm's weak shasum",
			tarball:   good,
			integrity: "sha1-" + base64.StdEncoding.EncodeToString([]byte("0123456789abcdefghij")),
			wantErr:   "is not sha512-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lib := library{File: "lib.min.js", Package: "lib", Version: "1.2.3", Path: "dist/lib.min.js"}
			registry := newFakeRegistry(t, lib, tt.tarball, tt.integrity)

			body, err := fetchLibrary(registry, lib)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("fetchLibrary() error = %v", err)
				}
				if string(body) != tt.wantBody {
					t.Errorf("fetchLibrary() = %q, want %q", body, tt.wantBody)
				}
				return
			}
			if err == nil {
				t.Fatalf("fetchLibrary() = nil error, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
			if tt.wantNotErr != "" && strings.Contains(err.Error(), tt.wantNotErr) {
				t.Errorf("error = %q mentions %q, so the tarball was opened before its integrity was checked", err, tt.wantNotErr)
			}
			if body != nil {
				t.Errorf("fetchLibrary() returned %d bytes alongside its error, want nothing extracted", len(body))
			}
		})
	}
}

// withFastRetries keeps the retrying tests quick without weakening them. It
// writes a package var, which is why its callers are the one group of tests
// here that cannot run in parallel.
func withFastRetries(t *testing.T) {
	t.Helper()

	original := retryPause
	retryPause = time.Millisecond
	t.Cleanup(func() { retryPause = original })
}

// sri renders a Subresource Integrity string the way the npm registry
// publishes one, so a test can hand fetchLibrary a digest it must accept.
func sri(body []byte) string {
	sum := sha512.Sum512(body)
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

type tarEntry struct {
	name     string
	body     string
	typeflag byte // tar.TypeReg when zero
}

func makeTarball(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	archive := tar.NewWriter(gz)
	for _, entry := range entries {
		flag := entry.typeflag
		if flag == 0 {
			flag = tar.TypeReg
		}
		header := &tar.Header{Name: entry.name, Mode: fileMode, Typeflag: flag}
		if flag == tar.TypeReg {
			header.Size = int64(len(entry.body))
		}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatalf("tar header %s: %v", entry.name, err)
		}
		if header.Size > 0 {
			if _, err := io.WriteString(archive, entry.body); err != nil {
				t.Fatalf("tar body %s: %v", entry.name, err)
			}
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	return buf.Bytes()
}

// newFakeRegistry serves one published version the way registry.npmjs.org
// does: a metadata document naming the tarball and its integrity, and the
// tarball itself on the same host. It returns the registry's base URL, so no
// test here reaches the real network.
func newFakeRegistry(t *testing.T, lib library, tarball []byte, integrity string) string {
	t.Helper()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tarballPath := fmt.Sprintf("/%s/-/%s-%s.tgz", lib.Package, lib.Package, lib.Version)
	mux.HandleFunc("/"+lib.Package+"/"+lib.Version, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(versionMetadata{Dist: struct {
			Tarball   string `json:"tarball"`
			Integrity string `json:"integrity"`
		}{Tarball: srv.URL + tarballPath, Integrity: integrity}})
	})
	mux.HandleFunc(tarballPath, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(tarball)
	})
	return srv.URL
}

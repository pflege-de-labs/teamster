package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The retry rules decide whether a red CI run means "the library changed" or
// "the CDN had a bad minute", so they are worth pinning down.
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

			_, err := download(srv.URL + "/lib.js")
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

	body, err := download(srv.URL + "/lib.js")
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

	_, err := download(srv.URL + "/huge.js")
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

func TestLibraryURL(t *testing.T) {
	t.Parallel()

	lib := library{Package: "d3", Version: "7.9.0", Path: "dist/d3.min.js"}
	const want = "https://unpkg.com/d3@7.9.0/dist/d3.min.js"
	if got := lib.url("https://unpkg.com"); got != want {
		t.Errorf("url() = %q, want %q", got, want)
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
		{name: "no libraries", body: `{"registry":"https://unpkg.com","libraries":[]}`, wantErr: "lists no libraries"},
		{
			name:    "a file with a directory in it",
			body:    `{"registry":"https://unpkg.com","libraries":[{"file":"../escape.js"}]}`,
			wantErr: "no directory part",
		},
		{
			name:    "a file with no name",
			body:    `{"registry":"https://unpkg.com","libraries":[{"file":""}]}`,
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

// withFastRetries keeps the retrying tests quick without weakening them. It
// writes a package var, which is why its callers are the one group of tests
// here that cannot run in parallel.
func withFastRetries(t *testing.T) {
	t.Helper()

	original := retryPause
	retryPause = time.Millisecond
	t.Cleanup(func() { retryPause = original })
}

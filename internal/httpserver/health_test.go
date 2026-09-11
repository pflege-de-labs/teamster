package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A probe arrives without credentials, so the endpoints have to answer without
// them — and answer the same way whether or not anyone is signed in.
func probe(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestProbesAnswerWithoutCredentials(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := probe(t, handler, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (%s)", path, rec.Code, rec.Body.String())
		}
		if got := strings.TrimSpace(rec.Body.String()); got != "ok" {
			t.Errorf("GET %s body = %q, want %q", path, got, "ok")
		}
	}
}

// Readiness is about this instance being able to serve, and the store is the
// one dependency a request cannot do without.
func TestReadinessFailsWhenTheStoreIsUnreachable(t *testing.T) {
	t.Parallel()

	st := newFakeStore().fail("Ping")
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := probe(t, handler, http.MethodGet, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /readyz = %d, want 503", rec.Code)
	}

	// Liveness must not follow it down: restarting the process does not bring
	// a database back, it only turns an outage into a crash loop.
	if live := probe(t, handler, http.MethodGet, "/healthz"); live.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200 while only the database is down", live.Code)
	}
}

// Graph is somebody else's service. Taking this instance out of rotation when
// it is unreachable would close the admin UI exactly when an operator wants to
// look at why delivery is failing.
func TestReadinessIgnoresGraph(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{directoryErr: errStore}
	handler := newTestServer(t, newFakeStore(), msg).Handler

	if rec := probe(t, handler, http.MethodGet, "/readyz"); rec.Code != http.StatusOK {
		t.Errorf("GET /readyz = %d, want 200 with only Graph unreachable", rec.Code)
	}
}

// A rolling update should stop traffic arriving before the process stops
// accepting it, which is what readiness failing first is for.
func TestReadinessFailsWhileDraining(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, newFakeStore(), &fakeMessenger{})
	if rec := probe(t, srv.Handler, http.MethodGet, "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("GET /readyz = %d before shutdown, want 200", rec.Code)
	}

	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// net/http runs the shutdown hooks in their own goroutines, so the flag is
	// set just after Shutdown returns rather than during it. That lag is
	// microseconds against a probe interval measured in seconds, but a test
	// that assumed the ordering would be flaky.
	var rec *httptest.ResponseRecorder
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		rec = probe(t, srv.Handler, http.MethodGet, "/readyz")
		if rec.Code == http.StatusServiceUnavailable {
			break
		}
		time.Sleep(time.Millisecond)
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /readyz = %d while shutting down, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "shutting down") {
		t.Errorf("body = %q, want it to say the instance is going away", rec.Body.String())
	}
}

func TestProbesRejectOtherMethods(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := probe(t, handler, http.MethodPost, path)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s = %d, want 405", path, rec.Code)
		}
		if rec.Header().Get("Allow") == "" {
			t.Errorf("POST %s carries no Allow header", path)
		}
	}
}

// HEAD is what some probes send, and it has to work as well as GET does.
func TestProbesAnswerHead(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler

	for _, path := range []string{"/healthz", "/readyz"} {
		if rec := probe(t, handler, http.MethodHead, path); rec.Code != http.StatusOK {
			t.Errorf("HEAD %s = %d, want 200", path, rec.Code)
		}
	}
}

package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/pflege-de-labs/teamster/internal/logging"
)

// requestIDHeader names the id a compact error body refers to, so a report
// from a client can be matched to the log line that has the details.
const requestIDHeader = "X-Request-ID"

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := newRequestID()
		w.Header().Set(requestIDHeader, id)
		logger := s.log.With("request_id", id)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		ctx := context.WithValue(logging.WithLogger(r.Context(), logger), requestIDKey{}, id)
		next.ServeHTTP(rec, r.WithContext(ctx))

		// Probes arrive every few seconds and say nothing on their own.
		level := slog.LevelInfo
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			level = slog.LevelDebug
		}
		logger.Log(r.Context(), level, "request",
			"method", r.Method, "path", loggedPath(r.URL.Path),
			"status", rec.status, "duration_ms", time.Since(start).Milliseconds())
	})
}

// loggedPath drops the Teams V2 token, which authenticates the sender and
// would otherwise sit in every log that ships this line.
func loggedPath(path string) string {
	if !strings.HasPrefix(path, "/teamsv2/") {
		return path
	}
	if i := strings.LastIndex(path, "/"); i > len("/teamsv2") {
		return path[:i+1] + "…"
	}
	return path
}

type requestIDKey struct{}

// requestID is the id the logging middleware gave this request, or empty.
func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// statusRecorder remembers the status a handler answered with.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the writer's own Flush and deadlines.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

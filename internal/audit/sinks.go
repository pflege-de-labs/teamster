package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// FileSink appends one JSON object per line.
type FileSink struct {
	mu     sync.Mutex
	w      io.Writer
	closer io.Closer
}

// OpenFile opens path for appending, or stdout for "-". O_APPEND keeps lines
// whole under logrotate's copytruncate.
func OpenFile(path string) (*FileSink, error) {
	if path == "-" {
		return &FileSink{w: os.Stdout}, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit file: %w", err)
	}
	return &FileSink{w: f, closer: f}, nil
}

func (s *FileSink) Name() string { return "file" }

func (s *FileSink) Write(_ context.Context, e models.AuditEvent) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return os.ErrClosed
	}
	_, err = s.w.Write(line)
	return err
}

func (s *FileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.w = nil
	if s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

// Store is the slice of store.Store the audit trail is kept in.
type Store interface {
	InsertAuditEvent(ctx context.Context, e models.AuditEvent) error
	PruneAuditEvents(ctx context.Context, cutoff time.Time, keep int) (int64, error)
}

// DBSink writes the events the admin UI lists.
type DBSink struct {
	store Store
}

func NewDBSink(st Store) *DBSink { return &DBSink{store: st} }

func (s *DBSink) Name() string { return "database" }

func (s *DBSink) Write(ctx context.Context, e models.AuditEvent) error {
	return s.store.InsertAuditEvent(ctx, e)
}

func (s *DBSink) Close() error { return nil }

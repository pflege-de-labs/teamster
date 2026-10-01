package cli

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
)

func TestNewRecorder(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	nats := config.AuditNATSConfig{URL: "nats://127.0.0.1:1", SubjectPrefix: "teamster.audit", Stream: "A", CreateStream: true, Timeout: 50 * time.Millisecond}
	tests := []struct {
		name    string
		cfg     config.AuditConfig
		wantErr bool
		relays  int
	}{
		{name: "nothing configured", cfg: config.AuditConfig{}},
		{name: "database and file", cfg: config.AuditConfig{Database: true, File: filepath.Join(t.TempDir(), "a.jsonl"), QueueSize: 1}},
		{name: "an unreachable NATS server does not stop the start", cfg: config.AuditConfig{NATS: nats, QueueSize: 1}},
		{name: "NATS fed from the trail is a relay", cfg: func() config.AuditConfig {
			c := config.AuditConfig{Database: true, NATS: nats, QueueSize: 1}
			c.NATS.Backfill, c.NATS.BackfillInterval = true, time.Second
			return c
		}(), relays: 1},
		{name: "a file that cannot be opened does", cfg: config.AuditConfig{File: filepath.Join(t.TempDir(), "missing", "a.jsonl"), QueueSize: 1}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec, relays, err := newRecorder(t.Context(), logger, tt.cfg, nil, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("newRecorder() = %v, want error %v", err, tt.wantErr)
			}
			if (rec == nil) != (tt.wantErr || tt.cfg == config.AuditConfig{}) {
				t.Errorf("newRecorder() = %v, want nil exactly when nothing is configured", rec)
			}
			if len(relays) != tt.relays {
				t.Errorf("%d relays, want %d", len(relays), tt.relays)
			}
			for _, relay := range relays {
				_ = relay.Close()
			}
			if err == nil {
				if err := rec.Close(context.Background()); err != nil {
					t.Errorf("Close: %v", err)
				}
			}
		})
	}
}

package audit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/pflege-de-labs/teamster/internal/models"
)

type fakePublisher struct {
	mu       sync.Mutex
	failures int
	calls    []string
	ids      []string
	payloads [][]byte
}

func (p *fakePublisher) Publish(_ context.Context, subject string, payload []byte, msgID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, subject)
	p.ids = append(p.ids, msgID)
	p.payloads = append(p.payloads, payload)
	if p.failures > 0 {
		p.failures--
		return errors.New("no responders")
	}
	return nil
}

func TestNATSSinkSubjects(t *testing.T) {
	t.Parallel()

	sink := &NATSSink{prefix: "teamster.audit"}
	tests := []struct {
		name string
		e    models.AuditEvent
		want string
	}{
		{"type and action levels", models.AuditEvent{ResourceType: "Template", Action: "template.update"}, "teamster.audit.Template.template.update"},
		{"wildcards are escaped", models.AuditEvent{ResourceType: "A*B", Action: "x>y.z w"}, "teamster.audit.A_B.x_y.z_w"},
		{"empty levels stay levels", models.AuditEvent{Action: "a..b"}, "teamster.audit._.a._.b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sink.Subject(tt.e); got != tt.want {
				t.Errorf("Subject() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNATSSinkWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		failures  int
		wantErr   bool
		wantCalls int
	}{
		{"first try", 0, false, 1},
		{"retried until acknowledged", 2, false, 3},
		{"gives up after the attempts", publishAttempts, true, publishAttempts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pub := &fakePublisher{failures: tt.failures}
			sink := &NATSSink{js: pub, prefix: "p", backoff: time.Millisecond}
			e := models.AuditEvent{ID: "ev-1", ResourceType: "Route", Action: "route.delete"}

			err := sink.Write(t.Context(), e)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Write() = %v, want error %v", err, tt.wantErr)
			}
			if len(pub.calls) != tt.wantCalls {
				t.Errorf("published %d times, want %d", len(pub.calls), tt.wantCalls)
			}
			for i, id := range pub.ids {
				if id != "ev-1" {
					t.Errorf("attempt %d carried message id %q, want the event id", i, id)
				}
			}
			var got models.AuditEvent
			if err := json.Unmarshal(pub.payloads[0], &got); err != nil || got.ID != "ev-1" {
				t.Errorf("payload %s, %v", pub.payloads[0], err)
			}
		})
	}
}

func TestNATSSinkStopsRetryingWhenCancelled(t *testing.T) {
	t.Parallel()

	pub := &fakePublisher{failures: publishAttempts}
	sink := &NATSSink{js: pub, prefix: "p", backoff: time.Hour}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := sink.Write(ctx, models.AuditEvent{ID: "x"}); err == nil || len(pub.calls) != 1 {
		t.Errorf("Write() = %v after %d calls, want one failed attempt", err, len(pub.calls))
	}
	if sink.Name() != "nats" || sink.Close() != nil {
		t.Error("Name or Close")
	}
}

// TestNATSSinkAgainstJetStream needs a server with JetStream:
// TEAMSTER_TEST_NATS_URL=nats://127.0.0.1:14222 (see make nats-up).
func TestNATSSinkAgainstJetStream(t *testing.T) {
	t.Parallel()

	url := os.Getenv("TEAMSTER_TEST_NATS_URL")
	if url == "" {
		// In CI a skip would quietly leave the sink untested.
		if os.Getenv("CI") != "" {
			t.Fatal("TEAMSTER_TEST_NATS_URL is unset in CI: the NATS sink would go untested")
		}
		t.Skip("TEAMSTER_TEST_NATS_URL is not set")
	}
	stream := "AUDIT_" + strings.ReplaceAll(t.Name(), "/", "_")
	prefix := "test." + strings.ToLower(stream)

	sink, err := OpenNATS(t.Context(), discard(), NATSOptions{
		URL: url, SubjectPrefix: prefix, Stream: stream, CreateStream: true, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("OpenNATS: %v", err)
	}
	defer func() { _ = sink.Close() }()

	e := models.AuditEvent{ID: "ev-" + stream, ResourceType: "Template", Action: "template.create"}
	for range 2 {
		if err := sink.Write(t.Context(), e); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = js.DeleteStream(context.Background(), stream) }()
	info, err := js.Stream(t.Context(), stream)
	if err != nil {
		t.Fatal(err)
	}
	state, err := info.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state.State.Msgs != 1 {
		t.Errorf("stream holds %d messages, want the duplicate dropped", state.State.Msgs)
	}
	msg, err := info.GetLastMsgForSubject(t.Context(), prefix+".Template.template.create")
	if err != nil {
		t.Fatalf("no message on the event's subject: %v", err)
	}
	var got models.AuditEvent
	if err := json.Unmarshal(msg.Data, &got); err != nil || got.ID != e.ID {
		t.Errorf("message %s, %v", msg.Data, err)
	}
}

package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// NATSOptions is where the JetStream sink publishes (ADR 0071).
type NATSOptions struct {
	URL           string
	SubjectPrefix string
	// Stream, with CreateStream, is created or updated to capture the prefix.
	Stream       string
	CreateStream bool
	CredsFile    string
	Timeout      time.Duration
}

// publisher is what the sink needs of JetStream: an acknowledged, deduplicated publish.
type publisher interface {
	Publish(ctx context.Context, subject string, payload []byte, msgID string) error
}

type jetStreamPublisher struct{ js jetstream.JetStream }

func (p jetStreamPublisher) Publish(ctx context.Context, subject string, payload []byte, msgID string) error {
	_, err := p.js.Publish(ctx, subject, payload, jetstream.WithMsgID(msgID))
	return err
}

// NATSSink publishes each event once JetStream acknowledges it; the event id
// is the message id, so a retried publish is deduplicated by the stream.
type NATSSink struct {
	js      publisher
	close   func()
	prefix  string
	backoff time.Duration
}

// publishAttempts bounds retries within one write; the recorder's timeout bounds the time.
const publishAttempts = 3

// OpenNATS connects without waiting for the server, so an outage at start
// delays audit events rather than the service.
func OpenNATS(ctx context.Context, logger *slog.Logger, opts NATSOptions) (*NATSSink, error) {
	connectOpts := []nats.Option{
		nats.Name("teamster-audit"),
		nats.Timeout(opts.Timeout),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				logger.Warn("audit nats disconnected", "err", err)
			}
		}),
		nats.ReconnectHandler(func(*nats.Conn) { logger.Info("audit nats reconnected") }),
	}
	if opts.CredsFile != "" {
		connectOpts = append(connectOpts, nats.UserCredentials(opts.CredsFile))
	}
	conn, err := nats.Connect(opts.URL, connectOpts...)
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("jetstream: %w", err)
	}

	if opts.CreateStream {
		streamCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
		_, err := js.CreateOrUpdateStream(streamCtx, jetstream.StreamConfig{
			Name:     opts.Stream,
			Subjects: []string{opts.SubjectPrefix + ".>"},
		})
		cancel()
		// Not fatal: the server may be down now and the stream made by hand later.
		if err != nil {
			logger.Warn("create audit stream", "stream", opts.Stream, "err", err)
		}
	}

	return &NATSSink{js: jetStreamPublisher{js}, close: conn.Close, prefix: opts.SubjectPrefix, backoff: 100 * time.Millisecond}, nil
}

func (s *NATSSink) Name() string { return "nats" }

// Subject is "<prefix>.<type>.<action>", so a consumer can filter by either.
func (s *NATSSink) Subject(e models.AuditEvent) string {
	return s.prefix + "." + subjectToken(e.ResourceType) + "." + subjectTokens(e.Action)
}

func (s *NATSSink) Write(ctx context.Context, e models.AuditEvent) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	subject := s.Subject(e)
	wait := s.backoff
	for attempt := 1; ; attempt++ {
		err = s.js.Publish(ctx, subject, payload, e.ID)
		if err == nil || attempt == publishAttempts {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(wait):
		}
		wait *= 2
	}
}

func (s *NATSSink) Close() error {
	if s.close != nil {
		s.close()
	}
	return nil
}

// subjectTokens keeps the dots of an action as subject levels.
func subjectTokens(value string) string {
	parts := strings.Split(value, ".")
	for i, part := range parts {
		parts[i] = subjectToken(part)
	}
	return strings.Join(parts, ".")
}

// subjectToken replaces what NATS would read as a separator or wildcard.
func subjectToken(value string) string {
	if value == "" {
		return "_"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '.', '*', '>', ' ', '\t', '\r', '\n':
			return '_'
		}
		return r
	}, value)
}

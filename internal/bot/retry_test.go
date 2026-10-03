package bot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

// answer is one canned Bot Connector reply.
type answer struct {
	status     int
	retryAfter string
}

// recordingPacer lets every call through and remembers what it was told.
type recordingPacer struct {
	mu     sync.Mutex
	keys   []string
	pauses []time.Duration
	err    error
}

func (p *recordingPacer) Wait(_ context.Context, key string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys = append(p.keys, key)
	return p.err
}

func (p *recordingPacer) Throttled(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pauses = append(p.pauses, d)
}

// countingInstrumentation counts what the client reports.
type countingInstrumentation struct {
	uninstrumented
	mu        sync.Mutex
	throttled int
	waited    int
}

func (c *countingInstrumentation) Throttled(context.Context, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.throttled++
}

func (c *countingInstrumentation) PacingWaited(context.Context, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waited++
}

func TestSendMessageRetriesThrottledCalls(t *testing.T) {
	t.Parallel()

	ok := answer{status: http.StatusOK}
	tests := []struct {
		name    string
		retries int
		answers []answer
		// wantCalls is how many requests reached the Connector.
		wantCalls  int
		wantWaits  []time.Duration
		wantPauses []time.Duration
		wantStatus int
	}{
		{
			name:    "a 429 is retried after its Retry-After, and pauses the pacer",
			retries: 3, answers: []answer{{status: 429, retryAfter: "2"}, ok},
			wantCalls: 2, wantWaits: []time.Duration{2 * time.Second}, wantPauses: []time.Duration{2 * time.Second},
		},
		{
			name:    "a 503 is retried without pausing everyone",
			retries: 3, answers: []answer{{status: 503}, ok},
			wantCalls: 2, wantWaits: []time.Duration{time.Second},
		},
		{
			name:    "without Retry-After the wait doubles",
			retries: 2, answers: []answer{{status: 429}, {status: 429}, {status: 429}},
			wantCalls: 3, wantWaits: []time.Duration{time.Second, 2 * time.Second},
			wantPauses: []time.Duration{time.Second, 2 * time.Second}, wantStatus: 429,
		},
		{
			name:    "a long Retry-After is capped",
			retries: 1, answers: []answer{{status: 429, retryAfter: "3600"}, ok},
			wantCalls: 2, wantWaits: []time.Duration{30 * time.Second}, wantPauses: []time.Duration{30 * time.Second},
		},
		{
			name:    "no retries fails at once",
			retries: 0, answers: []answer{{status: 429, retryAfter: "1"}},
			wantCalls: 1, wantStatus: 429,
		},
		{
			// A 502 may have been delivered; sending it again could post twice.
			name:    "other failures are not retried",
			retries: 3, answers: []answer{{status: 502}},
			wantCalls: 1, wantStatus: 502,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var mu sync.Mutex
			calls := 0
			client, ref := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				a := tt.answers[min(calls, len(tt.answers)-1)]
				calls++
				mu.Unlock()
				if a.retryAfter != "" {
					w.Header().Set("Retry-After", a.retryAfter)
				}
				w.WriteHeader(a.status)
				_, _ = w.Write([]byte(`{"id":"activity-1"}`))
			})
			pacer := &recordingPacer{}
			tel := &countingInstrumentation{}
			var waits []time.Duration
			client.pacer, client.tel, client.retries, client.maxRetryWait = pacer, tel, tt.retries, 30*time.Second
			client.sleep = func(_ context.Context, d time.Duration) error {
				waits = append(waits, d)
				return nil
			}

			_, err := client.SendMessage(context.Background(), ref, Message{Text: "hi"})

			var apiErr *APIError
			switch {
			case tt.wantStatus == 0 && err != nil:
				t.Fatalf("SendMessage() = %v, want success", err)
			case tt.wantStatus != 0 && (!errors.As(err, &apiErr) || apiErr.StatusCode != tt.wantStatus):
				t.Fatalf("SendMessage() = %v, want an APIError with status %d", err, tt.wantStatus)
			}
			if calls != tt.wantCalls {
				t.Errorf("requests = %d, want %d", calls, tt.wantCalls)
			}
			if !reflect.DeepEqual(waits, tt.wantWaits) {
				t.Errorf("waits = %v, want %v", waits, tt.wantWaits)
			}
			if !reflect.DeepEqual(pacer.pauses, tt.wantPauses) {
				t.Errorf("pacer pauses = %v, want %v", pacer.pauses, tt.wantPauses)
			}
			if len(pacer.keys) != tt.wantCalls || pacer.keys[0] != ref.ConversationID {
				t.Errorf("paced keys = %v, want %d of %q", pacer.keys, tt.wantCalls, ref.ConversationID)
			}
			if tel.waited != tt.wantCalls {
				t.Errorf("pacing waits recorded = %d, want %d", tel.waited, tt.wantCalls)
			}
			wantThrottled := tt.wantCalls
			if tt.answers[len(tt.answers)-1] == ok || tt.wantStatus == 502 {
				wantThrottled--
			}
			if tel.throttled != wantThrottled {
				t.Errorf("throttled calls counted = %d, want %d", tel.throttled, wantThrottled)
			}
		})
	}
}

func TestDoRequestStopsWaiting(t *testing.T) {
	t.Parallel()

	paceErr := errors.New("budget gone")
	tests := []struct {
		name      string
		pacerErr  error
		sleepErr  error
		wantErr   error
		wantCalls int
	}{
		{name: "the pacer gives up before anything is sent", pacerErr: paceErr, wantErr: paceErr},
		{name: "the caller gives up between attempts", sleepErr: context.Canceled, wantErr: context.Canceled, wantCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(http.StatusTooManyRequests)
			}))
			t.Cleanup(srv.Close)
			client := &Client{
				httpClient: srv.Client(), pacer: &recordingPacer{err: tt.pacerErr}, retries: 3,
				sleep: func(context.Context, time.Duration) error { return tt.sleepErr },
			}

			_, err := client.SendMessage(context.Background(), ConversationReference{ServiceURL: srv.URL, ConversationID: "c"}, Message{Text: "hi"})
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("SendMessage() = %v, want %v", err, tt.wantErr)
			}
			if calls != tt.wantCalls {
				t.Errorf("requests = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestCallsArePacedOnTheirConversation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(*Client, string) error
		want string
	}{
		{
			name: "update",
			call: func(c *Client, url string) error {
				return c.UpdateMessage(context.Background(), ConversationReference{ServiceURL: url, ConversationID: "convo"}, "a1", Message{Text: "hi"})
			},
			want: "convo",
		},
		{
			name: "channel post",
			call: func(c *Client, url string) error {
				_, err := c.PostToChannel(context.Background(), url, "tenant", "channel-1", Message{Text: "hi"})
				return err
			},
			want: "channel-1",
		},
		{
			name: "personal conversation",
			call: func(c *Client, url string) error {
				_, err := c.CreatePersonalConversation(context.Background(), url, "tenant", "bot", "oid-1")
				return err
			},
			want: "oid-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, ref := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"id":"x","activityId":"y"}`))
			})
			pacer := &recordingPacer{}
			client.pacer = pacer

			if err := tt.call(client, ref.ServiceURL); err != nil {
				t.Fatalf("call = %v", err)
			}
			if !reflect.DeepEqual(pacer.keys, []string{tt.want}) {
				t.Errorf("paced keys = %v, want [%s]", pacer.keys, tt.want)
			}
		})
	}
}

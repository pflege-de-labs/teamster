package httpserver

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// These are the tests the claim protocol exists for. Every one of them fails
// against the read-then-post-then-write it replaced, and none of them depends
// on the scheduler: the fake messenger holds the winner inside its Graph call
// until the test has watched the losers come back, so the interleaving is
// arranged rather than hoped for.

// TestConcurrentFiringPostsOneCard is the bug in one test. Several requests for
// the same alert arrive at once, all of them find no card, and exactly one is
// allowed to create it.
func TestConcurrentFiringPostsOneCard(t *testing.T) {
	t.Parallel()

	const callers = 4
	msg := &fakeMessenger{
		postGate: make(chan struct{}),
		posting:  make(chan struct{}, callers),
	}
	_, handler := seededServer(t, msg)

	done := make(chan int, callers)
	for range callers {
		go func() {
			rec := postWebhook(t, handler, "/webhook/universal", "token",
				`{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
			done <- rec.Code
		}()
	}

	// The winner is held inside PostMessage, so every caller that finishes
	// before the gate opens is one that was refused. Waiting for exactly those
	// rather than sleeping is what keeps this from depending on the scheduler.
	codes := make([]int, 0, callers)
	for range callers - 1 {
		select {
		case code := <-done:
			codes = append(codes, code)
		case <-time.After(5 * time.Second):
			t.Fatal("callers are still waiting: more than one reached the Graph call")
		}
	}

	close(msg.postGate)
	select {
	case code := <-done:
		codes = append(codes, code)
	case <-time.After(5 * time.Second):
		t.Fatal("the caller that posted never finished")
	}

	msg.mu.Lock()
	posts := len(msg.posts)
	msg.mu.Unlock()
	if posts != 1 {
		t.Errorf("posted %d cards, want exactly 1", posts)
	}

	var ok, refused int
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusBadGateway:
			refused++
		default:
			t.Errorf("unexpected status %d", code)
		}
	}
	if ok != 1 || refused != callers-1 {
		t.Errorf("got %d accepted and %d refused, want 1 and %d", ok, refused, callers-1)
	}
}

// A loser's retry has to land on the card the winner made, not make another.
func TestRetryAfterAnInFlightCardUpdatesIt(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	_, handler := seededServer(t, msg)

	first := postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first POST = %d, want 200", first.Code)
	}
	second := postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
	if second.Code != http.StatusOK {
		t.Fatalf("second POST = %d, want 200", second.Code)
	}

	msg.mu.Lock()
	defer msg.mu.Unlock()
	if len(msg.posts) != 1 {
		t.Errorf("posted %d cards, want 1", len(msg.posts))
	}
	if len(msg.updates) != 1 {
		t.Errorf("updates = %d, want the second alert to edit the first card", len(msg.updates))
	}
}

// A failed post must not leave a claim behind: the next attempt should be able
// to try immediately rather than wait out the staleness cutoff.
func TestClaimIsReleasedWhenTheGraphPostFails(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{postErr: errors.New("graph is down")}
	st, handler := seededServer(t, msg)

	if rec := postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{},"fingerprint":"fp-1"}`); rec.Code != http.StatusBadGateway {
		t.Fatalf("POST = %d, want 502", rec.Code)
	}

	st.mu.Lock()
	remaining := len(st.activeAlerts)
	st.mu.Unlock()
	if remaining != 0 {
		t.Errorf("%d rows left behind, want the claim released", remaining)
	}

	msg.mu.Lock()
	msg.postErr = nil
	msg.mu.Unlock()
	if rec := postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{},"fingerprint":"fp-1"}`); rec.Code != http.StatusOK {
		t.Errorf("retry = %d, want 200 without waiting out the cutoff", rec.Code)
	}
}

// A claim whose owner died is taken over once it is old enough — and not one
// second before, which is the half that keeps two instances from both posting.
func TestStaleClaimIsRecoveredAndFreshOneIsNot(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		age      time.Duration
		wantPost int
		wantCode int
	}{
		{name: "a claim younger than the cutoff is left alone", age: time.Second, wantPost: 0, wantCode: http.StatusBadGateway},
		{name: "a claim older than the cutoff is taken over", age: time.Hour, wantPost: 1, wantCode: http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &fakeMessenger{}
			st, handler := seededServer(t, msg)

			// A claim somebody else took, with no card behind it.
			abandoned := time.Now().UTC().Add(-tt.age)
			st.activeAlerts[activeAlertKey("fp-1", "team", "channel")] = models.ActiveAlert{
				Fingerprint: "fp-1",
				Status:      "firing",
				TeamID:      "team",
				ChannelID:   "channel",
				ClaimOwner:  "somebody-else",
				ClaimedAt:   abandoned,
				LastUpdate:  abandoned,
			}

			rec := postWebhook(t, handler, "/webhook/universal", "token",
				`{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
			if rec.Code != tt.wantCode {
				t.Errorf("POST = %d, want %d", rec.Code, tt.wantCode)
			}

			msg.mu.Lock()
			defer msg.mu.Unlock()
			if len(msg.posts) != tt.wantPost {
				t.Errorf("posted %d cards, want %d", len(msg.posts), tt.wantPost)
			}
		})
	}
}

// Resolving must not touch a claim that has no card yet: editing is impossible
// and deleting would strand the message the other instance is about to post.
func TestResolveLeavesAnInFlightClaimAlone(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)

	claimed := time.Now().UTC()
	st.activeAlerts[activeAlertKey("fp-1", "team", "channel")] = models.ActiveAlert{
		Fingerprint: "fp-1",
		Status:      "firing",
		TeamID:      "team",
		ChannelID:   "channel",
		ClaimOwner:  "somebody-else",
		ClaimedAt:   claimed,
		LastUpdate:  claimed,
	}

	rec := postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"resolved","labels":{},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("POST = %d, want 502 so the sender retries", rec.Code)
	}

	msg.mu.Lock()
	updates := len(msg.updates)
	msg.mu.Unlock()
	if updates != 0 {
		t.Errorf("updated %d messages, want none: there is no card yet", updates)
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if _, found := st.activeAlerts[activeAlertKey("fp-1", "team", "channel")]; !found {
		t.Error("the claim was deleted, which would strand the card being posted")
	}
}

// The residual failure this design cannot close, pinned down so nobody assumes
// it away: the post succeeded, and by the time it was recorded the row had been
// taken. Graph has no idempotency key for a channel message, so the card cannot
// be adopted — only reported.
func TestACompletedClaimThatWasTakenIsReported(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)
	st.fail("CompleteActiveAlertClaim")

	rec := postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("POST = %d, want the failure surfaced rather than swallowed", rec.Code)
	}

	msg.mu.Lock()
	defer msg.mu.Unlock()
	if len(msg.posts) != 1 {
		t.Errorf("posted %d cards, want the one that is now orphaned", len(msg.posts))
	}
}

// Two channels are two cards, and claiming one must not block the other.
func TestFanOutClaimsEachChannelSeparately(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)
	st.destinations["other"] = models.Destination{ID: "other", TeamID: "team", ChannelID: "other-channel"}
	st.routes["second"] = models.Route{ID: "second", TemplateID: "tmpl", DestinationID: "other", IsDefault: false}

	if rec := postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{},"fingerprint":"fp-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200", rec.Code)
	}

	cards, err := st.ListActiveAlerts(t.Context(), "fp-1")
	if err != nil {
		t.Fatalf("ListActiveAlerts: %v", err)
	}
	for _, card := range cards {
		if !card.Posted() {
			t.Errorf("card %+v was left claimed rather than posted", card)
		}
	}
}

// ErrClaimLost has to travel by errors.Is, since the retry classification a
// networked backend needs cannot match on strings.
func TestClaimLostIsComparable(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	claim := models.AlertClaim{Fingerprint: "fp", TeamID: "team", ChannelID: "channel", Owner: "nobody"}

	err := st.CompleteActiveAlertClaim(t.Context(), claim, "message-1", time.Now())
	if !errors.Is(err, store.ErrClaimLost) {
		t.Errorf("completing a claim nobody holds = %v, want ErrClaimLost", err)
	}
}

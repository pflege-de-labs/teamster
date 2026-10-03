package bot

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
)

func TestProcessPacerWait(t *testing.T) {
	t.Parallel()

	// Rates this slow never refill within a test, so the burst is all there is.
	tests := []struct {
		name string
		cfg  config.PacingConfig
		// keys are waited for in order; the last must be held back.
		keys    []string
		wantErr bool
	}{
		{
			name: "within both bursts",
			cfg:  config.PacingConfig{Rate: 0.001, Burst: 2, ConversationRate: 0.001, ConversationBurst: 2},
			keys: []string{"a", "a"},
		},
		{
			name:    "a conversation's burst spent",
			cfg:     config.PacingConfig{Rate: 1000, Burst: 10, ConversationRate: 0.001, ConversationBurst: 1},
			keys:    []string{"a", "a"},
			wantErr: true,
		},
		{
			name: "another conversation has its own",
			cfg:  config.PacingConfig{Rate: 1000, Burst: 10, ConversationRate: 0.001, ConversationBurst: 1},
			keys: []string{"a", "b"},
		},
		{
			name:    "the global burst spent",
			cfg:     config.PacingConfig{Rate: 0.001, Burst: 1, ConversationRate: 1000, ConversationBurst: 10},
			keys:    []string{"a", "b"},
			wantErr: true,
		},
		{
			name:    "no key is paced globally",
			cfg:     config.PacingConfig{Rate: 0.001, Burst: 1, ConversationRate: 1000, ConversationBurst: 10},
			keys:    []string{"", ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := NewPacer(tt.cfg)
			var err error
			for _, key := range tt.keys {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				err = p.Wait(ctx, key)
				cancel()
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("last Wait() = %v, want error %t", err, tt.wantErr)
			}
		})
	}
}

func TestProcessPacerThrottledPausesEveryone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pauses  []time.Duration
		timeout time.Duration
		wantErr bool
	}{
		{name: "no pause", timeout: 20 * time.Millisecond},
		{name: "outlasts the caller", pauses: []time.Duration{time.Hour}, timeout: 20 * time.Millisecond, wantErr: true},
		{name: "a shorter pause does not cut a longer one", pauses: []time.Duration{time.Hour, time.Millisecond}, timeout: 20 * time.Millisecond, wantErr: true},
		{name: "over before the caller gives up", pauses: []time.Duration{10 * time.Millisecond}, timeout: time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := NewPacer(config.PacingConfig{Rate: 1000, Burst: 10, ConversationRate: 1000, ConversationBurst: 10})
			for _, d := range tt.pauses {
				p.Throttled(d)
			}
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()
			err := p.Wait(ctx, "other")
			if (err != nil) != tt.wantErr {
				t.Errorf("Wait() = %v, want error %t", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("Wait() = %v, want the context's deadline", err)
			}
		})
	}
}

func TestProcessPacerEvictsIdleConversations(t *testing.T) {
	t.Parallel()

	p := NewPacer(config.PacingConfig{Rate: 1000, Burst: 10, ConversationRate: 0.001, ConversationBurst: 1}).(*processPacer)
	// A busy conversation survives: dropping it would hand back its burst.
	if err := p.Wait(context.Background(), "busy"); err != nil {
		t.Fatalf("Wait() = %v", err)
	}
	p.mu.Lock()
	for i := range maxConversationLimiters - 1 {
		p.conversation(fmt.Sprint(i))
	}
	p.conversation("new")
	p.mu.Unlock()

	if got := len(p.perKey); got != 2 {
		t.Errorf("limiters kept = %d, want 2: the busy one and the new one", got)
	}
}

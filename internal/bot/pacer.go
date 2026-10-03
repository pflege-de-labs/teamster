package bot

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// Pacer decides when a Bot Connector call may go out (ADR 0085). Every
// message, broadcast and install shares one, because the Connector throttles
// the bot as a whole rather than one request at a time.
type Pacer interface {
	// Wait blocks until a call to the conversation key may be sent, or ctx
	// ends. key is a conversation, channel or person id; "" is paced globally only.
	Wait(ctx context.Context, key string) error
	// Throttled tells the pacer the Connector answered 429 and asked for d.
	Throttled(d time.Duration)
}

// maxConversationLimiters bounds the per-conversation map; a broadcast to
// the tenant would otherwise keep one limiter per person forever.
const maxConversationLimiters = 10000

// processPacer paces this process alone: replicas each hold their own budget,
// which is why pacing-rate is a share of the tenant's, not all of it.
type processPacer struct {
	global *rate.Limiter
	// perRate and perBurst describe each conversation's limiter.
	perRate  rate.Limit
	perBurst int
	now      func() time.Time

	mu         sync.Mutex
	perKey     map[string]*rate.Limiter
	pauseUntil time.Time
}

// NewPacer builds the pacer cfg.Strategy names.
func NewPacer(cfg config.PacingConfig) Pacer {
	return &processPacer{
		global:   rate.NewLimiter(rate.Limit(cfg.Rate), cfg.Burst),
		perRate:  rate.Limit(cfg.ConversationRate),
		perBurst: cfg.ConversationBurst,
		now:      time.Now,
		perKey:   map[string]*rate.Limiter{},
	}
}

func (p *processPacer) Wait(ctx context.Context, key string) error {
	if err := p.sleepOutPause(ctx); err != nil {
		return err
	}
	// The conversation first: a call held there should not sit on a global token.
	if key != "" {
		if err := p.waitConversation(ctx, key); err != nil {
			return err
		}
	}
	return p.global.Wait(ctx)
}

// waitConversation takes the token under the lock, so evictIdle cannot drop
// the limiter between the lookup and the wait and hand out a second burst.
func (p *processPacer) waitConversation(ctx context.Context, key string) error {
	p.mu.Lock()
	r := p.conversation(key).ReserveN(p.now(), 1)
	p.mu.Unlock()
	if !r.OK() {
		return fmt.Errorf("conversation burst %d is below one call", p.perBurst)
	}
	if err := sleep(ctx, r.Delay()); err != nil {
		r.Cancel()
		return err
	}
	return nil
}

func (p *processPacer) Throttled(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if until := p.now().Add(d); until.After(p.pauseUntil) {
		p.pauseUntil = until
	}
}

// sleepOutPause waits until a Retry-After any sender received has passed, so
// one throttled call holds back the calls behind it rather than each finding out alone.
func (p *processPacer) sleepOutPause(ctx context.Context) error {
	p.mu.Lock()
	d := p.pauseUntil.Sub(p.now())
	p.mu.Unlock()
	if d <= 0 {
		return nil
	}
	return sleep(ctx, d)
}

// conversation is key's limiter; the caller holds p.mu.
func (p *processPacer) conversation(key string) *rate.Limiter {
	if l, ok := p.perKey[key]; ok {
		return l
	}
	if len(p.perKey) >= maxConversationLimiters {
		p.evictIdle()
	}
	l := rate.NewLimiter(p.perRate, p.perBurst)
	p.perKey[key] = l
	return l
}

// evictIdle drops limiters that have refilled: a fresh one behaves the same.
func (p *processPacer) evictIdle() {
	now := p.now()
	for key, l := range p.perKey {
		if l.TokensAt(now) >= float64(p.perBurst) {
			delete(p.perKey, key)
		}
	}
}

// sleep waits for d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

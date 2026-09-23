package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// A webhook that no route matches is still sampled, and what the sampler
// held when the server stopped reaches the database before it closes.
func TestServeSamplesWhatArrivesAndFlushesOnShutdown(t *testing.T) {
	t.Parallel()

	cfg := validConfig(t)
	cfg.Server.Addr = freePort(t)
	cfg.Samples = config.SamplesConfig{
		Enabled: true, Retention: time.Hour, MaxValuesPerKey: 5,
		MaxValueLength: 64, LRUSize: 16, FlushInterval: time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- (&ServeCmd{}).Run(ctx, cfg) }()
	waitForServer(t, cfg.Server.Addr)

	// Own transport, emptied before shutdown: a spare connection the shared
	// DefaultClient dialled but never used is StateNew, which Shutdown only
	// counts as idle after 5s -- exactly the drain deadline.
	transport := &http.Transport{}
	client := &http.Client{Transport: transport}
	for range 2 {
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/webhook/universal", cfg.Server.Addr),
			strings.NewReader(`{"status":"firing","labels":{"team":"db"},"annotations":{"summary":"secret detail"}}`))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("X-Teamster-Token", "token")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST /webhook/universal: %v", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	transport.CloseIdleConnections()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return after the context was cancelled")
	}

	st, err := store.Open(t.Context(), storeOptions(cfg, store.MigrateVerify))
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() { _ = st.Close() }()
	rows, err := st.ListAlertSamples(t.Context(), 10)
	if err != nil {
		t.Fatalf("ListAlertSamples: %v", err)
	}

	got := map[string]int64{}
	for _, row := range rows {
		got[string(row.Kind)+"/"+row.Key+"="+row.Value] = row.SeenCount
	}
	if got["label/team=db"] != 2 || got["annotation/summary="] != 2 || len(got) != 2 {
		t.Errorf("stored samples = %v, want team=db and the summary key, twice each", got)
	}
}

package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/llm"
)

// A reported zero and an unreported count must stay distinguishable on the
// wire — joe-pm threads/cost-latency-metadata-wire.md, invariant 2.
func TestTotalTokensCacheCountsOnTheWire(t *testing.T) {
	zero, read := 0, 512
	tests := []struct {
		name    string
		usage   llm.TokenUsage
		want    []string
		notWant []string
	}{
		{
			name:  "reported, zero included",
			usage: llm.TokenUsage{InputTokens: 10, OutputTokens: 2, CacheReadTokens: &read, CacheWriteTokens: &zero},
			want:  []string{`"cache_read_tokens":512`, `"cache_write_tokens":0`},
		},
		{
			name:    "unreported",
			usage:   llm.TokenUsage{InputTokens: 10, OutputTokens: 2},
			notWant: []string{`cache_read_tokens`, `cache_write_tokens`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := agentloop.NewSession(nil)
			t.Cleanup(session.Close)
			session.AddTokenUsage(context.Background(), tt.usage)

			resp := finalizeTaskResponse("t1", "s1", "completed", "", "answer", nil, session, 200000, "", "", time.Second)
			raw, err := json.Marshal(resp.TotalTokens)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := string(raw)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("total_tokens = %s, want it to contain %s", got, w)
				}
			}
			for _, nw := range tt.notWant {
				if strings.Contains(got, nw) {
					t.Errorf("total_tokens = %s, want no %s", got, nw)
				}
			}
		})
	}
}

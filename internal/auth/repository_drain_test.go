package auth

import (
	"context"
	"testing"
	"time"
)

// TestDeleteExpiredFlows proves the §12.5 abandoned-login-flow drain: it removes
// flows whose expires_at has passed and leaves still-valid flows intact, keyed by
// the injected clock. This is the auth-subsystem half of the sweeper's work,
// operating ONLY on auth_login_flows.
func TestDeleteExpiredFlows(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)

	mk := func(state string, expires time.Time) {
		if err := repo.CreateFlow(ctx, LoginFlow{
			State: state, CodeVerifier: "v", Nonce: "n",
			CreatedAt: expires.Add(-10 * time.Minute), ExpiresAt: expires,
		}); err != nil {
			t.Fatalf("CreateFlow %s: %v", state, err)
		}
	}
	mk("expired-1", now.Add(-1*time.Hour))
	mk("expired-2", now.Add(-1*time.Minute))
	mk("valid", now.Add(10*time.Minute))

	n, err := repo.DeleteExpiredFlows(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpiredFlows: %v", err)
	}
	if n != 2 {
		t.Errorf("drained %d flows, want 2", n)
	}

	for _, state := range []string{"expired-1", "expired-2"} {
		if f, _ := repo.GetFlow(ctx, state); f != nil {
			t.Errorf("expired flow %s survived the drain", state)
		}
	}
	if f, _ := repo.GetFlow(ctx, "valid"); f == nil {
		t.Error("still-valid flow was wrongly drained")
	}
}

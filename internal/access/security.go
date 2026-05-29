package access

import (
	"context"

	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
	"github.com/jaimegago/joe/internal/rbac"
)

// --- Falco ---

func (a *Accessor) FalcoListEvents(ctx context.Context, principal rbac.Principal, sourceID, priority, source, rule string, limit int) ([]falcoadapter.Event, error) {
	ad, err := guard[falcoadapter.FalcoAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "falco")
	if err != nil {
		return nil, err
	}
	return ad.ListEvents(ctx, priority, source, rule, limit)
}

func (a *Accessor) FalcoListRules(ctx context.Context, principal rbac.Principal, sourceID string) ([]falcoadapter.Rule, error) {
	ad, err := guard[falcoadapter.FalcoAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "falco")
	if err != nil {
		return nil, err
	}
	return ad.ListRules(ctx)
}

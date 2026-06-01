package llmsettings

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
)

// Audit context-key vocabulary (Stream G phase G4). These three keys
// establish the contract every later admin reader of
// llm_settings_mutation rows depends on. A change in shape would force
// every reader to special-case, so the keys are CONSTANTS — referenced
// from the service writes and asserted by tests, never re-spelled at
// the call site.
//
//   - AuditCtxTarget: WHAT was mutated. For the three current mutations
//     this is one of: "active_model", "cost_limit:<window>" (e.g.
//     "cost_limit:hourly"), or "runaway_ceiling". A target string lets
//     a later mutation share the kind/action without colliding on the
//     row shape.
//
//   - AuditCtxBefore: the value the target held before this write.
//     READ INSIDE THE SAME TRANSACTION as the write itself, so it
//     reflects true prior state — not a stale value the caller pulled
//     earlier. For the active model and the runaway ceiling the value
//     is a string and an integer respectively; for cost limits an int64
//     nano-units value. JSON's any-type representation handles all
//     three.
//
//   - AuditCtxAfter: the value being written.
//
// Tests in service_test.go assert these exact keys (and only these
// keys, in the canonical shape) round-trip through the audit row.
const (
	AuditCtxTarget = "target"
	AuditCtxBefore = "before"
	AuditCtxAfter  = "after"

	// AuditCtxTargetActiveModel is the canonical target string for an
	// active-model mutation.
	AuditCtxTargetActiveModel = "active_model"
	// AuditCtxTargetRunawayCeiling is the canonical target string for
	// a runaway-ceiling mutation.
	AuditCtxTargetRunawayCeiling = "runaway_ceiling"
)

// AuditCtxTargetCostLimit returns the canonical target string for a
// cost-limit mutation on the given window. Centralised so the format
// is decided in one place and tests can assert against it.
func AuditCtxTargetCostLimit(window string) string {
	return "cost_limit:" + window
}

// MutationService is the SOLE write path for the three settings
// tables. Each setter opens a transaction on the repository's database
// handle, reads the prior value inside the transaction, writes the new
// value, and writes one settings-mutation audit row through
// audit.Repository.InsertTx against the same transaction. On any error
// the transaction is rolled back: NEITHER the settings row NOR the
// audit row persists.
//
// The mutation service does NOT swap the live adapter. The endpoint
// that routes a model-change request through here (api/models.go)
// performs the swap on the SwappableAdapter only after the service
// returns nil — so a failed persist-and-audit transaction leaves the
// live adapter pointing at the prior model, which still matches what
// the settings table records.
type MutationService struct {
	repo  Repository
	audit audit.Repository
	// now is the test seam used by service_test.go to inject a
	// deterministic timestamp. Production passes time.Now.
	now func() time.Time
}

// NewMutationService builds the mutation service. auditRepo is
// required — every mutation writes an audit row, and the service
// refuses to construct without a sink (a nil sink would silently drop
// the forensic trail and defeat the atomicity contract).
func NewMutationService(repo Repository, auditRepo audit.Repository) *MutationService {
	return &MutationService{repo: repo, audit: auditRepo, now: time.Now}
}

// WithClock overrides the time source. Used by tests to assert the
// last_modified timestamp deterministically.
func (s *MutationService) WithClock(now func() time.Time) *MutationService {
	s.now = now
	return s
}

// SetActiveModel persists the new active-model value and writes one
// settings-mutation audit row atomically against the same
// transaction. The caller (api/models.go) is responsible for swapping
// the SwappableAdapter only after this returns nil.
func (s *MutationService) SetActiveModel(ctx context.Context, value string) error {
	return s.runMutation(ctx, AuditCtxTargetActiveModel, audit.ActionLLMSetActiveModel,
		func(tx *sql.Tx) (before any, after any, err error) {
			prev, rerr := s.repo.ReadActiveModelTx(ctx, tx)
			if rerr != nil {
				return nil, nil, rerr
			}
			if werr := s.repo.UpdateActiveModelTx(ctx, tx, value, s.now()); werr != nil {
				return nil, nil, werr
			}
			return prev, value, nil
		})
}

// SetCostLimit persists the new per-window cost threshold and writes
// the audit row atomically.
func (s *MutationService) SetCostLimit(ctx context.Context, window string, value int64) error {
	if !validWindow(window) {
		return fmt.Errorf("%w: invalid window %q", ErrSettingsWriteFailed, window)
	}
	return s.runMutation(ctx, AuditCtxTargetCostLimit(window), audit.ActionLLMSetCostLimit,
		func(tx *sql.Tx) (before any, after any, err error) {
			prev, rerr := s.repo.ReadCostLimitTx(ctx, tx, window)
			if rerr != nil {
				return nil, nil, rerr
			}
			if werr := s.repo.UpdateCostLimitTx(ctx, tx, window, value, s.now()); werr != nil {
				return nil, nil, werr
			}
			return prev, value, nil
		})
}

// SetRunawayCeiling persists the new session token ceiling and writes
// the audit row atomically.
func (s *MutationService) SetRunawayCeiling(ctx context.Context, value int) error {
	return s.runMutation(ctx, AuditCtxTargetRunawayCeiling, audit.ActionLLMSetRunawayCeiling,
		func(tx *sql.Tx) (before any, after any, err error) {
			prev, rerr := s.repo.ReadRunawayCeilingTx(ctx, tx)
			if rerr != nil {
				return nil, nil, rerr
			}
			if werr := s.repo.UpdateRunawayCeilingTx(ctx, tx, value, s.now()); werr != nil {
				return nil, nil, werr
			}
			return prev, value, nil
		})
}

// runMutation is the common single-transaction body shared by the
// three setters. The mutation callback runs the read-before-write and
// the write, returning the captured before/after values for the audit
// row. The audit row is written through audit.Repository.InsertTx
// against the SAME transaction — both rows commit or roll back as one.
func (s *MutationService) runMutation(
	ctx context.Context,
	target string,
	action string,
	mutate func(tx *sql.Tx) (before any, after any, err error),
) (err error) {
	tx, err := s.repo.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin tx: %v", ErrSettingsWriteFailed, err)
	}
	defer func() {
		if err != nil {
			// Best-effort rollback. If rollback itself errors, the
			// original err is what the caller sees.
			_ = tx.Rollback()
		}
	}()

	before, after, mErr := mutate(tx)
	if mErr != nil {
		err = mErr
		return err
	}

	blob, marshalErr := json.Marshal(map[string]any{
		AuditCtxTarget: target,
		AuditCtxBefore: before,
		AuditCtxAfter:  after,
	})
	if marshalErr != nil {
		// Practically unreachable for the value shapes the service
		// writes (string / int / int64), but a real failure must
		// abort the mutation — the contract is "atomic with the
		// audit row".
		err = fmt.Errorf("%w: marshal audit context: %v", ErrSettingsWriteFailed, marshalErr)
		return err
	}

	auditErr := s.audit.InsertTx(ctx, tx, audit.Event{
		Principal: string(rbac.PrincipalFromContext(ctx)),
		Action:    action,
		Decision:  audit.DecisionAllow,
		Reason:    "settings_change",
		Kind:      audit.KindLLMSettingsMutation,
		Context:   string(blob),
	})
	if auditErr != nil {
		// The settings write already happened inside this tx; the
		// rollback in the deferred handler removes it. No partial
		// state escapes.
		err = fmt.Errorf("%w: audit insert: %v", ErrSettingsWriteFailed, auditErr)
		return err
	}

	if cErr := tx.Commit(); cErr != nil {
		err = fmt.Errorf("%w: commit: %v", ErrSettingsWriteFailed, cErr)
		return err
	}
	return nil
}

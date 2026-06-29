package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/componentgov"
	"github.com/jaimegago/joe/internal/credential"
	"github.com/jaimegago/joe/internal/rbac"

	"github.com/jaimegago/joe/internal/adapters"
	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	azureadapter "github.com/jaimegago/joe/internal/adapters/azure"
	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	mongodbadapter "github.com/jaimegago/joe/internal/adapters/datastore/mongodb"
	mysqladapter "github.com/jaimegago/joe/internal/adapters/datastore/mysql"
	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	terraformadapter "github.com/jaimegago/joe/internal/adapters/iac/terraform"
	"github.com/jaimegago/joe/internal/adapters/k8s"
	envoyadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	helmadapter "github.com/jaimegago/joe/internal/adapters/packaging/helm"
	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
	"github.com/jaimegago/joe/internal/store"
)

// componentView is the read-model projection serialized by GET /api/v1/components
// and GET /api/v1/components/{id}. It deliberately OMITS the raw store.Component
// Config blob — which for an armed component carries credential reference
// locators (credential_provider, env_var, kubeconfig, context, in_cluster,
// audience) that must not reach any authenticated caller on these read endpoints
// (A002 read-model fix) — and replaces it with a server-derived arm-state
// projection: an `armed` boolean and, when armed, the provider Kind. The
// projection is computed from the already-loaded Config via armedState (the same
// helper the promote audit before-state uses) — never reimplemented, never a
// stored field. Every other field mirrors store.Component as before. Both read
// endpoints serialize this same shape so the frontend has one component read type.
type componentView struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	Name       string     `json:"name"`
	Armed      bool       `json:"armed"`
	Provider   string     `json:"provider,omitempty"`
	Status     string     `json:"status"`
	LastSyncAt *time.Time `json:"last_sync_at,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// newComponentView projects a loaded *store.Component into its read-model view,
// deriving armed/provider from the in-hand decrypted Config via armedState. The
// provider Kind is carried only when armed (omitempty), so an inert component
// serializes neither a provider nor the Config blob.
func newComponentView(c *store.Component) componentView {
	kind, armed := armedState(c.Config)
	v := componentView{
		ID:         c.ID,
		Type:       c.Type,
		Name:       c.Name,
		Armed:      armed,
		Status:     c.Status,
		LastSyncAt: c.LastSyncAt,
		LastError:  c.LastError,
		CreatedAt:  c.CreatedAt,
		UpdatedAt:  c.UpdatedAt,
	}
	if armed {
		v.Provider = kind
	}
	return v
}

func (s *Server) handleListComponents(w http.ResponseWriter, r *http.Request) {
	components, err := s.services.Store.Components.List(r.Context())
	if err != nil {
		writeInternalError(w, err, "list components")
		return
	}

	views := make([]componentView, 0, len(components))
	for _, c := range components {
		views = append(views, newComponentView(c))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"components": views,
		"count":      len(views),
	})
}

// handleListComponentTypes returns the authoritative component-type enum
// (store.AllowedComponentTypes) as a JSON list, so the operator-facing
// registration UI can populate its type selector from the single source of
// truth instead of hardcoding the set. This is AUTHENTICATED but deliberately
// NOT admin-gated: the value is a compile-time constant, not a privileged or
// tenant-specific datum, and the registration form is admin-gated at the write
// boundary regardless. It reads the enum directly — it never maintains its own
// copy — so a new component type flows into the UI automatically.
func (s *Server) handleListComponentTypes(w http.ResponseWriter, r *http.Request) {
	types := store.AllowedComponentTypes()
	writeJSON(w, http.StatusOK, map[string]any{
		"component_types": types,
		"count":           len(types),
	})
}

// newAdapterForType returns a fresh, unconnected adapter for the given source
// type, or nil when the type has no live connection to establish (config-only or
// metadata source types that are persisted as-is). It is the single source of
// truth for the type→adapter mapping shared by source creation and connection
// testing, so the two paths can never disagree on which components have adapters.
func newAdapterForType(sourceType string) adapters.Adapter {
	switch sourceType {
	case store.ComponentTypeAWS:
		return awsadapter.New()
	case store.ComponentTypeAzure:
		return azureadapter.New()
	case store.ComponentTypeKubernetes:
		return k8s.New()
	case store.ComponentTypeGit:
		return gitadapter.New()
	case store.ComponentTypePrometheus, store.ComponentTypeMimir:
		return prometheusadapter.New()
	case store.ComponentTypeLoki:
		return lokiadapter.New()
	case store.ComponentTypeTempo:
		return tempoadapter.New()
	case store.ComponentTypeJaeger:
		return jaegeradapter.New()
	case store.ComponentTypeAlertmanager:
		return alertmanageradapter.New()
	case store.ComponentTypePagerDuty:
		return pagerdutyadapter.New()
	case store.ComponentTypeGrafana:
		return grafanaadapter.New()
	case store.ComponentTypePostgreSQL:
		return postgresadapter.New()
	case store.ComponentTypeMySQL:
		return mysqladapter.New()
	case store.ComponentTypeRedis:
		return redisadapter.New()
	case store.ComponentTypeMongoDB:
		return mongodbadapter.New()
	case store.ComponentTypeKafka:
		return kafkaadapter.New()
	case store.ComponentTypeElasticsearch:
		return elasticsearchadapter.New()
	case store.ComponentTypeArgoCd:
		return argocdadapter.New()
	case store.ComponentTypeTerraform:
		return terraformadapter.New()
	case store.ComponentTypeHelm:
		return helmadapter.New()
	case store.ComponentTypeNginx:
		return nginxadapter.New()
	case store.ComponentTypeEnvoy:
		return envoyadapter.New()
	case store.ComponentTypeFalco:
		return falcoadapter.New()
	default:
		return nil
	}
}

// createComponentRequest is the JSON body for POST /api/v1/components.
type createComponentRequest struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

// handleCreateComponent registers a component (A003 Stream G governed CREATE).
// The registration is admin-gated, credential-less by construction, and lands
// the component plus its audit row in one fail-closed transaction. It performs
// NO eager Connect probe: a credential-less record cannot authenticate, so
// connecting at registration is both pointless and the attacker-controllable
// network-call / env-dereference vector A003 closes. The component lands inert
// — unassigned zone, read-only floor, no credential; connectivity checking and
// credential supply belong to promotion (a different stream).
func (s *Server) handleCreateComponent(w http.ResponseWriter, r *http.Request) {
	principal, gated := s.requireAdmin(w, r)
	if gated {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "failed to read body")
		return
	}

	var req createComponentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid JSON")
		return
	}

	if req.ID == "" || req.Type == "" || req.Name == "" {
		missing := []string{}
		if req.ID == "" {
			missing = append(missing, "id")
		}
		if req.Type == "" {
			missing = append(missing, "type")
		}
		if req.Name == "" {
			missing = append(missing, "name")
		}
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "id, type, and name are required", map[string]any{
			"missing": missing,
		})
		return
	}

	if !store.IsValidComponentType(req.Type) {
		writeError(w, http.StatusBadRequest, errorCodeInvalidComponent, "unsupported source type", map[string]any{
			"type":    req.Type,
			"allowed": store.AllowedComponentTypes(),
		})
		return
	}

	// Credential-less by construction: reject any credential-bearing field
	// rather than silently stripping it, so an operator who tried to smuggle a
	// secret in at registration learns it was refused. Credentials enter only
	// at promotion.
	if err := componentgov.RejectCredentialFields(req.Config); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest,
			"config must be credential-less at registration; credentials are supplied only at promotion",
			map[string]any{"detail": err.Error()})
		return
	}

	// Check if source already exists
	existing, err := s.services.Store.Components.Get(r.Context(), req.ID)
	if err != nil {
		writeInternalError(w, err, "get source")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, errorCodeInvalidRequest, "source already exists", map[string]any{
			"component_id": req.ID,
		})
		return
	}

	// Default an absent or empty config to an empty JSON object at the shared
	// registration seam, BEFORE the component is built (and so before encryption
	// in CreateTx). A config-less registration must persist and land inert; the
	// components.config column is NOT NULL with no default, so a nil config would
	// otherwise trip the constraint as a generic 500 (D-0029).
	source := &store.Component{
		ID:     req.ID,
		Type:   req.Type,
		Name:   req.Name,
		Config: componentgov.NormalizeRegistrationConfig(req.Config),
	}

	ev := componentRegisterEvent(principal, source)
	if err := s.mutateWithAudit(r.Context(), ev, func(tx *sql.Tx) error {
		return s.services.Store.Components.CreateTx(r.Context(), tx, source)
	}); err != nil {
		writeInternalError(w, err, "create source")
		return
	}

	writeJSON(w, http.StatusCreated, source)
}

// componentRegisterEvent builds the same-transaction audit row for a governed
// registration (HTTP create OR the register_component tool — both reuse this so
// the row shape cannot diverge across the two registration paths).
func componentRegisterEvent(principal rbac.Principal, source *store.Component) audit.Event {
	blob, _ := json.Marshal(audit.Details{
		Target: "component:" + source.ID,
		After:  map[string]string{"type": source.Type, "name": source.Name},
	})
	return audit.Event{
		Principal:   string(principal),
		Action:      audit.ActionComponentRegister,
		ComponentID: source.ID,
		Decision:    audit.DecisionAllow,
		Reason:      "component_registration",
		Kind:        audit.KindAdminAccess,
		Context:     string(blob),
	}
}

// mutateWithAudit runs mutate against a fresh transaction and, when the audit
// trail is wired, writes ev in the SAME transaction — fail-closed: an audit
// write failure rolls the mutation back, so a governed create/delete cannot
// land without its durable record. A nil audit repository (a unit-test harness
// that does not exercise the trail) skips the row but still commits the
// mutation, the same nil carve-out recordAdminDenial uses; the production wiring
// always sets services.Audit, so production is always fail-closed.
func (s *Server) mutateWithAudit(ctx context.Context, ev audit.Event, mutate func(*sql.Tx) error) (err error) {
	tx, err := s.services.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = mutate(tx); err != nil {
		return err
	}
	if s.services.Audit != nil {
		if err = s.services.Audit.InsertTx(ctx, tx, ev); err != nil {
			return fmt.Errorf("audit insert: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (s *Server) handleGetComponent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing source id", map[string]any{
			"param": "id",
		})
		return
	}

	source, err := s.services.Store.Components.Get(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "get source")
		return
	}
	if source == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "source not found", map[string]any{
			"component_id": id,
		})
		return
	}

	writeJSON(w, http.StatusOK, newComponentView(source))
}

// handleComponentPromotionRequirements describes — for a single component — the
// SHAPE of the credential reference an operator must supply to promote it (A002).
// It is the backend the promotion UI renders its provider-conditional form from:
// a GET sub-resource of the component, sibling of POST .../promote, ADMIN-GATED
// because it describes a privileged capability (the promote input contract).
//
// It is strictly DESCRIBE-ONLY and value-free. For a WIRED component it composes
// {type, wired:true, kind, locator_fields:[{name,required}], constraints:[...]}
// from credential.WiredProvider (type->kind) and the describe-only requirements
// table (credential.PromotionRequirements) — the locator field NAMES and required
// flags plus the Kind-level cross-field rules, never the component's stored Config,
// any locator VALUE, or any credential material. locator_fields excludes the
// credential_provider discriminator and the audience descriptor by construction
// (the table declares only the fields the form collects as a reference; a guard
// test pins the table's names to the provider struct's reflected fields).
//
// For an UNWIRED type it answers the legitimate capability question with 200 —
// {type, wired:false, armable_types:[...sorted...]} — rather than a 400: a describe
// GET reports that the type cannot be armed and which types can, sorting the
// (unsorted) wired-type registry for a stable form-facing list. Enforcement of the
// reject-unwired rule remains the promote handler's job (this endpoint changes no
// enforcement path).
func (s *Server) handleComponentPromotionRequirements(w http.ResponseWriter, r *http.Request) {
	if _, gated := s.requireAdmin(w, r); gated {
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing source id", map[string]any{
			"param": "id",
		})
		return
	}

	comp, err := s.services.Store.Components.Get(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "get source")
		return
	}
	if comp == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "source not found", map[string]any{
			"component_id": id,
		})
		return
	}

	kind, wired := credential.WiredProvider(comp.Type)
	if !wired {
		// A describe GET answers the capability question with 200: this type is not
		// armable, here are the types that are. The wired-type registry is unsorted;
		// sort for a stable, form-renderable list.
		armable := credential.WiredTypes()
		sort.Strings(armable)
		writeJSON(w, http.StatusOK, map[string]any{
			"type":          comp.Type,
			"wired":         false,
			"armable_types": armable,
		})
		return
	}

	reqs, ok := credential.PromotionRequirements(kind)
	if !ok {
		// A wired Kind with no requirements entry is a wiring bug the credential
		// coverage guard test forbids; fail loudly rather than describe an empty shape.
		writeInternalError(w, fmt.Errorf("no promotion requirements for wired kind %q (type %q)", kind, comp.Type), "promotion requirements")
		return
	}

	// SHAPE only — never the Config blob, a locator value, or credential material.
	writeJSON(w, http.StatusOK, map[string]any{
		"type":           comp.Type,
		"wired":          true,
		"kind":           string(kind),
		"locator_fields": reqs.Fields,
		"constraints":    reqs.Constraints,
	})
}

// handleComponentPromotionCandidates answers — for a single component — the LIVE
// question the promotion reference picker asks: "which credential references can
// the admin choose for this component right NOW?" (A002 candidate surface). It is
// the SIBLING of promotion-requirements, not a replacement: promotion-requirements
// is the cacheable SHAPE of a reference (which locator fields, which rules); this
// is the live candidate SET, which is not cacheable (it reflects the process
// environment / backing store at request time). Two endpoints, two questions.
//
// It is ADMIN-GATED like promote and promotion-requirements (it describes a
// privileged capability). It loads the component, resolves its type -> wired Kind
// via the W1 registry (credential.WiredProvider), and DELEGATES enumeration to the
// provider seam (credential.Provider.AvailableReferences): the static provider
// enumerates the process environment scoped to the type's JOE_<SEGMENT>_ prefix
// and returns {label, env_var_name} candidates — NAMES ONLY, never a value, never
// another prefix — while kubeconfig-exec answers honestly not-applicable. The
// env-var specifics live entirely behind the seam: this handler calls neither
// os.LookupEnv nor os.Environ (a structural guard test pins that).
//
// For a WIRED component it returns {type, wired:true, kind, prefix, applicable,
// candidates:[{label, env_var_name}]}. For an UNWIRED type it mirrors
// promotion-requirements' handling exactly — {type, wired:false,
// armable_types:[...sorted...]}, 200 — rather than a 400; enforcement of the
// reject-unwired rule remains the promote handler's job.
func (s *Server) handleComponentPromotionCandidates(w http.ResponseWriter, r *http.Request) {
	if _, gated := s.requireAdmin(w, r); gated {
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing source id", map[string]any{
			"param": "id",
		})
		return
	}

	comp, err := s.services.Store.Components.Get(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "get source")
		return
	}
	if comp == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "source not found", map[string]any{
			"component_id": id,
		})
		return
	}

	kind, wired := credential.WiredProvider(comp.Type)
	if !wired {
		// Same shape as promotion-requirements for an unwired type: 200 with the
		// sorted armable set. This type cannot be armed, here are the types that can.
		armable := credential.WiredTypes()
		sort.Strings(armable)
		writeJSON(w, http.StatusOK, map[string]any{
			"type":          comp.Type,
			"wired":         false,
			"armable_types": armable,
		})
		return
	}

	provider, err := credential.ProviderForKind(kind)
	if err != nil {
		// A wired Kind with no constructible provider is a wiring bug; fail loudly.
		writeInternalError(w, err, "promotion candidates")
		return
	}

	// DELEGATE to the provider seam — env-var enumeration lives in the static
	// provider, not here. The provider answers in normalized {candidates} terms.
	refs, err := provider.AvailableReferences(comp.Type)
	if err != nil {
		writeInternalError(w, err, "promotion candidates")
		return
	}

	// NAMES/labels only — refs carries no credential value by construction.
	writeJSON(w, http.StatusOK, map[string]any{
		"type":       comp.Type,
		"wired":      true,
		"kind":       string(kind),
		"prefix":     refs.Prefix,
		"applicable": refs.Applicable,
		"candidates": refs.Candidates,
	})
}

// handleDeleteComponent deregisters a component (A003 Stream G governed DELETE).
// Admin-gated; removes the FULL row — including whatever in-config credential
// reference the component carries — and writes its audit row in one fail-closed
// transaction, so a delete can never leave a dangling credential reference
// behind regardless of whether the component was ever promoted/armed.
func (s *Server) handleDeleteComponent(w http.ResponseWriter, r *http.Request) {
	principal, gated := s.requireAdmin(w, r)
	if gated {
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing source id", map[string]any{
			"param": "id",
		})
		return
	}

	source, err := s.services.Store.Components.Get(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "get source")
		return
	}
	if source == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "source not found", map[string]any{
			"component_id": id,
		})
		return
	}

	// Disconnect and unregister any in-memory adapter. This is in-process state,
	// not part of the DB transaction; a no-op for an inert (never-promoted)
	// component, which has no resident adapter.
	if err := s.services.Adapters.Unregister(id); err != nil {
		writeInternalError(w, err, "unregister source")
		return
	}

	blob, _ := json.Marshal(audit.Details{Target: "component:" + id})
	ev := audit.Event{
		Principal:   string(principal),
		Action:      audit.ActionComponentDelete,
		ComponentID: id,
		Decision:    audit.DecisionAllow,
		Reason:      "component_deletion",
		Kind:        audit.KindAdminAccess,
		Context:     string(blob),
	}
	if err := s.mutateWithAudit(r.Context(), ev, func(tx *sql.Tx) error {
		return s.services.Store.Components.DeleteTx(r.Context(), tx, id)
	}); err != nil {
		writeInternalError(w, err, "delete source")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// promoteComponentRequest is the JSON body for POST /api/v1/components/{id}/promote.
// It carries a credential REFERENCE — the credential_provider discriminator plus
// the wired provider Kind's locator fields — NOT an inline secret. The shape is
// the union of the two wired providers' locators; the handler validates which
// subset is well-formed for the component type's wired Kind. The `value` field
// exists only so an inline secret can be explicitly REJECTED (see
// buildArmedConfig) rather than silently ignored — the promotion boundary's whole
// point is that the armed record carries a reference, not a secret.
type promoteComponentRequest struct {
	CredentialProvider string `json:"credential_provider"`
	// static provider locators
	EnvVar string `json:"env_var"`
	Value  string `json:"value"`
	// kubeconfig-exec provider locators
	Kubeconfig string `json:"kubeconfig"`
	Context    string `json:"context"`
	InCluster  bool   `json:"in_cluster"`
	// shared, non-credential descriptor
	Audience string `json:"audience"`
}

// handlePromoteComponent arms a registered, inert, credential-less component by
// writing a credential REFERENCE into its Config blob (A003 promotion boundary).
// This is the single governed read-only-to-armed transition that owns credential
// entry, closing the credential-change gap that previously forced
// delete-and-recreate (Finding 3). The handler:
//
//   - is ADMIN-GATED via requireAdmin (the D-0029 standard);
//   - REJECTS promotion of a type with no wired credential provider — the FIRST
//     validation after the component loads, keyed on the W1 registry
//     (credential.WiredProvider): an unwired type can never be armed;
//   - VALIDATES the supplied reference is well-formed for that type's wired
//     provider Kind, and REFUSES an inline static secret (indirection-only);
//   - WRITES the reference into Config (B-2: discriminator + locator into the
//     existing blob, preserving routing fields) AND a component.promote audit
//     row in ONE fail-closed transaction (mutateWithAudit) — never recording the
//     credential material or locator values;
//   - performs NO credential resolution: no Connect, Resolve, or Probe.
//     Promotion writes a reference; whether it works is a separate explicit admin
//     Probe (admin.go resolveAndProbe). It is off the chat/LLM path — an admin
//     REST handler only.
//
// Re-promoting an already-armed component overwrites its reference in the same
// governed, audited transaction (idempotent-by-design), so a credential change is
// itself a gated, validated, audited event; the audit before-state distinguishes
// initial-arm from re-arm.
func (s *Server) handlePromoteComponent(w http.ResponseWriter, r *http.Request) {
	principal, gated := s.requireAdmin(w, r)
	if gated {
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing source id", map[string]any{
			"param": "id",
		})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "failed to read body")
		return
	}
	var req promoteComponentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid JSON")
		return
	}

	comp, err := s.services.Store.Components.Get(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "get source")
		return
	}
	if comp == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "source not found", map[string]any{
			"component_id": id,
		})
		return
	}

	// REJECT-UNWIRED — the first validation after the component is loaded. The
	// authority for "is this type wired" is the W1 registry; a type with no wired
	// credential provider can never be armed, so its promotion is refused before
	// any reference is even inspected.
	kind, wired := credential.WiredProvider(comp.Type)
	if !wired {
		writeError(w, http.StatusBadRequest, errorCodeInvalidComponent,
			"no credential provider wired for type "+comp.Type+"; this component type cannot be armed",
			map[string]any{"type": comp.Type, "wired_types": credential.WiredTypes()})
		return
	}

	// The supplied discriminator, if any, must match the type's wired Kind: a
	// component cannot be armed with a provider its adapter does not select.
	if req.CredentialProvider != "" && credential.Kind(req.CredentialProvider) != kind {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest,
			"credential_provider does not match the wired provider for this component type",
			map[string]any{"supplied": req.CredentialProvider, "wired": string(kind), "type": comp.Type})
		return
	}

	// Validate the reference is well-formed for the wired Kind and build the armed
	// config (reference merged into the existing routing config; any prior
	// reference cleared so a re-promote replaces it). locatorKeys is the reference
	// SHAPE for the audit row — key names only, never values.
	armedConfig, locatorKeys, err := buildArmedConfig(comp.Config, kind, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, err.Error())
		return
	}

	// UNIQUENESS GUARD (D-0061) — a static env_var locator must be unique across
	// the WHOLE component set, because environment variables are process-global:
	// two components sharing a name would resolve to the same secret with no
	// distinction. Names are operator-supplied and stored verbatim (never computed
	// or componentID-derived), so the only failure mode is a collision, prevented
	// here. The Config blob is encrypted at rest, so this cannot be a DB constraint
	// — it is an application-level decrypt-and-scan of peer components, excluding
	// this component itself so a re-promote keeping its own name is not a self-
	// conflict. Static-only: kubeconfig-exec references are file paths, not env vars.
	if kind == credential.KindStatic {
		if other, conflict, err := staticEnvVarConflict(r.Context(), s.services.Store.Components, req.EnvVar, id); err != nil {
			writeInternalError(w, err, "promote uniqueness scan")
			return
		} else if conflict {
			writeError(w, http.StatusConflict, errorCodeConflict,
				"environment variable "+req.EnvVar+" is already in use by another component; each component must reference a unique environment variable",
				map[string]any{"env_var": req.EnvVar, "conflicting_component_id": other})
			return
		}
	}

	// Distinguish initial-arm from re-arm from the BEFORE state, for the audit row.
	prevKind, wasArmed := armedState(comp.Config)

	ev := componentPromoteEvent(principal, id, comp.Type, kind, locatorKeys, wasArmed, prevKind)
	if err := s.mutateWithAudit(r.Context(), ev, func(tx *sql.Tx) error {
		return s.services.Store.Components.UpdateConfigTx(r.Context(), tx, id, armedConfig)
	}); err != nil {
		writeInternalError(w, err, "promote source")
		return
	}

	// Response carries the promotion OUTCOME only — never echoes the Config blob,
	// so the reference (env var name, kubeconfig path) does not leak into the
	// response body.
	writeJSON(w, http.StatusOK, map[string]any{
		"component_id": id,
		"type":         comp.Type,
		"provider":     string(kind),
		"armed":        true,
		"rearm":        wasArmed,
	})
}

// armedState reports whether a component's existing config already carries a
// credential reference (i.e. it was previously promoted/armed) and, if so, the
// provider Kind that reference selects. Presence of ANY credential-bearing field
// (the single-sourced credential.CredentialBearingFields set) means armed. Used
// only to distinguish initial-arm from re-arm in the audit before-state.
func armedState(config json.RawMessage) (kind string, armed bool) {
	if len(config) == 0 {
		return "", false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(config, &fields); err != nil {
		return "", false
	}
	for _, f := range credential.CredentialBearingFields() {
		if _, ok := fields[f]; ok {
			armed = true
			break
		}
	}
	if raw, ok := fields["credential_provider"]; ok {
		_ = json.Unmarshal(raw, &kind)
	}
	return kind, armed
}

// staticEnvVarConflict reports whether another component is already armed with
// the same static env_var locator, returning the conflicting component's id. It
// enforces the D-0061 uniqueness invariant: because env vars are process-global a
// name may belong to at most one component, and because the Config blob is
// encrypted at rest this cannot be a DB UNIQUE constraint — it is an application-
// level scan of the DECRYPTED peer configs (Components.List returns decrypted
// configs through the encrypted repository). selfID is excluded so re-promoting a
// component to its OWN existing name is not a self-conflict. An empty envVar never
// conflicts (buildArmedConfig already rejects it upstream). The scope is the whole
// component set, deliberately not per-type. It reads only the env_var locator
// NAME, never a value.
func staticEnvVarConflict(ctx context.Context, repo store.ComponentRepository, envVar, selfID string) (string, bool, error) {
	if envVar == "" {
		return "", false, nil
	}
	comps, err := repo.List(ctx)
	if err != nil {
		return "", false, err
	}
	for _, c := range comps {
		if c.ID == selfID || len(c.Config) == 0 {
			continue
		}
		var fields struct {
			EnvVar string `json:"env_var"`
		}
		if err := json.Unmarshal(c.Config, &fields); err != nil {
			// A non-object config carries no env_var locator to collide with.
			continue
		}
		if fields.EnvVar == envVar {
			return c.ID, true, nil
		}
	}
	return "", false, nil
}

// buildArmedConfig merges the validated credential reference into a copy of the
// component's existing (non-credential routing) config: routing fields are
// preserved, any prior credential reference is cleared first (so a re-promote
// replaces — not merges — the credential portion), then the discriminator +
// locator for the wired Kind are written. It validates the reference is
// well-formed for the Kind and enforces the inline-secret posture: a static
// reference MUST be an env_var indirection — an inline `value` (a literal secret)
// is REFUSED, because the armed record must carry a reference, not a secret.
// Returns the new config and the locator KEY names written (the reference shape
// for the audit row — never the values).
func buildArmedConfig(existing json.RawMessage, kind credential.Kind, req promoteComponentRequest) (json.RawMessage, []string, error) {
	fields := map[string]json.RawMessage{}
	if len(existing) > 0 {
		// A non-object existing config carries no routing fields the providers can
		// read, so starting clean loses nothing meaningful.
		_ = json.Unmarshal(existing, &fields)
	}
	// Clear any prior credential reference so a re-promote replaces it cleanly;
	// non-credential routing fields (e.g. endpoint) survive.
	for _, f := range credential.CredentialBearingFields() {
		delete(fields, f)
	}

	set := func(key string, val any) {
		b, _ := json.Marshal(val)
		fields[key] = b
	}

	var locatorKeys []string
	switch kind {
	case credential.KindStatic:
		if req.Value != "" {
			return nil, nil, fmt.Errorf("inline credential value is not accepted at promotion; supply an env_var indirection instead (the armed record carries a reference, not a secret)")
		}
		if req.EnvVar == "" {
			return nil, nil, fmt.Errorf("static credential reference requires env_var (the environment variable the credential is read from)")
		}
		if req.Kubeconfig != "" || req.Context != "" || req.InCluster {
			return nil, nil, fmt.Errorf("kubeconfig-exec locators are not valid for a static provider reference")
		}
		set("env_var", req.EnvVar)
		locatorKeys = []string{"env_var"}
	case credential.KindKubeconfigExec:
		if req.Value != "" || req.EnvVar != "" {
			return nil, nil, fmt.Errorf("static locators (value/env_var) are not valid for a kubeconfig-exec provider reference")
		}
		if !req.InCluster && req.Kubeconfig == "" {
			return nil, nil, fmt.Errorf("kubeconfig-exec reference requires either in_cluster=true or a kubeconfig path")
		}
		if req.InCluster {
			set("in_cluster", true)
			locatorKeys = append(locatorKeys, "in_cluster")
		}
		if req.Kubeconfig != "" {
			set("kubeconfig", req.Kubeconfig)
			locatorKeys = append(locatorKeys, "kubeconfig")
		}
		if req.Context != "" {
			set("context", req.Context)
			locatorKeys = append(locatorKeys, "context")
		}
	default:
		return nil, nil, fmt.Errorf("unsupported wired provider kind %q", kind)
	}

	// Write the discriminator the providers read (credential.KindFromConfig).
	set("credential_provider", string(kind))
	// Optional non-credential descriptor.
	if req.Audience != "" {
		set("audience", req.Audience)
	}

	out, err := json.Marshal(fields)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal armed config: %w", err)
	}
	return out, locatorKeys, nil
}

// componentPromoteEvent builds the same-transaction audit row for a promotion.
// The {target, before, after} context records the actor (Event.Principal),
// componentID (Event.ComponentID), type, provider Kind, and the reference SHAPE
// — the locator KEY names written, NEVER the credential material or locator
// values. before.armed distinguishes initial-arm from re-arm so a credential
// change via re-promotion is a legible event.
func componentPromoteEvent(principal rbac.Principal, id, compType string, kind credential.Kind, locatorKeys []string, wasArmed bool, prevKind string) audit.Event {
	before := map[string]any{"armed": wasArmed}
	if wasArmed && prevKind != "" {
		before["provider"] = prevKind
	}
	after := map[string]any{
		"armed":     true,
		"type":      compType,
		"provider":  string(kind),
		"reference": locatorKeys, // locator KEY names only — the reference shape, never values
	}
	blob, _ := json.Marshal(audit.Details{
		Target: "component:" + id,
		Before: before,
		After:  after,
	})
	reason := "component_promotion"
	if wasArmed {
		reason = "component_repromotion"
	}
	return audit.Event{
		Principal:   string(principal),
		Action:      audit.ActionComponentPromote,
		ComponentID: id,
		Decision:    audit.DecisionAllow,
		Reason:      reason,
		Kind:        audit.KindAdminAccess,
		Context:     string(blob),
	}
}

```
INVESTIGATION: Managed-system egress map (outbound calls to external/managed systems)
Date: 2026-06-09
Scope: Read-only investigation, no code changed. Runs against the tree AFTER the
       review-agent subsystem removal (commits 779c53f / a75fabc / 770ca64).
       Excludes the LLM-provider egress path (investigated separately). Every
       claim re-derived against the live tree with file:line.

================================================================================
VERDICT (§1 single-seam property)
================================================================================
NO — post-removal, "every managed-system mutation routes through the floor-checked
seam" holds ONLY for the agentic (LLM tool-executor) path. The direct HTTP API
surface still mutates managed systems OFF the floor:
  • the VCS mutation endpoints (GitHub comment/review, GitLab note) reach the
    accessor (RBAC) but skip the write floor; and
  • the publish-proposal endpoint reaches an external mutation (Confluence/Notion/
    Git) through NEITHER the floor NOR the RBAC accessor — only edge
    authentication — which is structurally the same "second entry point bypasses
    both gates" shape the removed review agent exhibited.
The review agent's own bypass is gone, but the bypass *pattern* survives in the
proposal-publish HTTP handler.

================================================================================
THE TWO ENFORCEMENT SEAMS (recap, as wired today)
================================================================================
1. WRITE FLOOR — checked only inside tools.Executor.Execute, on Mutate-classified
   tools: internal/tools/executor.go:215. Injected at exactly two construction
   sites: the Core Agent executor (internal/coreagent/agent.go:75) and the
   per-request chat/user-task loop executor (internal/api/tasks.go:280). There is
   NO floor check anywhere on the HTTP transport — the middleware chain
   (cmd/joe/server.go:703-723) has CORS → ratelimit → metrics → EdgeAuth →
   SessionMiddleware → rbac.EnforcementMiddleware → MaxRequestBody → mux, with no
   floor stage. The accessor does not check the floor either (see below).

2. RBAC ACCESSOR (access.permit) — the single guarded seam in front of the
   adapter registry. permit() evaluates the policy engine + writes one audit row;
   it does NOT consult the write floor (internal/access/access.go:120-172). guard[T]
   is the ONLY caller of adapters.Registry.Get (internal/access/access.go:206), an
   invariant asserted by guard_test / access_guard_test. Confirmed: the only other
   ".registry.Get" hits in the tree are the *tool* registry, not the adapter
   registry (internal/tools/executor.go:192, internal/coreagent/agent.go:161).

   rbac.EnforcementMiddleware is now a PURE PASS-THROUGH (internal/rbac/middleware.go:78-83)
   — RBAC was demoted off the transport (Phase E); the accessor is the sole RBAC
   gate. Consequence: any HTTP path that does NOT reach the accessor has NO RBAC at
   all, only EdgeAuth (authentication).

================================================================================
§1 — EVERY MUTATING OUTBOUND CALL TO AN EXTERNAL/MANAGED SYSTEM
================================================================================
Post-removal, the complete set of managed-system MUTATIONS is SIX adapter/method
egress points behind THREE mutate-classified tools, plus two local-only mutators.
(write_file / run_command mutate the LOCAL host, not a managed external system —
listed at the end for completeness.)

A. VCS THREAD MUTATIONS (GitHub/GitLab) — adapter call is gated by the accessor.
   ───────────────────────────────────────────────────────────────────────────
   Tool→accessor→adapter:
     github_comment          tier.go:252 (ActionMutate)
       → accessor.GitHubPostComment      access/vcs.go:81  (rbac.ActionMutate)
       → adapter PostComment
     github_request_changes  tier.go:258 (ActionMutate)
       → accessor.GitHubRequestChanges   access/vcs.go:89  (rbac.ActionMutate)
       → adapter RequestChanges
     gitlab_comment          tier.go:253 (ActionMutate)
       → accessor.GitLabPostNote         access/vcs.go:123 (rbac.ActionMutate)
       → adapter PostNote
   Registered as tools: internal/tools/default.go:143,144,147.

   TWO entry paths reach these accessor methods, with DIFFERENT enforcement:
     (a) Agentic path — executor (FLOOR ✓, tasks.go:280 / agent.go:75) → inproc
         client → accessor (RBAC ✓). Inproc wiring: internal/api/inproc_client.go:645,650,665.
         → BOTH floor and RBAC. ✓
     (b) Direct HTTP path — POST .../comments|/reviews|/notes →
         internal/api/vcs.go:24,25,32 → handlers call the accessor directly
         (vcs.go:139,172,268). NO executor in this path ⇒ NO floor; accessor still
         does RBAC.
         → RBAC only, NO FLOOR.  ← off the single-seam property
     Reachable by any authenticated API caller (these componentID-keyed paths are
     also what the remote client.Client exposes: internal/client/vcs.go:67,79,138).

   UNUSED: accessor.GitLabRequestChanges (access/vcs.go:131, rbac.ActionMutate) has
   NO caller anywhere — no tool, no HTTP route, no client method. Dead mutating
   accessor method (orphan; safe but worth pruning).

B. DOC-PUBLISH MUTATIONS (Confluence / Notion / Git) — NO accessor involvement.
   ───────────────────────────────────────────────────────────────────────────
   Tool: publish_doc_update  tier.go:233 (ActionMutate, PolicyKey "confluence_publish")
     registered at internal/tools/default.go:138.
   The publish does NOT go through the adapter registry or the accessor. It loads
   source config from services.Knowledge.ListSources and calls the sync packages /
   git helper DIRECTLY:
     internal/api/publish.go:20  publishProposalToTarget — dispatch by TargetType
       Confluence → confluencesync.UpdatePage   publish.go:61
       Notion     → notionsync.UpdatePage       publish.go:83
       Git        → gitadapter.CommitAndPush     publish.go:111
   So even on the floor-checked path, doc-publish has NO RBAC zone check; it writes
   to the FIRST source of the matching type regardless of caller zone.

   TWO entry paths, DIFFERENT enforcement:
     (a) Agentic path — executor (FLOOR ✓) → inproc client PublishProposal
         (internal/api/inproc_client.go:616-631) → publishProposalToTarget.
         → FLOOR ✓, but NO accessor/RBAC.
     (b) Direct HTTP path — POST /api/v1/knowledge/proposals/{id}/publish
         (internal/api/proposals.go:118 handlePublishProposal) → server.publishProposal
         (proposals.go:138 → publish.go:36 → publishProposalToTarget). This route has
         NO componentID, so even the (now-passthrough) EnforcementMiddleware is moot;
         the handler never touches the accessor and there is no executor/floor.
         → NEITHER floor NOR accessor — only EdgeAuth.  ← bypasses BOTH (see §3)
     This is the Web UI "publish" button's endpoint, so path (b) is live, not vestigial.

   Minor: the generic publish_doc_update tool is policy-gated under PolicyKey
   "confluence_publish" (tier.go:233) for ALL three target types — a Notion or Git
   publish is checked against the Confluence policy key. The per-target classification
   entries publish_doc_update_{confluence,notion,git} (tier.go:229-231) exist but no
   tool by those names is registered (default.go registers only the generic one).

C. LOCAL-HOST MUTATORS (not external/managed, listed for completeness)
   ───────────────────────────────────────────────────────────────────────────
   write_file  (tier.go:221, ActionMutate) — local filesystem.
   run_command (tier.go:222, ActionMutate) — local shell; can reach external infra
     via CLI side-effects, but is floor-gated like any Mutate. Both go through the
     executor floor; neither touches the accessor.

MUTATION SUMMARY TABLE
  Egress point                          Floor?            RBAC accessor?
  ─────────────────────────────────────────────────────────────────────
  github_comment   (agentic)            yes               yes
  github_comment   (direct HTTP)        NO                yes
  github_request_changes (agentic)      yes               yes
  github_request_changes (direct HTTP)  NO                yes
  gitlab_comment   (agentic)            yes               yes
  gitlab_comment   (direct HTTP)        NO                yes
  publish_doc_update Confluence (agentic) yes             NO
  publish_doc_update Confluence (HTTP)  NO                NO
  publish_doc_update Notion (agentic)   yes               NO
  publish_doc_update Notion (HTTP)      NO                NO
  publish_doc_update Git (agentic)      yes               NO
  publish_doc_update Git (HTTP)         NO                NO

So: of the current mutating outbound calls, NONE is reachable only off both gates
on the AGENTIC path — but on the DIRECT HTTP surface, the doc-publish endpoint
reaches an external mutation off BOTH gates, and the VCS endpoints reach external
mutations off the floor.

================================================================================
§2 — READ OUTBOUND CALLS (census by system class)
================================================================================
All read egress to managed systems flows through the accessor (the sole
adapters.Registry.Get seam), each declaring rbac.ActionRead at the call site —
EXCEPT two documented/structural bypasses (listed last). Grouped, not per-line:

  Class            Accessor file            Examples (all ActionRead)
  ─────────────────────────────────────────────────────────────────────────────
  VCS read         access/vcs.go:57-121     GitHubGetPR/PRDiff/ListPRs, GitLab MR get/diff/list
  Kubernetes       access/k8s.go:12-30       list/get resources, pod logs
  Git repo files   access/git.go:11-38       read file, list files, log, diff
  Cloud (AWS)      access/aws.go:10-66       EC2/EKS/RDS/VPC list+get (Azure adapter present)
  Observability    access/observability.go   Prometheus, Loki, Tempo, Jaeger, Datadog,
                                             Splunk, Dynatrace, NewRelic
  Alerting         access/alerting.go:14-58  Alertmanager, PagerDuty, Grafana
  Datastores       access/datastore.go:17-139 Postgres, MySQL, Redis, MongoDB, Kafka, Elasticsearch
  GitOps/IaC       access/gitops.go:14-90    ArgoCD, Terraform (state), Helm
  Networking       access/networking.go:13-55 Nginx, Envoy
  Registries       access/registry.go:14-82  OCI, Artifactory, ECR
  Security         access/security.go:12-20  Falco events/rules
  Observe category access/observe.go:50-114  Observe{Metrics,Logs,Traces,Alerts}; adapter
                                             resolved via graph edges, ActionRead

  Note (SELECT-only SQL): postgres_query / mysql_query run raw SQL but are classified
  ActionRead (tier.go:115,117) and gated rbac.ActionRead (access/datastore.go
  PostgresQuery/MySQLQuery). Read-only is enforced INSIDE the adapter's Query(), not
  by the safety classification or RBAC action — so a non-SELECT here would NOT be
  floor-gated; it relies entirely on the adapter rejecting it.

  READ BYPASSES OF THE ACCESSOR (do not route through access.permit):
    1. Core Agent refresh — internal/coreagent reads external Git/ArgoCD/etc.
       adapters directly during graph refresh (e.g. git_refresh.go:14 takes a
       git.GitAdapter). This is the codebase's own DOCUMENTED accessor exception
       (access/access.go:20-22 header + the api/access_guard_test allowlist). Reads
       only; runs under no principal (autonomous refresh).
    2. Doc-drift detector — internal/knowledge/drift/detector.go holds its OWN
       http.Client (detector.go:36,44) and fetches Confluence (detector.go:161) and
       Notion (detector.go:195, api.notion.com) directly, bypassing the accessor.
       Backs the detect_doc_drift tool (ActionRead, default.go:136). Read-only.

================================================================================
§3 — CALLS THAT BYPASS BOTH THE ACCESSOR AND THE FLOOR
================================================================================
SURVIVING bypass-both (mutation): the proposal-publish HTTP handler.
  POST /api/v1/knowledge/proposals/{id}/publish
    internal/api/proposals.go:118 handlePublishProposal
      → server.publishProposal (proposals.go:138)
      → publishProposalToTarget (publish.go:36/20)
      → confluencesync.UpdatePage / notionsync.UpdatePage / gitadapter.CommitAndPush
        (publish.go:61/83/111)
  No executor (⇒ no floor), no accessor (⇒ no RBAC; EnforcementMiddleware is a
  passthrough and the path has no componentID anyway). Only EdgeAuth gates it. This
  mutates an external system (Confluence/Notion/Git) in observation OR safe mode,
  which the write floor is specifically meant to forbid. Same structural shape as
  the removed review agent's separate-registry-wrapper bypass — the agent is gone,
  the pattern is not.

PARTIAL bypass (floor only): the VCS mutation HTTP endpoints in §1.A path (b)
  (vcs.go:139,172,268) skip the floor while keeping RBAC. Off the single-seam
  property, but RBAC + audit still fire.

NO OTHER bypass-both survives. The review-agent's own registry wrapper is removed;
the only other adapter-resolution seam is access.guard, and the only non-accessor
external egress points are the read-only drift detector and the (documented) Core
Agent refresh — neither mutates a managed system. grep for the removed subsystem
left only inert references in comments/tests (no live code path).

================================================================================
OUT OF SCOPE / ADJACENT EGRESS (noted, not managed-infra mutation)
================================================================================
  • Slack (internal/slack, slack-go socketmode): the `joe slack` SUBCOMMAND runs as
    a SEPARATE process; its outbound to the Slack API is chat transport, not a
    server-side managed-system mutation, and is governed by neither the server's
    floor nor its accessor (different process).
  • OIDC IdP (internal/auth/oidc.go): outbound token-exchange / JWKS to the
    configured identity provider — auth infrastructure, read-only.
  • LLM providers: excluded per scope (covered by the separate LLM-egress
    investigation). internal/notify and internal/safety/notifier.go do NO external
    egress (DB-backed clarifications/notifications only).
================================================================================
```

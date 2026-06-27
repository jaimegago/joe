# Investigation: the direct-HTTP managed-system mutation surface

**Date:** 2026-06-09
**Tree:** post-review-removal (`770ca64`, after `779c53f` / `a75fabc`)
**Scope:** read-only. No code changed. Every claim below was re-derived against the
live tree; file:line citations are to the current working tree.

---

## Verdict

**MIXED, but with a sharp and consistent split.** The HTTP routes that reach an
*external/managed-system mutation* — the three VCS comment/review/note endpoints
and the proposal-publish endpoint — are **dead as HTTP entry points**: no Web UI
component calls them, no production Go caller drives them (the only `*client.Client`
methods that target them have test-only callers), and neither the MCP proxy nor the
Slack bot invokes them. The *capability* each duplicates is live, but it is served
entirely **in-process** through the agentic tool path
(`executor → inProcessCoreClient → accessor → adapter`), which is the path that
actually passes the write floor (enforced in the executor, not the accessor). The
HTTP routes are therefore vestigial second entry points inherited from the
two-binary thin-client era (born as `internal/api/review.go` in phase 9, renamed to
`vcs.go` in `779c53f`), and because handlers call the accessor (or, for publish, not
even that) instead of the executor, they bypass the floor — exactly the structural
hole the prior audit flagged. They are **dead/vestigial HTTP surface**, not
load-bearing: nothing live depends on the HTTP route, and the floored agentic path
is unaffected by their existence. The read-only duplicate routes (per-vendor
observability/gitops/aws/k8s/git GET routes) share the same dead-HTTP-caller
property but carry no mutation risk. Genuinely live HTTP surface is confined to
category (c): control-plane (auth, admin, sessions/runs/captain, model swap, panic,
task submission, SSE) and the observe-category query API the MCP/Slack clients
actually use. *This section establishes dead-vs-live; it does not recommend removal.*

---

## Per-route table — external managed-system **mutation** routes (the focus)

| Route | Handler | Reaches | Floor? | RBAC (accessor)? | Duplicate of tool | Live caller today? | Era |
|---|---|---|---|---|---|---|---|
| `POST /api/v1/github/{componentID}/pulls/{number}/comments` | `vcsHandler.handleGitHubPostComment` ([vcs.go:116](internal/api/vcs.go:116)) | `accessor.GitHubPostComment` → `adapter.PostComment` | **NO** | yes (ActionMutate) | `github_comment` | **none** (UI ✗, client method test-only, MCP/Slack ✗) | phase 9 (2026-03-01), pre-collapse |
| `POST /api/v1/github/{componentID}/pulls/{number}/reviews` | `vcsHandler.handleGitHubRequestChanges` ([vcs.go:149](internal/api/vcs.go:149)) | `accessor.GitHubRequestChanges` → `adapter.RequestChanges` | **NO** | yes (ActionMutate) | `github_request_changes` | **none** | phase 9 (2026-03-01), pre-collapse |
| `POST /api/v1/gitlab/{componentID}/projects/{projectID}/mrs/{iid}/notes` | `vcsHandler.handleGitLabPostNote` ([vcs.go:246](internal/api/vcs.go:246)) | `accessor.GitLabPostNote` → `adapter.PostNote` | **NO** | yes (ActionMutate) | `gitlab_comment` | **none** | phase 9 (2026-03-01), pre-collapse |
| `POST /api/v1/knowledge/proposals/{id}/publish` | `proposalHandler.handlePublishProposal` ([proposals.go:118](internal/api/proposals.go:118)) | `s.publishProposal` → `publishProposalToTarget` → Confluence/Notion/Git write | **NO** | **NO** (does not go through accessor) | `publish_doc_update` | **none** (UI ✗, client method test-only, MCP/Slack ✗) | phase 8 (2026-02-21); shared dispatch extracted in Phase E (2026-05-30) |

## Per-route table — read-only duplicate routes (category a, no mutation risk)

| Route family | Handler file | Duplicate of tool(s) | Floor? | RBAC? | Live HTTP caller? |
|---|---|---|---|---|---|
| `GET /github/.../pulls`, `/pulls/{n}`, `/diff` | [vcs.go:39–114](internal/api/vcs.go:39) | `github_pr_get`, `github_pr_diff` (no list tool) | n/a (read) | yes | none (UI ✗, client test-only) |
| `GET /gitlab/.../mrs`, `/{iid}`, `/diff` | [vcs.go:184–244](internal/api/vcs.go:184) | `gitlab_mr_get`, `gitlab_mr_diff` (no list tool) | n/a | yes | none |
| `GET /argocd|terraform|helm/...` | [gitops.go:313](internal/api/gitops.go:313) | `argocd_tools`, `terraform_tools`, `helm_tools` | n/a | yes | none |
| `GET /prometheus|loki|tempo|jaeger|datadog|splunk|dynatrace|newrelic/...` | [server.go:200](internal/api/server.go:200) | observe-category tools | n/a | yes | none (MCP uses `POST /observe/*` instead) |
| `GET /aws/...`, `/k8s/...`, `/git/...` | aws/k8s/git handlers | aws/k8s/git core tools | n/a | yes | none |

## Category (c) — genuinely-live control-plane / infrastructure routes (out of scope, listed for completeness)

Auth (`/auth/config`, `/me`), admin RBAC (`/admins`, `/zones`, `/policies`,
`/component-zones`, `/principals/*`), session-model (`/sessions`, `/agent-sessions`,
`/captain/*`, `/runs/*`, `/findings`, `/solicitations`), model control plane
(`/models/current`, `/llm/settings/*`), safety (`/panic`, `/unlock`, `/regime/*`,
`/mutate-status`), skills (`/skills/*`), knowledge store CRUD (`/knowledge/entries`,
`/knowledge/sources`, `/knowledge/search`), component registration (`/components`,
`/components/{id}/test`), agent triggers (`/onboarding`, `/refresh`), task submission
+ SSE (`/tasks`, `/tasks/stream`), and the **observe-category query API**
(`POST /observe/{metrics,logs,traces,alerts,k8s}`) which the MCP proxy and Slack bot
do drive. These are not managed-system mutations (or are the live query surface).

---

## 1. Where the floor and RBAC actually sit (mechanism)

- **The accessor enforces RBAC + audit only — never the floor.** `guard[T]`
  ([access.go:194](internal/access/access.go:194)) calls `permitForPrincipal` →
  `permit` ([access.go:120](internal/access/access.go:120)), which evaluates the
  policy engine and writes an audit row. There is no floor reference anywhere in
  `internal/access/` (grep confirms: only `access.go:194` matches `floor`, and that
  is the word "guard" — no `WriteFloor` use). VCS mutations declare `rbac.ActionMutate`
  at [access/vcs.go:82,90,124](internal/access/vcs.go:82) so RBAC applies, but no
  floor check exists on this path.
- **The write floor is enforced only in the tool executor.**
  [executor.go:215](internal/tools/executor.go:215):
  `if e.floor.Up() && classification.Class == safety.ActionMutate { … WriteFloorError }`,
  with `classification := safety.ClassifyTool(name)` ([executor.go:199](internal/tools/executor.go:199)).
  The floor is injected via `tools.WithWriteFloor` ([executor.go:114](internal/tools/executor.go:114))
  and at the captain gate ([cmd/joe/server.go:599](cmd/joe/server.go:599)). HTTP
  handlers never go through the executor, so they never hit this check.
- **`EnforcementMiddleware` is a no-op pass-through after Phase E**
  ([rbac/middleware.go:78–83](internal/rbac/middleware.go:78): `return next`). So
  HTTP-path RBAC is *entirely* the accessor's responsibility. The only edge gate
  that always fires is `auth.EdgeAuth` ([cmd/joe/server.go:710](cmd/joe/server.go:710)) —
  authentication, not authorization.
- **Consequence:** a route whose handler calls the accessor gets RBAC but not the
  floor (the three VCS routes). A route whose handler skips the accessor gets
  neither (the publish route).

## 2. Liveness — who actually calls the four mutation routes

**(i) Web UI.** No api-client wrapper and no component exists for any of them. The
`ui/src/api/` client surface is `alerts, authConfig, chat, components, currentUser,
graph, llm, mutateStatus, panic, regime, security` only — no `proposals`, `vcs`,
`github`, `gitlab`, `gitops`, or `knowledge` client, and no proposal/PR/MR UI
component. The publish button the prior audit worried about **does not exist** in the
UI; there is no UI path to `POST /knowledge/proposals/{id}/publish`.

**(ii) Remote Go client (`internal/client/`).** The methods that target these routes
exist — `GitHubPostComment` ([client/vcs.go:67](internal/client/vcs.go:67)),
`GitHubRequestChanges` ([client/vcs.go:79](internal/client/vcs.go:79)),
`GitLabPostNote` ([client/vcs.go:138](internal/client/vcs.go:138)),
`PublishProposal` ([client/proposals.go:73](internal/client/proposals.go:73)) — but
their **only callers are `*_test.go` files** (`client_vcs_test.go`,
`proposals_test.go`). No production code constructs an HTTP request to these routes.
The core tools (`internal/tools/core/github_comment.go` etc.) are typed against the
`coretools.CoreToolsClient` *interface*, and the **sole production registry** is
`tools.NewCoreRegistry(h.server.inproc, …)` ([tasks.go:269](internal/api/tasks.go:269)) —
i.e. the in-process accessor client, not `*client.Client`. So the tool capability
runs in-process and never touches these HTTP routes.

**(iii) Everything else.** MCP and Slack do not reference any of these methods
(grep over `internal/mcp/`, `internal/slack/` for the VCS/publish method names
returns nothing). The MCP dispatcher calls only `GraphQuery`, `GraphRelated`,
`QueryMetrics/Logs/Traces/Alerts`, `SearchKnowledge`; Slack calls only `GraphQuery`,
`GraphSummary`. The in-process tool path (`inProcessCoreClient.PublishProposal`
[inproc_client.go:616](internal/api/inproc_client.go:616), `…GitHubPostComment`
[inproc_client.go:645](internal/api/inproc_client.go:645)) dispatches to the accessor
/ `publishProposalToTarget` directly — no HTTP hop.

**Net:** all four mutation routes are reachable today only by tests. As HTTP
entry points they are dead.

## 3. Provenance

Timeline anchors (from `git log`):
- **Phase E loopback removal** — `d3de80d`, **2026-05-30**, "feat(identity): remove
  loopback, accessor governs the loop — Phase E".
- **Single-binary collapse** — `5f9dbd7`, **2026-06-03**, "refactor: collapse
  joe-core into single joe binary". (Phase E preceded the collapse.)
- **Review-subsystem removal / rename** — `779c53f`, **2026-06-09**, deleted
  `internal/review/` but *renamed* `internal/api/review.go → vcs.go` and
  `registerReviewRoutes → registerVCSRoutes`, preserving the VCS routes. This rename
  masks the true lineage under `--follow`.

Per item:
- **VCS POST handlers + `registerVCSRoutes`** — born `b14bb97` "feat: phase 9"
  (**2026-03-01**) as `internal/api/review.go`; routed through the accessor in Phase A
  (`51531cc`, 2026-05-29); path params renamed for D-0021 (`041eb9c`, 2026-06-08);
  file/func renamed review→vcs (`779c53f`, 2026-06-09). **Predates** both the collapse
  and Phase E — original two-binary thin-client surface, since maintained but never
  re-wired to a live HTTP caller.
- **`registerProposalRoutes` + `handlePublishProposal`** — `00e3e9c` "feat: phase 8"
  (**2026-02-21**). Predates both anchors.
- **`publishProposalToTarget`** ([publish.go:20](internal/api/publish.go:20)) — the
  shared package-level dispatcher was extracted in **Phase E** (`d3de80d`,
  2026-05-30) so the HTTP handler and the new in-process client could share dispatch
  "neither path goes through the loopback after Phase E" (the file comment). The
  underlying `publishToConfluence/Notion/Git` helpers date to phase 8.
- **`registerGitOpsRoutes`** (read GETs) — `929a3c4` "feat: phase 6.7"
  (**2026-02-20**). Oldest; predates both anchors.
- **`accessor.GitLabRequestChanges`** ([access/vcs.go:131](internal/access/vcs.go:131))
  — added in Phase A (`51531cc`, 2026-05-29) and **never wired to anything** (see §5c).
- **Client VCS/publish methods** — phase 9 (VCS) / phase 8 (publish); two-binary era.

Assessment: every mutation route here originates in the two-binary era and is the
HTTP surface the old thin CLI client drove. The single-binary collapse and Phase E
moved the live path in-process (`inProcessCoreClient` + accessor), leaving the HTTP
routes as un-rewired vestige. The `779c53f` rename kept them compiling, not because a
live HTTP caller was identified.

## 4. The remote client's role in the single-binary world

`internal/client/` is **not wholesale dead, but is partially vestigial**. `client.New`
has live production callers, all in `cmd/joe/` subcommands that run as *separate
processes* and talk to the daemon over HTTP:
- `joe panic` — [cmd/joe/main.go:145](cmd/joe/main.go:145)
- `joe mcp` — [cmd/joe/main.go:228](cmd/joe/main.go:228) (`mcp.NewServer(coreClient)`)
- `joe slack` — [cmd/joe/main.go:269](cmd/joe/main.go:269)
- `joe skills reload` — [cmd/joe/main.go:524](cmd/joe/main.go:524)
- `joe incident` — [cmd/joe/incident.go:67](cmd/joe/incident.go:67)

But only a **subset** of client methods has a live caller: graph (`GraphQuery`,
`GraphRelated`, `GraphSummary`), the observe-category queries (`QueryMetrics/Logs/
Traces/Alerts`), `SearchKnowledge`, regime, panic, skills-reload. The client's
**VCS mutation methods, `PublishProposal`, and the per-vendor read methods**
(`PrometheusQuery`, `ArgoCDApps`, `K8sListResources`, `GitReadFile`, `AWSEC2…`, etc.)
have **no production caller** — test-only. Those methods are the two-binary remnant:
the in-process tool path replaced them with direct accessor calls, and the live
out-of-process consumers (MCP/Slack/CLI) never call them. So the direct-HTTP VCS/
publish routes have no real programmatic consumer; their nominal client is vestigial.

## 5. The three specific findings — confirmed and located

**(a) Proposal-publish bypasses BOTH gates — confirmed.**
Route `POST /api/v1/knowledge/proposals/{id}/publish`
([proposals.go:19](internal/api/proposals.go:19)) → `handlePublishProposal`
([proposals.go:118](internal/api/proposals.go:118)) → `s.publishProposal`
([proposals.go:138](internal/api/proposals.go:138)) → `publishProposalToTarget`
([publish.go:35→20](internal/api/publish.go:20)) → `publishToConfluence/Notion/Git`
([publish.go:39/66/88](internal/api/publish.go:39)) → `confluencesync.UpdatePage` /
`notionsync.UpdatePage` / `gitadapter.CommitAndPush`
([publish.go:61/83/111](internal/api/publish.go:61)). This dispatch **never calls the
accessor** → no RBAC; it **never goes through the executor** → no floor. The path has
no `componentID`, so even the (now no-op) `EnforcementMiddleware` would not apply; the
only gate is `auth.EdgeAuth` (authentication). **Contradiction resolved:** the prior
audit's worry that "the UI publish button drives this endpoint" is unfounded — there
is **no publish button and no proposals client in the UI at all**. The only live
driver of the publish *capability* is the agentic tool `publish_doc_update`
([tools/core/publish_doc_update.go](internal/tools/core/publish_doc_update.go)),
classified `ActionMutate` ([safety/tier.go:233](internal/safety/tier.go:233), and the
per-target keys at [tier.go:229–231](internal/safety/tier.go:229)), which runs
through the executor and therefore **does** pass the floor, then calls
`inProcessCoreClient.PublishProposal` → `publishProposalToTarget` in-process. The HTTP
route is an unguarded parallel path with no live caller.

**(b) VCS mutation endpoints bypass the floor — confirmed.**
`handleGitHubPostComment` ([vcs.go:116](internal/api/vcs.go:116)),
`handleGitHubRequestChanges` ([vcs.go:149](internal/api/vcs.go:149)),
`handleGitLabPostNote` ([vcs.go:246](internal/api/vcs.go:246)) each call the
accessor's `ActionMutate` methods ([access/vcs.go:81/89/123](internal/access/vcs.go:81)).
The accessor enforces RBAC + audit but has no floor check (§1), and the executor —
the only place the floor is enforced — is not on the HTTP path. The equivalent
agentic tools `github_comment`, `github_request_changes`, `gitlab_comment`
([safety/tier.go:252/258/253](internal/safety/tier.go:252)) are `ActionMutate` and so
are floor-gated when invoked through the executor. So the floor protects the tool
path but not the HTTP route.

**(c) Orphan `accessor.GitLabRequestChanges` — confirmed.**
Defined at [access/vcs.go:131](internal/access/vcs.go:131) (declares `ActionMutate`,
calls `adapter.RequestChanges`). It has **no caller anywhere**: no HTTP handler (the
GitLab routes expose only get/diff/list/notes — [vcs.go:28–32](internal/api/vcs.go:28)),
no `inProcessCoreClient` method (the inproc client has `GitHubRequestChanges` but
**no** `GitLabRequestChanges` — grep of [inproc_client.go](internal/api/inproc_client.go)
confirms), no `*client.Client` method, and no core tool (there is a
`github_request_changes` tool but no GitLab equivalent). Introduced in Phase A
(`51531cc`, 2026-05-29) as an accessor counterpart and never connected — dead since
birth.

---

## Evidence index (primary file:line)

- Route registration: [server.go:91–143](internal/api/server.go:91),
  [vcs.go:16–33](internal/api/vcs.go:16), [proposals.go:13–21](internal/api/proposals.go:13),
  [gitops.go:313–331](internal/api/gitops.go:313)
- Accessor (RBAC, no floor): [access.go:120,194](internal/access/access.go:120),
  [access/vcs.go](internal/access/vcs.go)
- Floor (executor only): [executor.go:199,215](internal/tools/executor.go:199)
- Tool classification: [safety/tier.go:229–259](internal/safety/tier.go:229)
- In-process tool path: [inproc_client.go:616,645](internal/api/inproc_client.go:616),
  [tasks.go:269](internal/api/tasks.go:269)
- Edge middleware: [rbac/middleware.go:78](internal/rbac/middleware.go:78),
  [cmd/joe/server.go:710,721](cmd/joe/server.go:710)
- Client callers: [cmd/joe/main.go:145,228,269,524](cmd/joe/main.go:145),
  [cmd/joe/incident.go:67](cmd/joe/incident.go:67)

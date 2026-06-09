# Security Architecture Direction

> **Status: DESIGN INTENT — decided, NOT yet verified against the live tree.**
> Date: 2026-06-09.
>
> This document records security/safety architecture **decisions** arrived at in
> design discussion. It is **not** a description of Joe's current implementation.
> Nothing here should be read as "Joe does this today." Several commitments are
> explicitly targets to build toward; two are open questions; all are pending a
> live-tree investigation (see §"Verification dependency"). Per the project's
> verify-first discipline, do **not** promote any item here into
> `JOE_PROJECT_KNOWLEDGE.md`, `docs/DECISIONS.md`, or a numbered D-00xx entry
> until it is built and re-derived against the code. Those numbered entries can
> reference this document once each piece lands and is verified.
>
> **Why this exists:** Joe's security mechanisms (OIDC/SSO, RBAC zones, write
> floor, incident gate, audit) were largely built organically — mechanism-first,
> without capturing the full requirement set or threat model up front. This
> record is the result of stepping back to a threat-derived design, with the
> launch goal of an **airtight observation mode** sitting on a layout that lets
> **full mode** be built without refactoring the backend.

---

## Unifying principle

Every decision below is the same move: **replace a discipline-maintained or
emergent behavior with a single computed or structural property, enforced at a
single seam, fail-closed, with no side door.** Permission, capability, and
egress are three instances of "one chokepoint, one decision, parameterized,
fail-closed." This is the property that makes the architecture extensible by
nature rather than by accretion.

---

## 1. Effective permission is ONE computed decision, not N independent gates

**Decision.** "What can Joe do right now" is a single function, not the emergent
result of which gate happens to fire first.

Inputs: **mode** (observation / full), the **principal-set**, the **target**,
the **action** (read / mutate), and the **target class** (see §3).

The write floor and the incident/captain gate stop being separate mechanisms
stacked in front of authorization and become **inputs to this one decision**.
The write floor is the **degenerate case**: `mode = observation ⇒ deny all
mutate` is simply what the function returns for that input. Same for the
incident gate.

**Why.** Today (per design discussion, not yet verified) precedence is
enforced by check-order short-circuit across independent gates. That works and
produces one clean error, but "what can Joe do" is computed nowhere — it is
emergent. Making it a single computed value is what lets **full mode** be a
*parameter change* (mode flips, the same function now lets RBAC decide) rather
than a new enforcement layer requiring a re-layering of the backend.

**This is the master decision.** Everything below is a consequence of it.

---

## 2. Effective permission = intersection(driving principal's grants, agent budget)

**Decision.** When a **human** principal is driving Joe, Joe's effective
capability is the **intersection** of (a) that human's grants and (b) a
separate, Joe-specific **agent budget** — NOT the human's full grants inherited
wholesale.

**Why.** This reverses the organic intended behavior "full mode = Joe gets the
driving user's permissions." That intended behavior is the central
prompt-injection exposure for an autonomous infra agent: a hijacked Joe would
act with the full privilege of whoever is driving. The intersection model caps
Joe's blast radius at `min(user, agent budget)`.

**The payoff — escalation becomes a property, not a rule.** Privilege
escalation stops being something enforced by a check and becomes something that
**falls out of the model**: Joe cannot mutate its own authz config even when
driven by an admin, *because that capability is never in the agent budget*, so
the intersection is empty regardless of who is driving. No admin-vs-Joe special
case, no escalation rule — just a target class (see §3) the agent budget never
grants.

**Structural hook (to verify).** The accessor's authorization is understood to
be **set-shaped** (evaluates a set of principals / union of grants). If true,
the intersection model may be expressible at the existing seam without a
structural change — i.e. full mode is config, not refactor. This is a primary
investigation question.

**OPEN QUESTION — non-human principal-sets.** This decision settles the
human-driving-Joe case. It does **not** settle what populates the principal-set
when:
- Joe acts **autonomously** (the Core Agent, no human in the loop) — is there a
  standing "agent principal" whose grants are just the agent budget, with
  nothing to intersect against?
- A **service account** drives Joe via MCP — intersection with the SA's grants,
  same shape as the human case?

The *shape* (intersection at a set-valued seam) is intended to cover all three.
What is unsettled is what fills the principal-set in the non-human cases.
**Not decided here.**

---

## 3. Target class is a first-class input

**Decision.** The computed decision (§1) takes a **target class** argument
distinguishing at least:
- **managed-system** mutation (live infra Joe operates, and the code/config that
  governs it),
- **Joe-internal-model** mutation (the graph / Joe's model of the system),
- **Joe-authz-config** mutation (policies, principals, zones, admin state).

These are governed **differently by the same function**, not by different gates:
- managed-system mutation → governed by the write floor + RBAC,
- authz-config mutation → governed by the **admin capability** (a dynamic
  capability evaluated at decision time),
- the graph is a **read** of the managed system (it is Joe's *model*, not the
  system), but *mutating* the graph is still a target class the function knows
  about.

**Why.** This is what makes internal-state-vs-infra-mutation clean instead of
special-cased, and it is the mechanism behind §2's escalation-as-property:
authz-config is a target class the agent budget never includes. **New kinds of
protected state become new target classes, not new gates.**

---

## 4. Capability is declared metadata; the airtight version is structural

**Decision.** Tools declare their action capability (read / mutate).

- **v1-acceptable:** declaration is present and **test-validated for presence**.
  Deny-by-default on omission already holds (unknown tool ⇒ Mutate). A test
  enforces every tool carries a declaration.
- **Airtight target (design toward this):** **capability-scoped interfaces** — a
  Read-declared tool is *handed* an adapter surface that has **no mutating
  methods**, so a Read tool that attempts a mutation is a **compile error**, not
  a runtime lie.

**Why.** A test can check a declaration *exists*; it cannot check the
declaration is *true*. The dangerous failure is not malicious tool code — it is
**honest miswiring**: a developer classifies a tool Read and its code calls a
mutating adapter method. The floor checks the classification (Read ⇒ skip),
the adapter call mutates anyway. Capability-scoped interfaces collapse "honest
miswiring can defeat the floor" into "honest miswiring won't compile." Because
the action is currently understood to be a **call-site argument** (e.g.
`...(..., ActionRead)`) rather than an interface-level property, reaching the
structural version is a refactor — so we **commit to designing toward it now**
even though v1 may ship test-validation, because retrofitting later is costly.

**To verify.** Whether the adapter/accessor surface is a shape where read/mutate
*could* become interface-level, or is per-method action arguments scattered
across the access layer.

---

## 5. One egress seam: configurable, OpenAI-compatible, fail-closed by default

**Decision.** There is **exactly one egress chokepoint**, it points only where
the operator configured it, and there is **no side door** around it.

- It lives at the **prompt-assembly boundary** — the point where context is
  marshaled for the provider call — because that is the only place where all
  three egress flows converge: (1) observed data → context, (2) the user's own
  prompt → context, (3) Joe's own state (graph/knowledge/prior turns) → context.
- It speaks **OpenAI-compatible chat-completions against a configurable base
  URL**, with configurable auth / headers.
- External egress is **OFF by default** (fail-closed): with no trusted endpoint
  and no verifiers configured, Joe does **not** call an external provider. This
  is the data-plane analogue of the write floor and of fail-closed-empty RBAC —
  secure by default, egress is opt-in.

**Why the OpenAI-compatible base URL is the whole requirement.** The industry
has standardized the egress seam on one protocol: base-URL-swappable,
OpenAI-compatible, bearer-auth. That single capability makes Joe work behind:
- enterprise/cloud **LLM gateways** (Azure API Management's AI gateway; and
  third-party gateways such as Bifrost / Kong / Traefik / Braintrust), and
- **in-perimeter cloud inference** (AWS Bedrock, Azure OpenAI), which are the
  "not Joe's problem" case — the data never crosses a trust boundary and the
  cloud owns governance/DLP/audit.

So "build for the AWS and Azure gateways" does **NOT** mean two cloud SDKs. It
means **one** capability: configurable OpenAI-compatible endpoint + headers.
Every gateway and in-perimeter option becomes a config value.

> **Naming caveat (researched 2026-06-09):** AWS **Bedrock AgentCore Gateway**
> is a *tool-ingress* product (turns infra APIs into MCP tools for the model) —
> it is **NOT** the egress seam and Joe should **not** build toward it for
> egress. The egress-relevant AWS surface is **Bedrock itself** as in-perimeter
> inference. On Azure the egress surface is **API Management's AI gateway**
> (OpenAI-compatible / passthrough). Do not conflate the two AWS products.

**Gateway metadata.** Gateways attribute calls by credential + headers. So
"propagate principal/purpose metadata" reduces to "send configurable headers /
a per-deployment credential" — a small standard capability, not a per-gateway
protocol. Whether this is a launch requirement or a documented gap depends on
whether any near-term deployment actually fronts Joe with a gateway (likely
document-as-known-gap at launch).

**Adapter-shape consequence (OPEN — pending investigation).** Joe currently
speaks **native** Claude + Gemini, which are *not* OpenAI-compatible. So the
OpenAI-compatible endpoint is either a **third** adapter beside the two native
ones, or — the cleaner long-term move given the gateway bet — the
**primary** egress path with native Claude/Gemini as the direct-to-public-vendor
fallback. Which one is an architecture call blocked on the current adapter
abstraction's actual shape. **Not decided here.**

---

## 6. Verifiers are interface-only at launch; the seam is built for them

**Decision.** Launch ships the **seam and the contract**, not the detector
implementations.

Ships at launch:
- the single prompt-assembly egress chokepoint (§5),
- the fail-closed default enforced **at that chokepoint** (not in config
  validation, not in the adapter),
- the **verifier interface with a frozen contract**:
  - input = the assembled outbound payload **plus the sensitivity classes** of
    its constituent data (see §7),
  - output = **pass / redact-and-pass / cancel**,
  - on error / timeout / unrecognized = **fail-closed (cancel)**, and this
    fail-closed-on-failure behavior is **non-configurable**. The admin-tunable
    choice (cancel vs. redact) applies only to **detections**, never to
    verification *failure*.
- context entries **carrying their sensitivity class** through to the seam
  (see §7) — the easily-missed structural requirement.

Defers to post-launch: the actual **PII** and **secret-detector** verifier
implementations (shipped deactivated-by-default, admin-activated, and
company-extensible — same pluggable shape as the skills system).

**Why interface-now-implementations-later is honest.** It is only honest if the
seam they plug into is real on day one. A fail-closed default enforced somewhere
*other* than where verifiers will run would make the first verifier a refactor.
The contract must be frozen at launch because the moment the interface ships it
is a published API; anyone building a custom verifier against v1 must not break.

**Verifier ≠ gateway (positioning).** A gateway / in-perimeter endpoint is a
**boundary** control — data does not cross the trust boundary (verifiable by
topology). A verifier is a **detection** control — data still crosses; it was
merely scrubbed first, and detection is never complete. Verifiers are
**defense-in-depth for the un-gatewayed long tail**, not a boundary, and should
be positioned with that honest residual. Given the gateway direction (§5), the
heavy detector implementations may never need to be Joe's responsibility at all
for the deployments that matter most.

---

## 7. Sensitivity is one property of the zone/component, read by many consumers

**Decision.** Authorization, egress, and verifiers all read the **same**
sensitivity classification, carried by the **zone / component** — not a parallel
taxonomy per concern.

- authorization reads it at the **accessor seam** ("who may read this"),
- egress / verifiers read it at the **assembly seam** ("may this data leave, and
  to where / scrubbed how").

**Structural requirement (likely the real launch deliverable hiding in §6).**
The sensitivity class must **travel with the data** from tool result, through
context assembly, to the egress seam. Today tool results are expected to flow
into context as bare content with no sensitivity tag (classification lives in
the RBAC/zone layer, which the context assembler has no reason to consult). So
"built for verifiers" specifically requires **context entries to carry their
provenance / sensitivity class**. If deferred, the first verifier becomes a
refactor of the entire context pipeline rather than a plugin. This is invisible
if one only thinks about the on/off egress default (which needs no per-data
classing) and essential the moment a verifier acts per-sensitivity.

---

## 8. Incident regime / captain gate — post-launch, and an input not a layer

**Decision.** The incident/captain gate is **post-launch as a feature**, and
when it returns it must land as an **input to the computed decision (§1)**, not
as a surviving independent layer.

**Why post-launch.** The captain gate can **only deny** mutations (never
elevate) — RBAC authority is identical in and out of incident mode. It therefore
only bites when mutations are possible at all, which is **full mode**. In
observation mode the write floor already denies every mutation, so the captain
gate is **redundant** — it cannot deny what is already denied. Launch is airtight
observation mode, so the captain gate adds nothing at launch.

**Why it is still in scope for this architecture.** It is one of the gates §1
collapses. Today (per design discussion, not yet verified) precedence is
"floor > incident > RBAC" enforced as **independent layers** by check-order. The
target design makes "what Joe can do right now" a single computed function;
the incident regime must become an **input** to that function (an
incident-active flag that can flip an otherwise-allowed mutation to denied), not
a layer that survives outside it. If it is rebuilt as a standalone layer, §1's
unification is only partial and full mode re-fragments into the old organic
stack. **One line of intent recorded here so future work does not rebuild it as
a layer.**

**Relationship to the §2 open question.** The captain gate binds mutations to a
**captain session** (the human who declared / holds the incident). That is a
**principal-set** constraint: during an incident Joe's effective permission is
not merely `intersection(driving principal, agent budget)` — it is further
constrained by "is there an active captain, and is this principal it." So the
captain gate is the **incident-mode case of the §2 open question** (what
populates and composes the principal-set), not a separate problem. When the
captain feature is revisited post-launch, it should be designed as that case of
§2, not as a fresh feature.

**Investigation note.** No separate captain investigation is needed. Session A
(the permissions/capability seam investigation) already surfaces the captain
gate's current shape when it determines whether floor/incident/RBAC are
independent gates or could be one decision — the captain gate is precisely an
instance of "an independent gate," so A's evidence on it feeds directly here.

---

## What the write floor does and does NOT cover (threat-model note)

The write floor, when up (observation mode), makes it **deterministically
impossible** for any Mutate-classified tool to execute, regardless of LLM
decision, prompt injection, or malicious skill. This neutralizes — *in
observation mode, for mutations* — three agent-specific threats: autonomous
destructive action, injection-driven mutation, and malicious-skill mutation.

It explicitly does **NOT** cover:
- **Reads / egress.** The floor is mutate-only. In observation mode an injection
  can still drive Joe to *read* sensitive data and exfiltrate it via a
  read-classified channel — including by placing it in the next provider call.
  Observation mode is **"cannot mutate the managed system,"** NOT
  "data-confidentiality." (This is why §5–§7 exist as separate controls.)
- **Full mode.** When the floor is down, the LLM's judgment is load-bearing
  again and §2 (intersection) is the actual blast-radius control.
- **Classification correctness.** The guarantee is only as good as the
  read/mutate classification and the single-seam property (§4). A tool
  misdeclared Read that mutates walks through the floor; a future mutation path
  that bypasses the gated executor defeats it. The guard test defends the
  *symbol*; it does not defend the *invariant* that all mutation flows through
  the gated seam.

---

## Verification dependency

**None of the above is verified against the live tree.** The following must be
confirmed before any item is promoted to landed-and-verified status (numbered
D-00xx / `JOE_PROJECT_KNOWLEDGE.md`):

1. **Single mutation seam / capability could become structural.** Does every
   managed-system mutation route through one gated executor seam, and is the
   adapter surface a shape where read/mutate could become interface-level (vs.
   scattered per-method action arguments)? (§1, §4)
2. **Accessor principal-set shape.** Can the accessor express
   "effective grants = f(principal-set, agent budget)" — e.g. intersection —
   at the existing seam, or does that force a structural change? (§2)
3. **Egress seam + adapter shape.** Is there exactly one egress chokepoint; can
   the LLM adapter speak OpenAI-compatible chat-completions against a
   configurable base URL with configurable auth/headers; and is that a third
   adapter or a re-centering of the existing native Claude/Gemini adapters? (§5)
4. **Sensitivity provenance in the context pipeline.** Do (or can) context
   entries carry their source zone / sensitivity class through to the
   prompt-assembly seam? (§6, §7)
5. **Gate composition (floor / incident / RBAC).** Are these enforced as
   independent layers by check-order, or could they be inputs to one computed
   decision? The captain gate's current shape is the concrete probe. (§1, §8 —
   surfaced by Session A's gate-composition tracing)

The two **open design questions** (non-human principal-sets, §2;
primary-vs-third adapter, §5) are partly blocked on these facts. The incident
regime (§8) is the incident-mode case of the first of those.

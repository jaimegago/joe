---
title: Agent identity and authentication
status: draft
description: How Joe authenticates as its own non-human identity, and the provenance and governance that bound every action it takes.
---

> **Draft — target design, not yet shipped.** This page describes the identity and
> authentication stance Joe is settling on as design-of-record. It deliberately describes
> the *target*; parts of it are not yet implemented. In particular, the Kubernetes
> credential path Joe ships today is the **kubeconfig-or-in-cluster locator** (the
> `kubeconfig-exec` provider wired at `internal/credential/wiring.go:46`, resolving an
> in-cluster service account or a kubeconfig file at `internal/credential/kubeconfig_exec.go`),
> established by **D-0026** and re-confirmed by **D-0059**. The target Kubernetes section
> below replaces that path with two native bearer methods; until the implementing ADR and
> code land, treat this page as direction, not shipped behaviour. It is held outside the
> published documentation surface for that reason.

# Agent identity and authentication

## An agent is a third identity class

A human authenticates by assuming presence. The whole shape of human authentication takes
for granted that someone is there: an interactive login, a multi-factor prompt to answer,
a re-authentication when a short-lived credential lapses. Consent is obtained in the
moment, from a person who is present to give it.

A service authenticates on fixed scope. It is trusted not because anyone is watching but
because of where it was deployed: a thing placed in a particular position to do one
bounded job, and trusted by virtue of that placement to act within it unmediated.

An agent is neither. It runs unattended, the way a service does — no person sits behind
each of its actions waiting to approve them. But its actions are open-ended and
human-directed at runtime, the way a human session's are — it is told what to do as it
goes, across a surface no deployment scope drew in advance. So it cannot lean on human
presence to obtain consent, because no human is present at the moment of action. And it
cannot inherit the service's trusted-by-deployment pass that lets a service act
unmediated, because its actions were never bounded by a deployment in the first place.
What is left is that an agent's safety must come from a mediation layer the agent
enforces on itself.

## Provenance: the authority an action traces back to

Every action Joe takes traces back to some authority. That authority is its
**provenance**, and it comes in two modes.

An action is **delegated** when a human originated it. The human asked; Joe carried it
out. The originator is that human, and the actor is Joe.

An action is **autonomous** when Joe originated it itself. Nothing external asked; Joe
decided to act as part of its own work. The originator and the actor are both Joe. This
is the characteristic mode of discovery and observation — the agent looking around its
managed estate on its own initiative.

In both modes **Joe is the actor on the wire.** Only the originator varies. And
provenance is orthogonal to whether an action reads or mutates: a read can be delegated
or autonomous, and a mutation can be delegated or autonomous, just the same. The
read/mutate axis answers *what kind of effect*; provenance answers *on whose authority* —
two independent questions about one action.

This split is not novel; it mirrors a distinction the broader standards work has already
drawn. The foundational pattern, set out in **RFC 8693**, separates *delegation* — where
the original subject and the acting party are both preserved together in a composite —
from *impersonation*, where the actor is simply replaced by the subject. Joe's delegated
mode is the delegation case: the human and Joe both kept, distinct, in a composite
record. Joe's autonomous mode is the simpler one still: the agent acting as its own
subject, with no delegation at all.

## Joe's stance

The stance follows from those two ideas and can be stated plainly.

**Joe authenticates only as its own non-human identity. Joe never authenticates as a
human and never uses the human authentication path. Joe never ingests a human's
kubeconfig and never assumes another identity through impersonation.**

When a human is the originator of an action, that human is recorded only inside Joe, as a
provenance assertion of originator, actor, and action, derived from the authenticated
session's creator principal, and is never transmitted to the managed system. The managed
system sees only Joe's own service identity.

This is deliberate. The human principal stays Joe-internal, so accountability for who
asked lives in Joe's own audit and session record rather than on the wire. The managed
system is never asked to reason about which human stood behind a request; it only ever
sees Joe, and the question of *who asked Joe* is answered where Joe can answer it
completely — in the session that originated the action.

## Three planes that are never collapsed

Three questions sit behind every action, and Joe keeps them on separate planes.

**Identity** answers *who Joe is allowed to be on a system* — the non-human credential it
presents at the edge of the managed system.

**Provenance** answers *on whose authority Joe is acting* — delegated or autonomous, the
originator recorded as an assertion. It lives only inside Joe.

**Governance** is the floor that answers *what Joe may do right now*, and it sits in front
of every action regardless of the other two.

The invariant binding them is that **a valid credential never implies a permitted
action.** Authentication and authorization are separate; the floor is independent of
credential validity. Holding a credential that a system accepts says nothing about
whether Joe is allowed to use it for the action at hand — that is the floor's question,
asked every time. This separation is precisely what makes Joe an agent rather than an
automation holding broad credentials: an automation's credential *is* its permission;
Joe's credential is only its identity, and permission is decided independently in front
of every action.

## Credentials are resolved, not stored

A component's non-secret coordinates — its endpoint, its certificate-authority bundle,
its default namespace — are recorded. The credential material itself is not. It is
resolved from the environment at the moment of use and never persisted. Joe holds the
pointer to a credential, never the credential, so the secret lives where it already lived
and Joe's records carry nothing that needs guarding beyond the reference.

## Where this sits in the landscape

This stance is not isolated. There is a consensus forming about how agents should hold
identity, and it is worth being precise about where Joe aligns with it and where Joe
deliberately diverges — as a matter of Joe's own philosophy, not a judgement of anyone
else's.

**RFC 8693** gives the foundation: the delegation-versus-impersonation distinction and
the composite-actor pattern. Joe's provenance assertion follows that composite-actor
pattern directly — the human and Joe both preserved, distinct, in one record.

**Kubernetes impersonation** offers a native mechanism for one identity to act as
another, carried in impersonation headers. Joe declines to use it. Impersonation replaces
the actor with the human, and that replacement is exactly the human-path assumption Joe
refuses: the managed system would see the human, not Joe, and accountability would move
onto the wire and out of Joe's record.

And the **emerging standardization work on agent identity** is converging — independently
of Joe — on the same shape Joe adopts: that agents should hold first-class non-human
identities rather than borrow a human's; that delegation should preserve the human as a
distinct principal rather than replace them; that provenance should be anchored to a
human and remain auditable; and that an agent's effective authority for a given task
should be attenuated toward least privilege. This is active work in the standards
community, still moving, and so it is pointed to here by its shape rather than by the
names and revisions of particular drafts.

## The refusal, as principle

If a managed system decides that agents should impersonate humans, Joe declines. It
authenticates as a service identity and carries the human as provenance rather than
wearing the human's identity.

What Joe refuses is impersonation in the narrow sense of **identity replacement** — the
actor on the wire becoming the human. It is worth being precise that this is not a
blanket refusal of delegation. A genuine future delegation primitive — one that kept Joe
as the authenticated actor while carrying the human as a distinct principal, the
composite-actor shape rather than a substitution — would not violate the stance at all.
No such primitive exists today, which is why, today, the refusal and the stance amount to
the same thing.

## Kubernetes, concretely

*This section describes the target design. It is not what Joe ships today; the current
shipped path is the kubeconfig-or-in-cluster locator noted at the top of this page.*

Joe's launch direction for Kubernetes is exactly two transport authentication methods,
both implemented natively by Joe, and both authenticating only as a non-human identity.

The first is a **static bearer** method: a long-lived bearer token, injected as an
`Authorization: Bearer` header. This is the method for OpenShift, and for self-managed or
local clusters reached through a ServiceAccount token.

The second is an **Entra exchange** method: Joe itself performs an Azure Entra OAuth2
token exchange to mint a short-lived bearer token, for AKS.

Client-certificate authentication is **permanently excluded**, as a matter of stance
rather than convenience — a client certificate is a human authentication path, and Joe
does not take the human path.

## The model generalizes

The model — a first-class non-human identity, a Joe-internal provenance assertion, and a
governance floor in front of every action — is not specific to Kubernetes. It is how Joe
means to authenticate and act against every kind of managed component. Kubernetes is the
first place it is made concrete, not the boundary of where it applies.

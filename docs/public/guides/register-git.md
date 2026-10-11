---
title: Register a Git repository
weight: 17
description: Bring a Git repository under Joe's management through the web UI — register it with its clone URL, promote it with a token reference or as public, and confirm Joe can clone it.
---

# Register a Git repository

This how-to walks you, click by click, through bringing one Git repository under Joe's
management using the web UI. You will **register** the repository with its clone URL
(it lands inert), **assign it a zone**, **promote** it — with a token reference for a
private repository, or explicitly with no credential for a public one — and run a
**connectivity test** to confirm Joe can clone it. No daemon restart is needed.

A `git` component is one repository. Joe clones it over HTTPS into its own data directory
and searches its files when you ask about the code. The copy is brought up to date each
time Joe connects to the repository: at promotion, at each connectivity test, and when the
daemon starts.

For a GitHub or GitLab account's pull requests and merge requests, register a `github` or
`gitlab` component instead — see
[Register a GitHub, GitLab, or PagerDuty component](../register-token-components/). The
two work together: a `git` component can name the `github` or `gitlab` component that
hosts it.

For *why* the lifecycle is split the way it is, see
[The component lifecycle](../../concepts/component-lifecycle/).

## Before you start

- A running Joe with the web UI open and a signed-in session — see
  [The web UI and human login](../web-ui/).
- An **admin principal**. Registering, assigning a zone, and promoting are admin-gated.
  Running the connectivity test does not require admin.
- The repository's **HTTPS clone URL**, e.g. `https://github.com/acme/payments.git`.
- For a **private** repository, an access token that can read it — a GitHub or GitLab
  token works — set in an environment variable **where the Joe daemon runs**, named
  `JOE_GIT_<LABEL>`:

  ```sh
  export JOE_GIT_PAYMENTS="github_pat_..."
  ```

  Joe sends the token as the password of an HTTPS login. It reads its own process
  environment, so the variable has to be present when the daemon starts; if you set it
  later, restart Joe before you promote. A public repository needs no token.

## Step 1 — Open the Components page

In the left sidebar, click **Components**.

## Step 2 — Register the repository (admin) — it lands inert

> **Admin only.** The **+ Register Component** button appears only for an admin principal.

1. Click **+ Register Component** in the top-right of the page.
2. In the dialog:
   - **Name** — a human label, e.g. `Payments repo`.
   - **Component ID** — prefilled from the Name (e.g. `payments-repo`) and read-only by
     default; click **Edit** beside it to set your own. It is a permanent identifier: 1–63
     lowercase letters, digits, and hyphens, starting and ending with a letter or digit.
   - **Type** — choose **`git`**. Two more fields appear.
   - **Repository URL** — the HTTPS clone URL. Required. It carries no credential; a
     private repository gets its token reference at promotion.
   - **Hosting provider** *(optional)* — the registered `github` or `gitlab` component that
     hosts this repository, so its pull-request surface can be found alongside it. This is
     for discovery only: it grants no access and changes no permissions. Leave it unset if
     you have not registered one.
3. Click **Register**.

The new row appears with its zone marked **⚠ unassigned** and its arming state **inert**.
Joe has not contacted the repository yet.

Joe tracks the repository's default branch. The dialog does not offer a branch choice; to
track another branch, register through the API and set `branch` in the component's
`config` — see [Components](../../components/).

## Step 3 — Assign the component a zone (admin)

1. In the left sidebar, click **Zones**.
2. In the **unassigned components** panel at the top, find the repository you just
   registered.
3. Pick its zone from the **Assign Zone** dropdown next to it.

For what zones mean, see
[RBAC, zones, and read posture](../../concepts/rbac-zones-and-read-posture/).

## Step 4 — Promote the component (admin)

Promotion is the single governed transition from inert to **armed**. For a repository it
records how Joe authenticates the clone — and choosing *no* authentication is a decision
too, recorded the same way.

> **Admin only.** The **Promote** button is shown only to an admin principal.

1. Back on the **Components** page, click the repository's row to open its detail card.
2. Click **Promote**.
3. Open the **Credential** dropdown and choose one:
   - **Credential reference** — for a private repository. Then pick the variable:
     - If Joe can see variables with the `JOE_GIT_` prefix, the **Credential reference**
       dropdown lists them by label. Choose yours.
     - Otherwise, or after clicking **Don't see it? Enter a label**, type the label into
       **Reference label** — `PAYMENTS`, not the full name. The dialog shows the name it
       resolves to and warns you if that variable is not currently set where Joe runs. You
       can still arm, but the clone cannot succeed until the variable exists and Joe has
       been restarted to see it.
   - **No credential (public repository)** — for a repository anyone can read. The dialog
     explains what you are granting: Joe will reach the repository over the network
     **unauthenticated**, fetch its contents, and write a local copy. Choose this only for
     a repository that is genuinely public.
4. Click **Continue**, then confirm with **Promote**. The confirmation names the grant —
   a credentialed connection, or unauthenticated access for a public repository — as a
   privileged, audited change recorded against you.

Each component needs **its own** variable; promotion refuses a name another component
already uses.

The row's arming state flips to **armed**, and Joe clones the repository straight away. If
that first clone fails, the component stays armed, and the next step is how you retry.

## Step 5 — Confirm Joe can clone it

> Running the test does **not** require an admin principal.

1. In the component's detail card, click **Test Connection**.
2. On success you see **connection successful**, and the component's status moves to
   **connected**.

The test does real work: if Joe holds no local copy it clones the repository, and if it
does it pulls the latest commits. So success means Joe can reach the repository and read
it with the credential choice you made — and running the test again is how you bring
Joe's copy up to date between restarts.

If the test fails, the message says why. `clone repo: …` covers an unreachable URL and a
refused login — a private repository armed with **No credential** fails here, as does a
token that cannot read it. `resolve credential: …` means the referenced variable is not
set where Joe runs. Fix the cause and test again, or click **Re-arm** to change the
credential choice.

## Step 6 — Ask Joe about the code

Open **Chat** and ask something that needs the repository's contents, for example:

> Search payments-repo for where the retry timeout is configured.

Joe searches the files of the repository at a pinned commit and tells you which commit it
searched. Reading the repository grants no permission to change it; that is governed
separately by the write floor and RBAC — see
[Observation mode and the write floor](../../concepts/observation-mode-and-the-write-floor/).

## Where to go next

- Pull requests and merge requests for the hosting account →
  [Register a GitHub, GitLab, or PagerDuty component](../register-token-components/)
- Every connectable type and its credential mechanism →
  [Connectable systems](../../components/connectable-systems/)
- Why registration and promotion are separate governed steps →
  [The component lifecycle](../../concepts/component-lifecycle/)

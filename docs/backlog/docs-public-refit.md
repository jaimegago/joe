Public docs refit — deferred follow-ups
Status: open

The `docs-public-refit` session (D-0069, D-0070) reworked the capability Concepts page
into `concepts/action-model.md` (binary Read/Mutate spine, shipped mutation surface
documented), renamed the `integrations/` section to `components/`, retitled the
"Components and promotion" Concepts page to "The component lifecycle", and replaced the
copilot-for-platform-engineers / HTTP-daemon copy with the self-hosted open-source
AI-agent framing. The following thread is deliberately deferred and keeps this item open.

## Deferred items

- **Elevate RBAC to a first-class published section.** The rename amended the D-0052
  nine-section taxonomy for one section only (Integrations → Components) and did not add or
  remove a section. Giving RBAC / zones / read-posture its own top-level `docs/public`
  section — rather than living only in the `concepts/rbac-zones-and-read-posture.md`
  explanation page — is deferred until the post-launch RBAC work lands (see
  `read-posture-latch.md` and `rbac-v2.md`). When that work settles the full-mode RBAC
  surface, revisit whether the taxonomy should grow a dedicated section and record the
  taxonomy change as its own decision.

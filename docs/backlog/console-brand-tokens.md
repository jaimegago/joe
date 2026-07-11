# Console brand token layer

Status: open

## Context

Joe now has a finalized brand identity: the Watt flyball governor mark; a palette
of ink `#211D19`, rust `#C05F2E`, a lightened rust for dark surfaces `#D97B4A`,
brass `#D9A441`, cream `#F5F0E4`, and slate `#4A6670`; a typographic system of
Zilla Slab for display, Source Sans 3 for body, and IBM Plex Mono for code; and a
verbal identity including the "Joe Operates Everything" backronym. The brand
assets live with the joeagent.dev site work.

This item covers deriving a product design token layer for the operator console
from that brand system. It is deferred post-launch deliberately: the console
works, retheming is aesthetic rather than safety-relevant, and launch takes
priority.

## Scope, when picked up

Define semantic CSS custom properties consumed by the console rather than raw
brand colors. The required derivations, with their rationale preserved so the
reasoning is not lost when the work is picked up:

- **Interactive accent.** Rust cannot be the interactive accent as-is, because
  orange reads as "warning" in ops UIs. Either an interactive accent must be
  chosen, or rust must be reserved for brand moments.
- **Semantic status colors.** Brass collides with warning amber, so semantic
  success, warning, danger, and info colors must be deliberately assigned (slate
  is the info candidate) and harmonized with the warm palette.
- **Neutral surface ramp.** A neutral surface ramp is needed in both light and
  dark directions — surface, raised surface, borders, muted text — since the
  brand palette only supplies the cream and ink poles. Decide dark-first vs
  light-first vs both, noting that operator-tooling convention skews dark.
- **Typography scoping.** Zilla Slab is scoped to display moments only (login,
  page titles) and never to dense data surfaces. Plex Mono and Source Sans 3
  transfer directly.
- **A designed brand moment.** One brand moment lives inside the token system:
  the incident regime banner is where rust may run at full intensity.

## Explicitly out of scope for launch

Everything above. The only pre-launch brand touches in the product are the
favicon and the header mark, which are tracked separately.

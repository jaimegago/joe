# Register Component dialog — stale state on reopen, and a Type-unlinked example

Status: done — both defects fixed (session `register-component-dialog`): a key-prop
remount from `ComponentsPage` resets the dialog's fields on reopen, and a type-keyed
`id`/`name` placeholder map (with a generic fallback for unmapped types) replaces the
static Prometheus example. Verified in the browser (open/fill/cancel/reopen; kubernetes
and github placeholders; an unmapped type falling to the generic fallback).
Priority: now

Two defects observed walking the v0.2.0 Quickstart against the running web UI, both
in `ui/src/components/admin/ComponentRegisterForm.tsx`.

## 1. Form state is not reset when the dialog reopens

`id`/`type`/`name` are `useState('')` (`ComponentRegisterForm.tsx:42-44`) initialized
once on mount. The dialog is rendered persistently — not conditionally mounted, no
`key` prop — at `ui/src/pages/ComponentsPage.tsx:429-434`; only `open` toggles. No
`useEffect` keyed on `open` clears the fields. Result: fill the form, Cancel
(`onOpenChange(false)` at `ComponentRegisterForm.tsx:117`), reopen — the previously
typed values are still there.

Per CLAUDE.md's UI verification note, this class of bug (react-query cache /
component-lifecycle state across mounts) is exactly what green Vitest misses and only
reproduces against the real running app — verify any fix by driving the actual dialog
open/cancel/reopen sequence in the browser, not just a unit test.

## 2. The ID/Name placeholders are a single static example, unlinked to Type

`ComponentRegisterForm.tsx:87` (`placeholder="prod-prometheus"`) and `:112`
(`placeholder="Production Prometheus"`) are hardcoded literals, independent of the
`type` state (`:43`) or the fetched `types` array (`:53`, from
`ui/src/api/components.ts:29-32`). A reader on the Kubernetes rails (e.g. the
Quickstart → register-kubernetes guide path) selects `kubernetes` in the Type select
(`:93-104`) and still sees a Prometheus-flavored example, which reads as unrelated to
the type just chosen.

No per-type mapping exists nearby to key a fix against: `types` is a flat `string[]`
from the backend with no enum/object literal alongside it.

## Proposed direction

- Reset `id`/`type`/`name` to `''` on dialog open (an effect keyed on `open`, or a
  simpler `key`-prop remount from the parent).
- Drive per-type `id`/`name` placeholder examples from a small type-keyed map defined
  beside the type definitions the form already reads, with a neutral generic fallback
  (e.g. `component-id` / `Component name`) for any type not in the map — so an
  unmapped type never regresses to a wrong-domain example.

Both are UI-only; no backend change implied. Not fixed in this session (docs-only
session, no `ui/` source change in scope).

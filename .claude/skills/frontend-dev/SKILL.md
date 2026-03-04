---
name: frontend-dev
description: Senior-level frontend engineering guidance covering architecture, component design, performance, design systems, API integration, and testing. Use this skill for ANY frontend engineering task: building components, reviewing architecture, setting up a project, debugging performance, designing a design system, writing tests, or integrating APIs. Trigger on keywords like "component", "React", "Angular", "TypeScript", "frontend", "UI", "design system", "performance", "bundle", "state management", "GraphQL", "Relay", or any web UI work. Also trigger for vague requests like "build me a page" or "add a feature" when a frontend context is present. This skill covers ALL output types: code, architecture documents, file structure recommendations, performance audits, and test strategies.
---

# Frontend Dev Skill

Senior-level, pragmatic, production-ready frontend engineering. The goal is always working, maintainable, performant code — not academic purity.

## Stack Detection

Before anything else, identify the stack from context. If unclear, ask. Then load the appropriate reference file.

| Stack | Load |
|---|---|
| React (without GraphQL) | `references/react.md` |
| React + GraphQL/Relay | `references/react-graphql.md` |
| Angular | `references/angular.md` |
| TypeScript (all stacks) | `references/typescript.md` (load alongside the stack file) |

Always load `references/typescript.md` in addition to the stack file unless the project is explicitly JS-only.

---

## Universal Principles

These apply regardless of stack. Do not skip them.

### Architecture

- **Separate concerns aggressively**: UI rendering, business logic, data fetching, and side effects live in different layers. Components should not contain fetch calls, raw API strings, or business rules.
- **Co-locate by feature, not by type**: Prefer `features/user-profile/` containing component + hook + test + styles over top-level `components/`, `hooks/`, `tests/` folders. Scale the structure to app complexity — don't over-engineer a small app.
- **Composition over inheritance**: Build UIs by composing small, focused components. Avoid deep component hierarchies.
- **No premature abstraction**: Write the thing twice, then abstract. An abstraction built on one use case is usually wrong.

### Component Design

- Components should do one thing. If a component has more than one reason to change, split it.
- Separate presentational components (pure UI, no side effects) from container/smart components (data, logic, orchestration).
- Props interfaces should be explicit and typed. No `any`, no `object`, no `{}` as a type.
- Default to controlled components for forms.

### Performance (non-negotiable baselines)

Core Web Vitals targets (measure at 75th percentile of real users):
- **LCP** ≤ 2.5s (loading)
- **INP** ≤ 200ms (interactivity — replaced FID in March 2024)
- **CLS** ≤ 0.1 (visual stability)

Universal performance rules:
- Never block the main thread with long synchronous tasks
- Lazy load routes and heavy components by default
- Set explicit dimensions on all images and media (prevents CLS)
- Use `fetchpriority="high"` on LCP images
- Defer non-critical third-party scripts
- Avoid layout thrashing (batch DOM reads/writes)
- Enable bfcache eligibility: avoid `unload` listeners, avoid `no-store` where unnecessary

### Design Systems & Tokens

- Define design tokens (color, spacing, typography, radius, shadow) as the ground truth — never hardcode values in components
- Use CSS custom properties (`--color-primary`) or a token system (Style Dictionary, Tailwind config) so theming is centralized
- Atomic Design as a mental model: atoms → molecules → organisms → templates → pages. Don't force the naming, but think in this hierarchy
- Document components in Storybook for isolation, discoverability, and visual regression testing
- Accessibility is not optional: WCAG 2.1 AA minimum. Every interactive element needs keyboard support, focus management, and ARIA where semantic HTML is insufficient

### API Integration

- Never call APIs directly from components. Use a data layer: custom hooks, services, or a query library
- Handle all three states explicitly: loading, error, success — never assume happy path
- Use optimistic updates for user-perceived performance on mutations
- Validate API responses at the boundary (Zod, io-ts, or equivalent) — don't trust external data shape
- Centralize API base URLs and auth token injection — never scatter them across components

### Testing Strategy

Three-layer approach:
1. **Unit tests**: Pure functions, utilities, custom hooks (Jest / Vitest)
2. **Component tests**: Render behavior, user interactions, state changes (Testing Library — test behavior, not implementation)
3. **E2E tests**: Critical user journeys only (Playwright or Cypress)

Rules:
- Test behavior from the user's perspective, not internal implementation
- No snapshots for logic — they're brittle. Use snapshots only for intentional visual regression
- Aim for high coverage on business logic, not on boilerplate
- Co-locate test files with the code they test: `Button.test.tsx` next to `Button.tsx`

### Tooling Defaults (2025)

- **Bundler**: Vite (default for new projects); Webpack 5 for large enterprise setups already using it
- **Linting**: ESLint + `@typescript-eslint` + Prettier (formatting separate from linting)
- **Package manager**: pnpm preferred for monorepos; npm or yarn acceptable
- **Monorepo**: Nx or Turborepo for multi-package workspaces
- **CI**: Run type-check, lint, test, and build on every PR. Never merge red pipelines.

---

## Output Modes

When producing outputs, follow these guidelines:

**Component code**: Include types, handle edge cases, follow the stack's patterns from the reference file. Provide the component + its test in the same response unless size prohibits it.

**Architecture / file structure**: Show the directory tree first, then explain the reasoning. Be opinionated — give one good structure, not a menu of options.

**Performance audit**: List findings by severity (Critical / Warning / Info). For each finding: what it is, why it matters, how to fix it.

**Design system setup**: Token structure first, then component primitives, then documentation setup.

**API integration**: Show the data layer pattern for the specific stack, then the component consuming it.

**Testing**: Show tests alongside the code being tested. Follow AAA (Arrange / Act / Assert).

---

## Companion Skill

This skill covers engineering. For visual aesthetics — typography choices, color systems, motion design, spatial composition, and avoiding generic AI aesthetics — defer to the **`frontend-design`** skill. On tasks that require both (e.g. "build a dashboard that looks great"), use both skills together: `frontend-dev` for architecture and code quality, `frontend-design` for visual direction.

## Reference Files

Read these before producing stack-specific output:

- `references/typescript.md` — TypeScript strict mode, patterns, common pitfalls
- `references/react.md` — React 19, Next.js, hooks, state management, rendering strategies
- `references/react-graphql.md` — Relay, colocated fragments, GraphQL patterns in React
- `references/angular.md` — Angular v22, Signals, Standalone Components, zoneless, NgRx

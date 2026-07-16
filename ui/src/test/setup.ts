import '@testing-library/jest-dom/vitest';

// jsdom does not implement scrollIntoView; components that auto-scroll (e.g.
// the chat transcript) call it on mount/update. Provide a no-op so those
// components can render under test.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => undefined;
}

// jsdom does not implement the Pointer Capture API, which Radix UI primitives
// (e.g. <Select>) call on pointer interactions. Without these, opening a Select
// under userEvent throws "hasPointerCapture is not a function". Provide no-ops so
// Radix-based controls are interactable in tests.
if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => undefined;
  Element.prototype.releasePointerCapture = () => undefined;
}

// jsdom does not implement ResizeObserver, which ReactFlow constructs on mount to
// track its container size. Without it the graph surface throws "ResizeObserver is
// not defined" before rendering anything. A no-op is enough: jsdom reports zero
// dimensions regardless, and no test here asserts on measured layout.
if (!('ResizeObserver' in globalThis)) {
  globalThis.ResizeObserver = class {
    observe = () => undefined;
    unobserve = () => undefined;
    disconnect = () => undefined;
  };
}

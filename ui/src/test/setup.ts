import '@testing-library/jest-dom/vitest';

// jsdom does not implement scrollIntoView; components that auto-scroll (e.g.
// the chat transcript) call it on mount/update. Provide a no-op so those
// components can render under test.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => undefined;
}

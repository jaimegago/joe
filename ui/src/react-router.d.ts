import type { To, NavigateOptions } from 'react-router';

// React Router types navigate() as `void | Promise<void>` to cover both routers.
// Joe mounts <BrowserRouter> (declarative mode), where it is always void; the
// union otherwise trips no-floating-promises at every call site.
declare module 'react-router' {
  interface NavigateFunction {
    (to: To, options?: NavigateOptions): void;
    (delta: number): void;
  }
}

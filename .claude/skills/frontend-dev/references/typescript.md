# TypeScript Reference

Always-on for any TypeScript project. Load alongside the stack-specific reference file.

## tsconfig Baseline

Every project starts with strict mode. No exceptions.

```json
{
  "compilerOptions": {
    "strict": true,
    "noImplicitAny": true,
    "strictNullChecks": true,
    "strictFunctionTypes": true,
    "strictPropertyInitialization": true,
    "noImplicitThis": true,
    "noImplicitReturns": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "forceConsistentCasingInFileNames": true,
    "target": "ES2020",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "esModuleInterop": true,
    "skipLibCheck": true
  }
}
```

`strict: true` enables 7 flags at once. The additional flags above (`noImplicitReturns`, `noUnusedLocals`, etc.) are not included in `strict` but are strongly recommended.

For existing codebases that can't enable strict all at once: enable flags incrementally, starting with `strictNullChecks`, then `noImplicitAny`.

---

## Type System Rules

### Never use `any`
`any` disables the type checker. Use `unknown` when the type is genuinely unknown, then narrow:

```typescript
// Bad
function process(data: any) { return data.name }

// Good
function process(data: unknown): string {
  if (typeof data === 'object' && data !== null && 'name' in data) {
    return String((data as { name: unknown }).name)
  }
  throw new Error('Invalid data shape')
}
```

For API boundaries, use a validation library (Zod) instead of manual narrowing.

### Prefer `interface` for object shapes, `type` for unions and aliases

```typescript
// Object shapes → interface (extensible, better error messages)
interface User {
  id: string
  name: string
  email: string
}

// Unions, intersections, mapped types → type alias
type Status = 'idle' | 'loading' | 'success' | 'error'
type AdminUser = User & { role: 'admin' }
```

### Discriminated unions over optional fields

```typescript
// Bad — both fields might be undefined, forces runtime checks everywhere
interface Result {
  data?: User
  error?: Error
}

// Good — exhaustive, TypeScript enforces handling all cases
type Result =
  | { status: 'success'; data: User }
  | { status: 'error'; error: Error }
  | { status: 'loading' }
```

### Generics with constraints

Be explicit about what a generic must provide. Don't use unconstrained `T` when you know the shape:

```typescript
// Bad — T could be anything, no guarantee of .id
function findById<T>(items: T[], id: string): T | undefined {
  return items.find((item: any) => item.id === id)
}

// Good
function findById<T extends { id: string }>(items: T[], id: string): T | undefined {
  return items.find(item => item.id === id)
}
```

### `as const` for fixed data structures

```typescript
const ROUTES = {
  home: '/',
  dashboard: '/dashboard',
  settings: '/settings',
} as const

type Route = typeof ROUTES[keyof typeof ROUTES] // '/' | '/dashboard' | '/settings'
```

### Runtime validation at API boundaries

TypeScript types disappear at runtime. Validate external data:

```typescript
import { z } from 'zod'

const UserSchema = z.object({
  id: z.string(),
  name: z.string(),
  email: z.string().email(),
  createdAt: z.string().datetime(),
})

type User = z.infer<typeof UserSchema>

// At API boundary
const user = UserSchema.parse(response.data) // throws if invalid
// or
const result = UserSchema.safeParse(response.data) // returns { success, data/error }
```

---

## Common Patterns

### Utility types worth knowing

```typescript
Partial<T>         // All fields optional
Required<T>        // All fields required
Pick<T, K>         // Subset of fields
Omit<T, K>         // All fields except K
Readonly<T>        // Immutable version
Record<K, V>       // Object with typed keys/values
NonNullable<T>     // Removes null | undefined
ReturnType<F>      // Return type of a function
Parameters<F>      // Tuple of function parameter types
```

### Explicit return types on public API functions

Internal helpers: let TypeScript infer. Public functions (exported, component event handlers, service methods): annotate explicitly. This prevents accidental breaking changes and serves as documentation.

```typescript
// Internal util — inference is fine
const double = (n: number) => n * 2

// Public service method — explicit is better
export function fetchUser(id: string): Promise<User> {
  return api.get(`/users/${id}`).then(res => UserSchema.parse(res.data))
}
```

### Type guards

```typescript
function isUser(value: unknown): value is User {
  return UserSchema.safeParse(value).success
}
```

### Template literal types for string patterns

```typescript
type HTTPMethod = 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH'
type Endpoint = `/api/${string}`

function request(method: HTTPMethod, endpoint: Endpoint) { ... }

request('GET', '/api/users')     // ✅
request('FETCH', '/api/users')   // ✅ type error
request('GET', '/users')         // ✅ type error
```

---

## ESLint Config

```json
{
  "extends": [
    "plugin:@typescript-eslint/recommended-type-checked",
    "prettier"
  ],
  "parser": "@typescript-eslint/parser",
  "parserOptions": {
    "project": true
  },
  "rules": {
    "@typescript-eslint/no-explicit-any": "error",
    "@typescript-eslint/no-unsafe-assignment": "error",
    "@typescript-eslint/no-floating-promises": "error",
    "@typescript-eslint/consistent-type-imports": "warn"
  }
}
```

Use `recommended-type-checked` (not just `recommended`) — it enables rules that require type information and catches significantly more issues.

---

## Pitfalls to Avoid

- **Type assertions (`as`)**: Only use when you have information the compiler doesn't. Never use to silence errors.
- **Non-null assertion (`!`)**: Avoid. Fix the underlying nullability instead.
- **Enums**: Prefer union types or `as const` objects. Enums have surprising runtime behavior and complicate tree-shaking.
- **Namespace imports for side effects**: `import 'some-lib'` is fine; `namespace` keyword is legacy, avoid it.
- **Ignoring promise rejections**: Always handle async errors. `@typescript-eslint/no-floating-promises` catches unhandled promises.

# React Reference

Covers React 19, Next.js 15, and the modern React ecosystem. No class components.

## Project Setup

Prefer **Next.js** for new apps (SSR, RSC, file-based routing, image optimization built-in).
Use **Vite + React Router v7** for SPAs that don't need SSR.
Use **Remix** for form-heavy, progressive enhancement apps.

```bash
# Next.js
npx create-next-app@latest --typescript --tailwind --eslint --app

# Vite SPA
npm create vite@latest my-app -- --template react-ts
```

---

## Component Patterns

### Function components only

```typescript
// Standard component
interface ButtonProps {
  label: string
  variant?: 'primary' | 'secondary' | 'ghost'
  onClick: () => void
  disabled?: boolean
}

export function Button({ label, variant = 'primary', onClick, disabled = false }: ButtonProps) {
  return (
    <button
      className={cn(buttonVariants({ variant }))}
      onClick={onClick}
      disabled={disabled}
    >
      {label}
    </button>
  )
}
```

### Composition with children

Prefer `children` and slot patterns over heavily prop-drilled components:

```typescript
interface CardProps {
  children: React.ReactNode
  className?: string
}

export function Card({ children, className }: CardProps) {
  return <div className={cn('rounded-lg border p-4', className)}>{children}</div>
}

// Usage — composable, no prop explosion
<Card>
  <Card.Header>Title</Card.Header>
  <Card.Body>Content</Card.Body>
</Card>
```

### Custom hooks for logic extraction

Extract anything non-trivial from components into hooks. Hooks are testable in isolation.

```typescript
// hooks/useDebounce.ts
export function useDebounce<T>(value: T, delay: number): T {
  const [debouncedValue, setDebouncedValue] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedValue(value), delay)
    return () => clearTimeout(timer)
  }, [value, delay])
  return debouncedValue
}
```

---

## State Management

Choose based on complexity:

| Scenario | Solution |
|---|---|
| Local UI state | `useState`, `useReducer` |
| Shared UI state (theme, modals) | Context + `useReducer` |
| Server state (fetching, caching) | TanStack Query (React Query) |
| Complex global client state | Zustand |
| Large-scale with devtools / time-travel | Redux Toolkit |

**Default for most apps**: `useState` + TanStack Query. Don't add Zustand or Redux until you have a clear need.

Context API is for low-update-frequency data (auth state, theme, locale). Not for frequently updating state — every consumer re-renders on context change.

### TanStack Query pattern

```typescript
// hooks/useUser.ts
export function useUser(id: string) {
  return useQuery({
    queryKey: ['user', id],
    queryFn: () => userService.getById(id),
    staleTime: 5 * 60 * 1000, // 5 minutes
  })
}

// Mutation with optimistic update
export function useUpdateUser() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: userService.update,
    onMutate: async (updatedUser) => {
      await queryClient.cancelQueries({ queryKey: ['user', updatedUser.id] })
      const previous = queryClient.getQueryData(['user', updatedUser.id])
      queryClient.setQueryData(['user', updatedUser.id], updatedUser)
      return { previous }
    },
    onError: (_, updatedUser, context) => {
      queryClient.setQueryData(['user', updatedUser.id], context?.previous)
    },
    onSettled: (_, __, updatedUser) => {
      queryClient.invalidateQueries({ queryKey: ['user', updatedUser.id] })
    },
  })
}
```

---

## React 19 Features

### React Compiler (stable as of Oct 2025)

The React Compiler automatically memoizes components. In React 19 projects, **do not manually add `useMemo`, `useCallback`, or `React.memo`** unless you have a profiler-measured reason. The compiler handles it and manual memoization can interfere.

### Server Components (Next.js App Router)

Default components in the App Router are **React Server Components (RSC)**. They run on the server, have no client JS bundle cost, and can directly access databases/services.

```typescript
// app/users/page.tsx — Server Component (default)
export default async function UsersPage() {
  const users = await db.user.findMany() // Direct DB access — no API call needed
  return <UserList users={users} />
}
```

Add `'use client'` only when you need interactivity (event handlers, hooks, browser APIs):

```typescript
'use client'
// This component ships JS to the browser
export function SearchInput({ onSearch }: { onSearch: (q: string) => void }) {
  const [value, setValue] = useState('')
  return <input value={value} onChange={e => { setValue(e.target.value); onSearch(e.target.value) }} />
}
```

**Rule**: Push `'use client'` as far down the component tree as possible. Keep data fetching in Server Components.

### Server Actions

```typescript
// actions/user.ts
'use server'
export async function updateUser(id: string, data: Partial<User>) {
  const validated = UserUpdateSchema.parse(data)
  return db.user.update({ where: { id }, data: validated })
}

// Component usage
<form action={updateUser.bind(null, userId)}>
  <input name="name" />
  <button type="submit">Save</button>
</form>
```

---

## Rendering Strategy (Next.js)

| Strategy | When to use |
|---|---|
| SSR (dynamic) | User-specific, real-time, auth-gated pages |
| SSG (static) | Marketing pages, docs, content that rarely changes |
| ISR | Content that changes periodically (e.g. hourly) |
| CSR | Dashboards, internal tools where SEO doesn't matter |

Prefer SSR or SSG by default. Only go CSR when there's a specific reason.

---

## Performance

### Code splitting

```typescript
// Lazy load heavy components
const HeavyChart = lazy(() => import('./HeavyChart'))

function Dashboard() {
  return (
    <Suspense fallback={<ChartSkeleton />}>
      <HeavyChart />
    </Suspense>
  )
}
```

### Image optimization (Next.js)

```typescript
import Image from 'next/image'

// Always use next/image — handles WebP conversion, lazy loading, CLS prevention
<Image
  src="/hero.jpg"
  width={1200}
  height={600}
  alt="Hero image"
  priority // Add for LCP images — disables lazy loading
/>
```

### Avoid unnecessary re-renders

With the React Compiler active, focus on:
- Keeping state as local as possible
- Avoiding object/array literals as default prop values (new reference each render)
- Not creating functions inline when passing to non-compiled third-party components

---

## File Structure (feature-based)

```
src/
  app/                    # Next.js App Router pages
    (auth)/
      login/page.tsx
    dashboard/
      page.tsx
      layout.tsx
  features/
    users/
      components/
        UserCard.tsx
        UserCard.test.tsx
      hooks/
        useUser.ts
        useUser.test.ts
      services/
        userService.ts     # API calls, data transformation
      types.ts             # Feature-specific types
  components/
    ui/                    # Shared primitives (Button, Input, Modal)
    layout/                # Header, Sidebar, Footer
  lib/
    api.ts                 # Axios/fetch instance with auth interceptors
    queryClient.ts         # TanStack Query configuration
  hooks/                   # Shared hooks (useDebounce, useLocalStorage)
  types/                   # Global shared types
```

---

## Testing (React)

```typescript
// UserCard.test.tsx
import { render, screen, userEvent } from '@testing-library/react'

describe('UserCard', () => {
  it('displays user name and email', () => {
    render(<UserCard user={{ id: '1', name: 'Alice', email: 'alice@example.com' }} />)
    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(screen.getByText('alice@example.com')).toBeInTheDocument()
  })

  it('calls onEdit when edit button is clicked', async () => {
    const onEdit = vi.fn()
    render(<UserCard user={mockUser} onEdit={onEdit} />)
    await userEvent.click(screen.getByRole('button', { name: /edit/i }))
    expect(onEdit).toHaveBeenCalledWith(mockUser.id)
  })
})
```

Use `@testing-library/react`. Test what users see and do, not internal state or implementation details.

---

## Key Libraries (2025 defaults)

| Concern | Library |
|---|---|
| Server state | TanStack Query v5 |
| Client state | Zustand |
| Forms | React Hook Form + Zod |
| Styling | Tailwind CSS |
| UI primitives | shadcn/ui (Radix-based) |
| Animation | Motion (formerly Framer Motion) |
| Tables | TanStack Table |
| Date | date-fns |
| Icons | Lucide React |
| Testing | Vitest + Testing Library + Playwright |

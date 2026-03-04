# React + GraphQL / Relay Reference

Covers React with GraphQL, with a focus on Relay (Meta's GraphQL client). Also covers Apollo Client as an alternative.

## The Core Relay Principle: Colocated Fragments

Relay's defining feature is that **each component declares exactly what data it needs** via a GraphQL fragment. The parent query composes fragments from children — components don't know about each other's data requirements.

This is the opposite of "fetch everything at the top and pass props down."

```typescript
// UserCard declares its own data needs
const UserCardFragment = graphql`
  fragment UserCard_user on User {
    id
    name
    avatarUrl
    email
  }
`

interface Props {
  user: UserCard_user$key  // Relay-generated opaque type — not the raw data
}

export function UserCard({ user }: Props) {
  const data = useFragment(UserCardFragment, user)
  return (
    <div>
      <img src={data.avatarUrl} alt={data.name} />
      <h2>{data.name}</h2>
      <p>{data.email}</p>
    </div>
  )
}
```

```typescript
// Parent query composes child fragments
const UserProfileQuery = graphql`
  query UserProfileQuery($userId: ID!) {
    user(id: $userId) {
      ...UserCard_user        # Spread child's fragment
      ...UserActions_user     # Another child's fragment
    }
  }
`

export function UserProfile({ userId }: { userId: string }) {
  const data = useLazyLoadQuery<UserProfileQuery>(UserProfileQuery, { userId })
  return (
    <div>
      <UserCard user={data.user} />       {/* Pass the fragment key, not raw data */}
      <UserActions user={data.user} />
    </div>
  )
}
```

**Key insight**: `data.user` passed to `UserCard` is a fragment key (opaque reference), not the actual data object. The component calls `useFragment` to read its slice. This is **data masking** — `UserProfile` cannot accidentally access fields it didn't declare.

---

## Relay Setup

```bash
npm install react-relay relay-runtime
npm install --save-dev relay-compiler babel-plugin-relay
```

`relay.config.js`:
```javascript
module.exports = {
  src: './src',
  schema: './schema.graphql',
  language: 'typescript',
  eagerEsModules: true,
  excludes: ['**/node_modules/**', '**/__mocks__/**', '**/__generated__/**'],
}
```

Run the compiler (generates `__generated__/` files with TypeScript types):
```bash
npx relay-compiler
# or watch mode
npx relay-compiler --watch
```

Relay generates all TypeScript types from your GraphQL schema. You never write query/fragment response types manually.

---

## Core Hooks

### `useLazyLoadQuery` — fetch data on render

```typescript
const data = useLazyLoadQuery<MyQuery>(
  graphql`query MyQuery($id: ID!) { ... }`,
  { id: userId },
  { fetchPolicy: 'store-and-network' } // options
)
```

Fetch policies:
- `store-and-network`: Return cached, also fetch fresh (default for most cases)
- `network-only`: Always fetch, ignore cache
- `store-or-network`: Return cached if available, else fetch
- `store-only`: Never fetch, return from cache only (useful for disconnected flows)

### `useFragment` — read fragment data in a component

```typescript
const data = useFragment(MyFragment, fragmentKey)
```

Always use `useFragment` — never access raw data from props without it. This is what enables data masking and incremental loading.

### `usePaginationFragment` — cursor-based pagination

```typescript
const { data, loadNext, hasNext, isLoadingNext } = usePaginationFragment(
  graphql`
    fragment UserList_query on Query
    @refetchable(queryName: "UserListPaginationQuery")
    @argumentDefinitions(
      first: { type: "Int", defaultValue: 10 }
      after: { type: "String" }
    ) {
      users(first: $first, after: $after) @connection(key: "UserList_users") {
        edges {
          node {
            id
            ...UserCard_user
          }
        }
      }
    }
  `,
  queryRef
)

// Render
<button onClick={() => loadNext(10)} disabled={!hasNext || isLoadingNext}>
  Load more
</button>
```

### `useMutation` — mutations

```typescript
const [commitMutation, isMutationInFlight] = useMutation<UpdateUserMutation>(
  graphql`
    mutation UpdateUserMutation($input: UpdateUserInput!) {
      updateUser(input: $input) {
        user {
          id
          name
          email
        }
      }
    }
  `
)

function handleSubmit(values: FormValues) {
  commitMutation({
    variables: { input: values },
    onCompleted: (data) => { /* success */ },
    onError: (error) => { /* handle error */ },
    // Optimistic response — update UI before server confirms
    optimisticResponse: {
      updateUser: {
        user: { id: userId, ...values }
      }
    }
  })
}
```

---

## Relay Environment Setup

```typescript
// lib/relayEnvironment.ts
import { Environment, Network, RecordSource, Store } from 'relay-runtime'

async function fetchGraphQL(query: string, variables: Record<string, unknown>) {
  const response = await fetch('/graphql', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${getAuthToken()}`,
    },
    body: JSON.stringify({ query, variables }),
  })
  return response.json()
}

export const environment = new Environment({
  network: Network.create(fetchGraphQL),
  store: new Store(new RecordSource()),
})
```

Wrap your app:
```typescript
<RelayEnvironmentProvider environment={environment}>
  <Suspense fallback={<LoadingSpinner />}>
    <App />
  </Suspense>
</RelayEnvironmentProvider>
```

---

## Suspense Integration

Relay is built around Suspense. Components that use `useLazyLoadQuery` suspend while data loads.

```typescript
// Always wrap query roots in Suspense
function UsersPage() {
  return (
    <ErrorBoundary fallback={<ErrorState />}>
      <Suspense fallback={<UserListSkeleton />}>
        <UserList />
      </Suspense>
    </ErrorBoundary>
  )
}
```

Use skeleton components (not spinners) for Suspense fallbacks — better perceived performance and prevents layout shift.

---

## Apollo Client (alternative to Relay)

Use Apollo when: you don't need Relay's strict data masking, your team is more familiar with it, or the backend isn't Relay-optimized.

```typescript
// Apollo setup
const client = new ApolloClient({
  uri: '/graphql',
  cache: new InMemoryCache(),
  headers: { Authorization: `Bearer ${getAuthToken()}` }
})

// Query
const GET_USER = gql`
  query GetUser($id: ID!) {
    user(id: $id) {
      id
      name
      email
    }
  }
`

function UserCard({ userId }: { userId: string }) {
  const { data, loading, error } = useQuery(GET_USER, { variables: { id: userId } })
  if (loading) return <Skeleton />
  if (error) return <ErrorState error={error} />
  return <div>{data.user.name}</div>
}
```

Apollo vs Relay decision:
- **Relay**: Strict, compiler-enforced, best for large teams and complex apps. Steeper learning curve.
- **Apollo**: More flexible, easier to adopt, larger community. Less opinionated.

---

## Fragment Naming Convention

Relay enforces a strict convention: `ComponentName_propName`

```graphql
fragment UserCard_user on User { ... }
#        ^^^^^^^^^^ ^^^^^
#        Component  Prop name in component
```

Stick to this — it's enforced by the compiler and makes colocated data requirements scannable.

---

## Testing Relay Components

Use `@relay/test-utils` with `MockEnvironment`:

```typescript
import { createMockEnvironment, MockPayloadGenerator } from 'relay-test-utils'

const environment = createMockEnvironment()

test('renders user name', async () => {
  render(
    <RelayEnvironmentProvider environment={environment}>
      <Suspense fallback="Loading">
        <UserCard userId="1" />
      </Suspense>
    </RelayEnvironmentProvider>
  )

  // Resolve the pending operation with mock data
  act(() => {
    environment.mock.resolveMostRecentOperation(operation =>
      MockPayloadGenerator.generate(operation, {
        User: () => ({ id: '1', name: 'Alice', email: 'alice@example.com' })
      })
    )
  })

  expect(await screen.findByText('Alice')).toBeInTheDocument()
})
```

---

## Common Mistakes

- **Accessing data without `useFragment`**: Never read `user.name` directly when `user` is a fragment key. Always call `useFragment` first.
- **Over-fetching in fragments**: Only declare fields you actually use. Relay's compiler will warn about unused fields.
- **Mutations without optimistic responses**: For any user-triggered mutation, add `optimisticResponse` to avoid UI lag.
- **Missing `@connection` on paginated lists**: Required for Relay's pagination to work correctly and for cache updates after mutations.
- **Not running the compiler after schema changes**: Generated types will be stale. Make `relay-compiler --watch` part of your dev script.

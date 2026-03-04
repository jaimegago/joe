# Angular Reference

Covers Angular v22 (current stable as of late 2025). Modern Angular means: Standalone Components, Signals, Zoneless, functional APIs.

## The Modern Angular Mindset

Angular has shed a lot of legacy baggage. In 2025:
- **NgModules are legacy** — standalone components are the standard
- **Signals replace most RxJS** for local/component state
- **Zoneless is the default** in new projects
- **Functional guards/interceptors** replace class-based ones
- **Typed reactive forms** are the standard

If you're working on a legacy codebase, migrate incrementally. New code should always follow modern patterns.

---

## Project Setup

```bash
ng new my-app --standalone --routing --style=scss
# Angular CLI now scaffolds standalone by default
```

`tsconfig.json` — Angular enforces strict TypeScript:
```json
{
  "compilerOptions": {
    "strict": true,
    "strictTemplates": true,
    "strictInjectionParameters": true
  }
}
```

`strictTemplates` is Angular-specific and catches type errors in HTML templates at compile time. Always enable it.

---

## Standalone Components (the default)

```typescript
import { Component, input, output } from '@angular/core'
import { CommonModule } from '@angular/common'

@Component({
  standalone: true,
  selector: 'app-user-card',
  imports: [CommonModule],   // Import dependencies directly — no NgModule
  template: `
    <div class="user-card">
      <h2>{{ user().name }}</h2>
      <p>{{ user().email }}</p>
      <button (click)="onEdit()">Edit</button>
    </div>
  `,
})
export class UserCardComponent {
  // Angular 17+ signal-based inputs
  user = input.required<User>()
  edit = output<string>()

  onEdit() {
    this.edit.emit(this.user().id)
  }
}
```

Key points:
- `standalone: true` — no NgModule required
- `input()` / `input.required()` — signal-based inputs (Angular 17+), replace `@Input()`
- `output()` — replaces `@Output() EventEmitter`
- Use `imports` array directly in the component for dependencies

---

## Signals

Signals are the recommended reactivity primitive for local and shared state. They replace most `BehaviorSubject` + async pipe patterns.

```typescript
import { signal, computed, effect } from '@angular/core'

@Component({
  standalone: true,
  template: `
    <p>Count: {{ count() }}</p>
    <p>Double: {{ double() }}</p>
    <button (click)="increment()">+</button>
  `
})
export class CounterComponent {
  // Writable signal
  count = signal(0)

  // Derived signal — recomputes automatically when count changes
  double = computed(() => this.count() * 2)

  constructor() {
    // Side effect that runs when dependencies change
    effect(() => {
      console.log('Count changed to', this.count())
    })
  }

  increment() {
    this.count.update(c => c + 1)
    // or: this.count.set(this.count() + 1)
  }
}
```

### When to use Signals vs RxJS

| Use case | Signals | RxJS |
|---|---|---|
| Local component state | ✅ | — |
| Derived/computed values | ✅ | — |
| HTTP requests | — | ✅ (HttpClient returns Observables) |
| Complex async streams, debounce, merge | — | ✅ |
| Global state (simple) | ✅ `@ngrx/signals` | — |
| Global state (complex, devtools) | — | ✅ NgRx |

Bridge between worlds: `toSignal()` converts Observables to Signals for template use.

```typescript
users = toSignal(this.userService.getAll(), { initialValue: [] })
// Now use users() in template — no async pipe needed
```

---

## Dependency Injection

Use `inject()` function (modern) instead of constructor injection:

```typescript
// Modern — inject() in field initializer
@Component({ ... })
export class UserListComponent {
  private userService = inject(UserService)
  private router = inject(Router)

  users = toSignal(this.userService.getAll(), { initialValue: [] })
}

// Legacy (still valid, avoid in new code)
constructor(private userService: UserService) {}
```

### Services

```typescript
@Injectable({ providedIn: 'root' })  // Singleton, tree-shakable
export class UserService {
  private http = inject(HttpClient)
  private apiUrl = inject(API_URL_TOKEN)  // Use injection tokens for config

  getAll(): Observable<User[]> {
    return this.http.get<User[]>(`${this.apiUrl}/users`).pipe(
      map(users => users.map(u => UserSchema.parse(u)))  // Validate at boundary
    )
  }

  update(id: string, data: Partial<User>): Observable<User> {
    return this.http.patch<User>(`${this.apiUrl}/users/${id}`, data)
  }
}
```

---

## Routing

```typescript
// app.routes.ts
export const routes: Routes = [
  {
    path: 'users',
    loadComponent: () => import('./features/users/user-list.component')
      .then(m => m.UserListComponent),  // Lazy load by default
  },
  {
    path: 'users/:id',
    loadComponent: () => import('./features/users/user-detail.component')
      .then(m => m.UserDetailComponent),
    resolve: {
      user: userResolver,  // Functional resolver
    },
    canActivate: [authGuard],  // Functional guard
  },
]

// Functional guard (replaces class-based CanActivate)
export const authGuard: CanActivateFn = (route, state) => {
  const authService = inject(AuthService)
  const router = inject(Router)
  return authService.isAuthenticated()
    ? true
    : router.createUrlTree(['/login'], { queryParams: { returnUrl: state.url } })
}

// Functional resolver
export const userResolver: ResolveFn<User> = (route) => {
  const userService = inject(UserService)
  return userService.getById(route.paramMap.get('id')!)
}
```

---

## Change Detection

New projects should be zoneless:

```typescript
// main.ts
bootstrapApplication(AppComponent, {
  providers: [
    provideExperimentalZonelessChangeDetection(),  // Zoneless
    provideRouter(routes),
    provideHttpClient(),
  ]
})
```

With zoneless + Signals, Angular only re-renders components when their signals change. No Zone.js overhead.

For components not yet on Signals, use `ChangeDetectionStrategy.OnPush` as the minimum:

```typescript
@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  // ...
})
```

---

## Deferrable Views

Lazy load parts of a template with `@defer`:

```typescript
@Component({
  template: `
    <app-header />

    @defer (on viewport) {
      <app-heavy-chart [data]="chartData()" />
    } @placeholder {
      <div class="chart-placeholder">Chart loading...</div>
    } @loading (minimum 200ms) {
      <app-skeleton />
    } @error {
      <p>Failed to load chart</p>
    }
  `
})
```

`@defer` conditions:
- `on idle` — when browser is idle
- `on viewport` — when element enters viewport
- `on interaction` — on user interaction
- `on timer(2s)` — after a delay
- `when condition` — signal/boolean condition

Use `@defer` for: heavy third-party widgets, charts, below-the-fold content, non-critical UI.

---

## State Management

| App size | Solution |
|---|---|
| Small / medium | Signals + services |
| Large, complex, needs devtools | `@ngrx/signals` store |
| Legacy / very complex | NgRx (actions/reducers/effects) |

### Signal Store (`@ngrx/signals`)

```typescript
import { signalStore, withState, withMethods, withComputed } from '@ngrx/signals'

export const UserStore = signalStore(
  { providedIn: 'root' },
  withState({ users: [] as User[], isLoading: false, error: null as string | null }),
  withComputed(({ users }) => ({
    userCount: computed(() => users().length),
  })),
  withMethods((store, userService = inject(UserService)) => ({
    async loadUsers() {
      patchState(store, { isLoading: true })
      try {
        const users = await firstValueFrom(userService.getAll())
        patchState(store, { users, isLoading: false })
      } catch (error) {
        patchState(store, { error: 'Failed to load users', isLoading: false })
      }
    }
  }))
)
```

---

## File Structure

```
src/
  app/
    app.config.ts          # Bootstrap providers
    app.routes.ts          # Root routes
    features/
      users/
        components/
          user-card.component.ts
          user-card.component.spec.ts
        pages/
          user-list.page.ts
          user-detail.page.ts
        services/
          user.service.ts
          user.service.spec.ts
        store/
          user.store.ts    # @ngrx/signals store (if needed)
        user.routes.ts     # Feature-level routes (lazy loaded)
        index.ts           # Public barrel export
    shared/
      components/          # Shared UI primitives
      directives/
      pipes/
      services/
    core/
      guards/
      interceptors/
      tokens/              # Injection tokens
```

---

## HTTP Interceptors (functional)

```typescript
export const authInterceptor: HttpInterceptorFn = (req, next) => {
  const authService = inject(AuthService)
  const token = authService.getToken()

  if (!token) return next(req)

  const authReq = req.clone({
    headers: req.headers.set('Authorization', `Bearer ${token}`)
  })
  return next(authReq)
}

export const errorInterceptor: HttpInterceptorFn = (req, next) => {
  return next(req).pipe(
    catchError((error: HttpErrorResponse) => {
      if (error.status === 401) inject(Router).navigate(['/login'])
      return throwError(() => error)
    })
  )
}

// Register in app.config.ts
provideHttpClient(withInterceptors([authInterceptor, errorInterceptor]))
```

---

## Testing

```typescript
// user.service.spec.ts
describe('UserService', () => {
  let service: UserService
  let httpMock: HttpTestingController

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [UserService, provideHttpClientTesting()]
    })
    service = TestBed.inject(UserService)
    httpMock = TestBed.inject(HttpTestingController)
  })

  afterEach(() => httpMock.verify())

  it('fetches users', () => {
    const mockUsers: User[] = [{ id: '1', name: 'Alice', email: 'alice@test.com' }]
    service.getAll().subscribe(users => {
      expect(users).toEqual(mockUsers)
    })
    const req = httpMock.expectOne('/api/users')
    expect(req.request.method).toBe('GET')
    req.flush(mockUsers)
  })
})

// Component spec — use Testing Library (@testing-library/angular)
it('displays user list', async () => {
  const { getByText } = await render(UserListComponent, {
    providers: [{ provide: UserService, useValue: mockUserService }]
  })
  expect(getByText('Alice')).toBeInTheDocument()
})
```

Prefer `@testing-library/angular` for component tests — same philosophy as React Testing Library, tests behavior not implementation.

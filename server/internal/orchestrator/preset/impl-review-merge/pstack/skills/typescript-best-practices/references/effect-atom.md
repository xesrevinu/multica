# Atom state management

Read this when a client app needs reactive state that is backed by effects: server data, derived values, mutations, persisted preferences, or URL state. Atom is Effect's state layer; it keeps the `Effect` graph and the component tree on the same model instead of translating between a promise cache and a store.

Status: `@stability unstable` in `effect@4`. Check the local source before relying on a constructor (`packages/effect/src/reactivity/`).

## What an Atom is

An `Atom<A>` describes how to produce or update one piece of state. An `AtomRegistry` runs reads, remembers current values, tracks dependencies, starts effects and streams, and clears atoms nobody uses. Consequences you must design around:

- Atoms are lazy. Nothing runs until something reads them, and an unobserved atom can be garbage collected. `Atom.keepAlive` opts an atom out of collection.
- Identity is the atom object, not a string key. Build atoms as module constants or with `Atom.family`; never inside a render or request.
- Async atoms hold an `AsyncResult`, not `A | undefined` plus a boolean.

Module: `effect/reactivity` exports `Atom`, `AsyncResult`, `AtomRegistry`, `AtomRef`, `AtomHttpApi`, `AtomRpc`, and `Hydration`. Framework bindings are separate packages: `@effect/atom-react`, `@effect/atom-solid`, `@effect/atom-vue`.

## Constructors

| Need | Constructor | Result |
|---|---|---|
| Local writable state | `Atom.make(initialValue)` | `Writable<A>` |
| Derived state | `Atom.make((get) => value)` | `Atom<A>` |
| Effect-backed query | `Atom.make(effect, { initialValue })` | `Atom<AsyncResult<A, E>>` |
| Stream-backed value | `Atom.make(stream, { initialValue })` | `Atom<AsyncResult<A, E>>` |
| Action / mutation | `Atom.fn(effect)`, `Atom.fnSync(fn)` | writable atom; set it with the argument |
| Per-key instances | `Atom.family((key) => atom)` | memoized factory |
| Persisted value | `Atom.kvs({ runtime, key, schema, defaultValue })` | `Writable<A>` |
| URL state | `Atom.searchParam(name, { schema })` | `Writable<Option<A>>` |
| Refetch policy | `Atom.swr`, `Atom.withRefresh`, `Atom.refreshOnWindowFocus` | wraps an async atom |
| Optimistic writes | `Atom.optimistic`, `Atom.optimisticFn` | writable view over an async atom |

```ts
// Writable: `registry.set(count, 1)`.
const count = Atom.make(0)

// Derived: recomputed from dependencies, no manual invalidation.
const doubled = Atom.make((get) => get(count) * 2)

// Query: the value is an AsyncResult; reading it starts the effect.
// Effects that need services are built through `Atom.runtime` (below).
const todos = Atom.make(fetchTodos, { initialValue: [] })
```

`Atom.fn` is the mutation primitive: `registry.set(saveTodo, payload)` starts the effect and the atom's value becomes the `AsyncResult` of that call. A second call replaces the running one; `{ concurrent: true }` lets runs overlap instead.

## AsyncResult, not loading booleans

`AsyncResult<A, E>` is `Initial | Success<A> | Failure<E>`, each with a `waiting` flag for a refresh on top of the previous value. That is exactly the four UI states you actually render: nothing yet, refreshing with stale data, success, failure. It removes the `isLoading` + `isError` + `data?` triple that can contradict itself.

```ts
const view = (result: AsyncResult.AsyncResult<Todo[], ApiError>) => {
  if (AsyncResult.isInitial(result)) return "loading"
  if (AsyncResult.isFailure(result)) return `failed: ${result.error._tag}`
  const todos = result.value
  return result.waiting ? `${todos.length} (refreshing)` : `${todos.length}`
}
```

- Do not `AsyncResult.getOrThrow` in a component; branch on the state or use `Atom` patterns (`withFallback`) to supply a default.
- `AsyncResult.map`, `flatMap`, `match`, and `matchWithError` compose results without unwrapping them.
- A failed query keeps its error in the atom; refresh or a dependency change replaces it.

## Services in atoms

`Atom.runtime(layer)` gives atoms access to services. Use it instead of reaching for module singletons or a global `Effect.runPromise`.

```ts
export const appRuntime = Atom.runtime(AppLayer)

// Query atom whose effect needs AppLayer services.
const todos = appRuntime.atom(
  Effect.gen(function*() {
    const api = yield* TodoApi
    return yield* api.list()
  })
)

// Mutation that also invalidates every atom tagged with "todos".
const addTodo = appRuntime.fn(
  Effect.fnUntraced(function*(title: string) {
    const api = yield* TodoApi
    return yield* api.create(title)
  }),
  { reactivityKeys: ["todos"] }
)

// Any query can opt into that invalidation:
const todosLive = todos.pipe(Atom.withReactivity(["todos"]))
```

- One runtime per app, built from the app Layer. In React, one registry per app (or per test) via `RegistryProvider`.
- `reactivityKeys` is the invalidation contract between a mutation and the queries it affects. Prefer keys over manually refreshing N atoms.
- `runtime.pull` and `runtime.subscriptionRef` when state must be read on demand or kept in a `SubscriptionRef`.

## Server data: AtomHttpApi and AtomRpc

When the backend is an `HttpApi`, generate the client and the atoms from the same definition.

```ts
export class ApiClient extends AtomHttpApi.Service<ApiClient>()("app/ApiClient", {
  api: Api,
  httpClient: FetchHttpClient.layer,
  baseUrl: "https://api.example.com"
}) {}

// Query: memoized per group/endpoint/options; the value is an AsyncResult.
const user = ApiClient.query("users", "getUser", {
  params: { id: 1 },
  reactivityKeys: ["users"],
  timeToLive: "30 seconds"
})

// Mutation: set it to run. `reactivityKeys` travels with each call and invalidates
// every query tagged with the same keys.
const createUser = ApiClient.mutation("users", "createUser")
// registry.set(createUser, { payload: { name: "Ada" }, reactivityKeys: ["users"] })
```

- Queries are cached atoms keyed by endpoint plus options; do not rebuild them per render.
- Mutations are writable atoms; the UI sets them and reads the resulting `AsyncResult` for pending/error state. The third argument only selects `responseMode`; invalidation keys are passed with the call.
- `AtomRpc.Service` is the same shape for `effect/rpc` groups.
- If the repo already has TanStack Query or Zustand, do not add Atom: follow the repo's existing split (Query owns server data, Zustand owns client state). Atom is the equivalent layer for Effect-native clients, not an addition on top.

## React bindings

```tsx
import { RegistryProvider, useAtom, useAtomValue } from "@effect/atom-react"

export function App() {
  return (
    <RegistryProvider>
      <TodoList />
    </RegistryProvider>
  )
}

function TodoList() {
  const todos = useAtomValue(todosAtom) // AsyncResult<Todo[], ApiError>
  const [, addTodo] = useAtom(addTodoAtom) // setter only; the atom itself is the action
  // ...
}
```

- `RegistryProvider` creates one registry for the subtree; `RegistryContext` supplies an existing one. Mount it once near the root.
- `useAtom(writable)` returns `[value, write]`; `useAtomValue` subscribes without a setter; `useAtomSet` gets the setter alone; `useAtomMount` starts an effect without subscribing; `useAtomRefresh` returns a refetch callback; `useAtomSuspense` suspends on the first load.
- With an async atom, `useAtom(..., { mode: "promise" })` hands back a promise-returning setter for form submits and transitions.
- A component must not create atoms during render. Keep them at module scope, or in `Atom.family` keyed by an id prop.

## SSR and persistence

- Mark atoms that can be serialized with `Atom.serializable(atom, { key, schema })`, then `Hydration.dehydrate(registry)` on the server and `Hydration.hydrate(registry, entries)` on the client. `HydrationBoundary` and `useAtomInitialValues` wire the same data into React.
- `Atom.kvs` persists through `KeyValueStore` with a schema, so a stale or malformed stored value cannot become an untyped value in the app.
- Never dehydrate secrets or whole request payloads; dehydrate exactly the atoms the first screen renders.

## Testing atom code

```ts
it.effect("loads todos", () =>
  Effect.scoped(Effect.gen(function*() {
    const registry = yield* Effect.acquireRelease(
      Effect.sync(() => AtomRegistry.make()),
      (registry) => Effect.sync(() => registry.dispose())
    )

    const todos = Atom.make(Effect.succeed<Todo[]>([]))

    // getResult unwraps AsyncResult: it succeeds with the value, fails with the error.
    assert.deepStrictEqual(yield* AtomRegistry.getResult(registry, todos), [])
  }))
)
```

- Build a fresh registry per test and `dispose()` it; a shared registry leaks atoms and subscriptions between tests.
- Provide test layers to `Atom.runtime` instead of mocking modules. `AtomRegistry.getResult` fails with the atom's typed error, so the test asserts the same failure the UI renders.
- `Atom.batch(() => { registry.set(a, 1); registry.set(b, 2) })` when several writes must notify observers once.

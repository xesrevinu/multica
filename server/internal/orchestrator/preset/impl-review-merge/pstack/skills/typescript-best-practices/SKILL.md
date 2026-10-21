---
name: typescript-best-practices
description: TypeScript engineering defaults, with Effect (effect@4) for the hard parts. whenever work involves failure handling, concurrency, streaming, shared state, or owned resources.
paths: ["**/*.ts", "**/*.tsx"]
disable-model-invocation: true
---

# TypeScript best practices

Apply the **type-system-discipline** principle skill first. Use the simplest tool that makes the hard part impossible to get wrong: plain TypeScript for data shapes and pure transforms, Effect for control flow that must survive failure, concurrency, interruption, and cleanup.

## Reach for Effect when the work is hard

| Hard part | Hand-rolled (avoid) | Effect |
|---|---|---|
| Failure paths | `throw`, `try/catch`, `null` for missing | tagged errors in `E`, `Effect.catchTag(s)`, `Option`, `Result` |
| Retry, timeout, polling | `setTimeout` loops, attempt counters | `Effect.retry(Schedule...)`, `Effect.timeout`, `Stream.fromEffectSchedule` |
| Concurrency | `Promise.all` on unbounded input, hand-written pools | `Effect.all(..., { concurrency })`, `Semaphore`, fibers |
| Cleanup | `try/finally` | `Effect.acquireRelease`, `Effect.scoped`, Layer teardown |
| Dependency injection | module singletons, clients threaded by hand | `Context.Service` + `Layer` |
| Streaming | async iterators plus manual backpressure | `Stream`, `Sink`, `Queue`, `PubSub` |
| Caching, batching, N+1 | ad-hoc `Map`s, bespoke loaders | `Cache`, `Request` + `RequestResolver` |
| HTTP API | hand-written `fetch` plus manual JSON validation | `HttpApi`, `HttpApiClient`, `HttpClient` |
| Database | string SQL, row casts, ad-hoc migrations | `SqlClient`, `SqlModel`, `SqlSchema`, `Migrator` |
| Client state | ad-hoc store plus loading booleans | `Atom`, `AsyncResult`, `AtomRegistry` |
| OS access | `node:fs`, `node:http`, `child_process` directly | `FileSystem`, `Path`, `ChildProcessSpawner`, platform layers |
| Shared state | locks and flags | `Ref`, `SynchronizedRef`, `Deferred`, `TxRef` |
| Config, secrets | `process.env` at import time | `Config`, `Context.Reference`, `Redacted` |
| Telemetry | `console.log`, ad-hoc timing | `Effect.log*`, `Effect.withSpan`, `annotateLogs` |

If code must coordinate two or more of {time, failure, concurrency, cancellation, cleanup, external resource}, write an `Effect` instead of an `async` function plus helpers. Do not build a worse version of a module that already ships in `effect`.

Plain TypeScript remains correct for pure transforms, domain types, parsing, component state, and scripts with no failure policy. Effect is not a syntax tax on every file.

Adding Effect is a dependency decision, not a drive-by refactor. Use it where the repository already depends on `effect`; where it does not, apply these type rules with the tools already present (`zod`, `AbortSignal`, plain `async`) and raise one issue that proposes the dependency and the boundary it would cover. Never add `effect` for a single file, and never rewrite a v3 codebase in v4 syntax unasked.

## Non-negotiable rules when writing Effect

- Sequence with `Effect.gen(function* () {})`; declare reusable or traced functions with `Effect.fn("Service.method")`, and `Effect.fnUntraced` in library internals and hot paths. Never keep a function whose only body is `Effect.gen`.
- Raise expected failure with `return yield* new NotFound({ id })`; the `return` tells the compiler the function does not continue. Do not `throw` for expected failure.
- Run at the edge only: `NodeRuntime.runMain`, `ManagedRuntime`, one `Effect.runPromise`. Calling `runPromise`/`runSync` inside a service loses `E`, spans, and interruption.
- Declare dependencies in `R` through services and Layers; provide the app layer once. No `Effect.provide` scattered through call sites.
- Concurrency is explicit: pass `{ concurrency }` or hold a `Semaphore` permit for anything touching a shared resource.
- Anything opened (file, socket, pool, subscription, worker) is acquired with `acquireRelease` inside `Effect.scoped` or a `Layer`.
- Absence is `Option`; a recoverable outcome is `Result`. No `null`/`undefined` or boolean-plus-optional-field bags in domain types.
- When a service's `E` keeps growing, put the actionable failures behind one binding error with a `reason` field and handle them with `Effect.catchReason(s)`; reach for `unwrapReason` when the caller must tag-match every case.
- Decode untrusted input with `Schema` at the boundary and let the decoded type flow inward. See the **boundary-discipline** principle skill.
- No `try/catch` inside `Effect.gen`; it cannot catch typed failure. Use `Effect.try`, `Effect.tryPromise`, `Effect.exit`.
- Detect the major version before writing syntax. `effect@4`: `Context.Service`, `Effect.catch`, `Effect.forkChild`, `Schema.TaggedError`, `Result`. `effect@3`: `Context.Tag`, `Effect.catchAll`, `Effect.fork`, `Data.TaggedError`, `Either`. Never mix.

## Type rules

| Rule | Summary |
|---|---|
| Discriminated unions | A literal discriminant makes impossible states unrepresentable. No optional-field bags. In Effect code: `Schema.TaggedClass` + `_tag`. |
| Branded types | `Schema.brand("AgentId")` in Effect code, `string & { readonly __brand: "AgentId" }` otherwise. The brand comes from a parser or verifying guard, never `as`. |
| Constructive modeling | Make the illegal value unconstructable: `[T, ...T[]]` non-empty, `[T, T][]` even, `start` + `duration` for a range. |
| Simplest total type | Keep `T[]` while every operation stays total; strengthen only where the loose type forces `!`, a cast, or a "should never happen" throw. |
| `unknown` over `any` | External data is `unknown` until decoded: RPC, `JSON.parse`, IPC, files, env, database rows. |
| Schema before guards | One schema owns validation and the inferred type — `effect/Schema` in Effect code, `zod` elsewhere. Never schema + interface + guard in parallel. |
| No `as` casts | Cast only after validation. Fix the missing discriminant, the wide source type, or the untyped boundary instead. |
| Narrowing hierarchy | Discriminant or `Match` > `in` > `typeof`/`instanceof` > user guard > `as`. |
| Exhaustiveness | `Match.exhaustive` in Effect code, `const _exhaustive: never = x` elsewhere. Both must break the build when a variant is added. |
| `satisfies` over `as` | Validates without widening literal types. |
| Schema-derived types | Try `Pick`/`Omit`/`Parameters`/`ReturnType`/`Awaited`/`typeof` before a new interface. |
| Object args | Objects, not positional args, so order is self-documenting. Skip on hot paths: per-frame render, tokenizers, parsers. |
| Real tests | Don't mock what you can run: `it.effect` with test Layers, real primitives with leak checks, a running build for UI. |

## Verify Effect APIs against the local source

Effect changes between majors and betas, so memory, blog posts, and other skill packs are stale by default. This machine keeps a v4 checkout at `~/.local/share/references-repos/effect`; a repository that depends on Effect also has `node_modules/effect`.

- Start with `LLMS.md` and `ai-docs/src/**`; those examples are compiled and tested. Then confirm signatures in `packages/effect/src/<Module>.ts`, whose JSDoc examples are doctested.
- `SCHEMA.md` for modeling. `MIGRATION.md` and `migration/v3-to-v4.md` for renames — grep them, never read them whole.
- If a name is not in the source, it does not exist. Never invent an Effect API, and never make a type error disappear with a wider type or a cast.

References:
- `references/effect.md` — services and Layers, typed errors (including v4 error reasons), retry/timeout, concurrency, resources, streams, batching, testing, running at the edge, spans and attributes.
- `references/effect-platform.md` — HttpClient, HttpApi servers and typed clients, SQL models and migrations, filesystem/process/socket layers.
- `references/effect-atom.md` — Atom state, AsyncResult, registry, React bindings, AtomHttpApi, SSR hydration.
- `references/effect-corrections.md` — v3/v4 renames and repeated-but-wrong APIs, checked against the local v4 source.
- `references/patterns.md` — type-level patterns with code.

# Effect API corrections

Other Effect skill packs are written against different betas, so a "correction" from one of them can itself be wrong. Every row here was checked against `~/.local/share/references-repos/effect` (`effect@4.0.1`). Re-check before trusting a row in a repository that pins another version.

```sh
E=~/.local/share/references-repos/effect
grep -n "export const Service"          $E/packages/effect/src/Context.ts
grep -n "export const TaggedError"      $E/packages/effect/src/Schema.ts
grep -n "export const catchTags\|export const forkChild" $E/packages/effect/src/Effect.ts
grep -n "export function decodeTo\|export function fromJsonString" $E/packages/effect/src/Schema.ts
grep -n "export function transform\b" $E/packages/effect/src/SchemaTransformation.ts
python3 -c 'import json;print([k for k in json.load(open("'$E'/packages/effect/package.json"))["exports"] if "http" in k])'
node -p "require('$E/packages/effect/package.json').version"
```

## v3 → v4 renames

| v3 | v4 (this checkout) | Note |
|---|---|---|
| `Context.Tag("X")<X, Shape>` | `class X extends Context.Service<X, Shape>()("app/X")` | Class with a static `layer`, not a tagged function |
| `Effect.catchAll` | `Effect.catch` | `catchTag`/`catchTags` are unchanged |
| `Effect.catchAllCause` | `Effect.catchCause` | |
| `Effect.catchAllDefect` | `Effect.catchDefect` | |
| `Effect.fork` | `Effect.forkChild` | `forkDaemon` → `forkDetach` |
| `Data.TaggedError` | `Schema.TaggedError` | Errors are also schema-encodable |
| `Effect.Service` | `Context.Service` + explicit `Layer` | No implicit accessors/`Default` layer |
| `Either` | `Result` | No `Either` module remains |
| `Effect.all(..., { mode: "either" })` | `Effect.all(..., { mode: "result" })` | Collects `Result`s instead of `Either`s |
| `Effect.makeSemaphore(n)` | `Semaphore.make(n)` | Then `semaphore.withPermits(n)(effect)` |
| `import { Schema } from "@effect/schema"` | `import { Schema } from "effect"` | Schema is core |

## Schema in v4

| Habit from v3 snippets | Reality in 4.0.1 | Consequence |
|---|---|---|
| `Schema.transform(...)`, `Schema.filter(...)` | No such combinators. Transformations are `Schema.decodeTo` / `Schema.encodeTo` with `SchemaTransformation.transform`, `transformEffect`, `trim`, `snakeToCamel`; validation is `Schema.check` / `Schema.refine` | Copy-pasted pipelines do not type check |
| `Schema.brand` as a runtime guarantee | The brand only narrows the TypeScript output type; representations and generated schemas omit it | Reapply `brand` after a rebuild or codegen; the schema still owns validation |
| `JSON.parse` then decode | `Schema.fromJsonString(schema, { space })` decodes the text directly | One boundary, one error channel |
| Decoding a body and setting the status separately | `Schema.TaggedError<Self>()("Tag", fields, { httpApiStatus: 404 })` carries the status on the error | HttpApi reads it; no handler-side status table |

## Repeated in other packs, wrong here

| Claim in other packs | Reality in 4.0.1 | Verify with |
|---|---|---|
| `ServiceMap.Service` replaced `Context.Service` | `Context.Service` is the API; `ServiceMap` does not exist | `Context.ts`, `package.json` exports |
| `Schema.TaggedErrorClass("Tag")("Tag", {...})` | `Schema.TaggedError<Self>()("Tag", {...})`; `Schema.Error<Self>("Tag")({...})` also exists | `grep -n "export const TaggedError" Schema.ts` |
| v4 uses `effect/unstable/http` | Subpaths are `effect/http`, `effect/http-api`, `effect/sql`, `effect/process`, `effect/rpc`, `effect/cluster`, `effect/socket` | `package.json` `exports` |
| `Effect.cachedWithTTL` was removed in favor of `Cache` | Both exist; `Cache.make` for keyed lookup, `cachedWithTTL` for one value | `grep -n "export const cachedWithTTL" Effect.ts` |
| `Effect.fn` bodies may `.pipe(...)` | Extra behaviour is passed as extra `Effect.fn` arguments; `.pipe` after `Effect.fn(...)` is applied outside the named span | `ai-docs/src/01_effect/01_basics/02_effect-fn.ts` |

Treat any API you cannot find in `packages/effect/src` as nonexistent, including names that look plausible (`Effect.retryWith`, `Effect.parallel`, `Effect.useService`).

## Mistakes that survive a type check

| Mistake | Why it is wrong | Instead |
|---|---|---|
| `const user = yield fetchUser(id)` | Yields the `Effect` description, not its result | `yield*` |
| `if (bad) yield* Effect.fail(e)` | The generator's control flow is not narrowed; the next line still runs in the types | `return yield* Effect.fail(e)` |
| `try { yield* effect } catch (e) {}` | Typed failures are values, not exceptions; only defects throw | `Effect.catchTag`, `Effect.tryPromise` |
| `throw new Error("not found")` | Becomes a defect: untracked in `E`, unhandleable by design | `return yield* new NotFound({ id })` |
| `Effect.all(items.map(fetch))` on external IO | Unbounded fan-out; failures interrupt the batch | `{ concurrency: n }`, or `Semaphore` |
| `Effect.runPromise(effect)` inside a service | Drops `E`, `R`, spans, and interruption | Compose the effect; run once at the edge |
| `Effect.provide(layer)` mid-call-chain | Hides the requirement from `R` and from tests | Provide once at the program edge |
| `Layer.provide(Postgres.layer({ url }))` inline per call | New layer identity each call defeats memoization and rebuilds the resource | Store the parameterized layer in a constant |
| `Option.getOrThrow(value)` in domain logic | Reintroduces the throw the `Option` existed to remove | `Option.match({ onNone, onSome })` |
| `null` / `undefined` as "missing" in a service interface | Loses the distinction between absent, failed, and not loaded | `Option` in the interface, `Result` for outcomes |
| Forking without join or scope | Fiber leak; cancellation never reaches the work | `Effect.forkScoped`, or join the child |
| `console.log` / `Date.now()` in effectful code | Bypasses log routing, spans, and the virtual clock in tests | `Effect.log*`, `Clock.currentTimeMillis`, `DateTime` |

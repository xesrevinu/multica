# Effect playbook

Read this when the work involves typed failure handling, retries or timeouts, concurrency, resource lifecycle, streaming, request batching, dependency injection, observability, or testing an effectful service. Every snippet follows the local v4 source; when you change one, re-check it there first (see `SKILL.md`, "Verify Effect APIs").

## Mental model

`Effect<A, E, R>` is a description, not a running promise:

- `A` — success value.
- `E` — expected failures the caller must handle. Bugs are **defects**, not `E`.
- `R` — services the effect needs. Satisfy it with a `Layer`, not by passing clients around.

Interruption is separate from both channels: a `Scope` teardown runs even when the fiber is interrupted. `run*` executes a description and belongs only at a program edge.

## Functions: `gen` for flow, `fn` for boundaries

```ts
// Inside a service method or an entry point (userId comes from that scope):
const program = Effect.gen(function* () {
  const users = yield* Users;
  const user = yield* users.findById(userId);
  yield* Effect.logInfo("loaded user", { userId: user.id });
  return user;
});

// Reusable, traced, cross-cutting behaviour attached as arguments (not .pipe).
export const loadUserTraced = Effect.fn("Users.load")(
  function* (id: UserId): Effect.fn.Return<User, UserNotFound> {
    const users = yield* Users;
    return yield* users.findById(id);
  },
  Effect.annotateLogs({ operation: "loadUser" }),
);

// Library internals and hot paths: same generator body, no span or stack capture.
// A better way to use Effect schema for decoding
export const validatePageSize = Effect.fnUntraced(function* (
  size: number,
): Effect.fn.Return<number, InvalidPageSize> {
  if (!Number.isInteger(size) || size <= 0) return yield* InvalidPageSize.make({ size });
  return size;
});
```

Rules:

- `Effect.fn("Name")` takes the function name so traces and stack frames match the code.
- Do not wrap a lone `Effect.gen` in a plain function; declare it with `Effect.fn`/`fnUntraced` or inline it.
- Pass cross-cutting combinators as `Effect.fn` arguments. A `.pipe` after `Effect.fn(...)` applies outside the function's span, so the span no longer names the call site.
- Annotate the return type with `Effect.fn.Return<A, E, R>` when inference is not obvious.

## Services and Layers

A service is an interface plus a tag. Construction lives in a static `layer`; callers only see the interface.

```ts
export class Smtp extends Context.Service<
  Smtp,
  {
    send(message: {
      readonly to: string;
      readonly subject: string;
      readonly body: string;
    }): Effect.Effect<void, SmtpError>;
  }
>()("app/Smtp") {
  static readonly layer = Layer.effect(
    Smtp,
    Effect.gen(function* () {
      const user = yield* Config.String("SMTP_USER");
      const pass = yield* Config.Redacted("SMTP_PASS");

      const transporter = yield* Effect.acquireRelease(
        Effect.sync(() => NodeMailer.createTransport({/* ... */})),
        (transporter) => Effect.sync(() => transporter.close()),
      );

      const send = Effect.fn("Smtp.send")(
        (message: { readonly to: string; readonly subject: string; readonly body: string }) =>
          Effect.tryPromise({
            try: () => transporter.sendMail({ from: "Acme <cloud@acme.com>", ...message }),
            catch: (cause) => new SmtpError({ cause }),
          }).pipe(Effect.asVoid),
      );

      return Smtp.of({ send });
    }),
  );
}
```

Wiring:

- `Layer.effect` for effectful construction, `Layer.sync` for pure construction, `Layer.effectDiscard` for a background task with no interface.
- `Layer.provideMerge` composes incrementally while exposing the provided layer too; `Layer.merge` builds independent layers in parallel.
- Provide the application layer once at the edge. A service method's `R` should be `never`; its dependencies come from its own layer.
- `Layer` memoizes by reference identity. Build parameterized layers in a constant or a `LayerMap`, never inline at a call site, or every call rebuilds the resource.
- Tag ids are globally unique and path-shaped: `"app/Smtp"`, `"app/Users"`.
- `Context.Reference` is for values with a default (feature flag, time zone, policy), not for behaviour with dependencies.

## Typed errors

```ts
export class ValidationError extends Schema.TaggedError<ValidationError>()("ValidationError", {
  message: Schema.String,
}) {}
export class NetworkError extends Schema.TaggedError<NetworkError>()("NetworkError", {
  statusCode: Schema.Int,
}) {}

declare const fetchUserName: (id: string) => Effect.Effect<string, NetworkError>;

// Fail: return the raise so the generator's control flow stays visible.
const loadName = Effect.fn("Users.loadName")(function* (
  id: string,
): Effect.fn.Return<string, ValidationError | NetworkError> {
  if (!id) return yield* new ValidationError({ message: "id is required" });
  return yield* fetchUserName(id);
});

// Recover specific tags; everything else stays in E.
export const nameOrFallback = loadName("123").pipe(
  Effect.catchTags({
    ValidationError: (error) => Effect.succeed(`Validation failed: ${error.message}`),
    NetworkError: (error) => Effect.succeed(`Request failed with ${error.statusCode}`),
  }),
);
```

- `Effect.catchTag` / `catchTags` name the failures you handle and keep the rest in `E`. `Effect.catch` takes every remaining failure at once; reach for it at a boundary, not as a default.
- `Effect.orDie` converts a case you have decided is impossible into a defect. Use it deliberately, at a boundary, not to silence `E`.
- Never `throw` for an expected failure, and never `try/catch` around `yield*`.
- `Effect.tryPromise({ try, catch })` is how a rejected promise becomes `E`. Without a `catch` that maps the `unknown` reason into a tagged error, the rejection is a defect.
- Need every failure instead of the first: `Effect.all(effects, { mode: "result" })` yields `Result`s rather than short-circuiting.
- Inspect without losing the channel: `Effect.exit` gives `Exit`, `Effect.flip` moves the failure into the success channel. Both are for tests and edges.

### Errors with reasons (v4)

When a service can fail in many ways, one binding error per service keeps `E` small while callers still discriminate:

```ts
export class NotFound extends Schema.TaggedError<NotFound>()("NotFound", { id: UserId }) {}
export class RateLimited extends Schema.TaggedError<RateLimited>()("RateLimited", { retryAfter: Schema.Int }) {}

// The service fails with UsersError; the actionable cases live in `reason`.
export class UsersError extends Schema.TaggedError<UsersError>()("UsersError", {
  reason: Schema.Union([NotFound, RateLimited]),
}) {}

const load = Effect.fn("Users.load")(function*(id: UserId) {
  const row = yield* repo.findById(id)
  if (row === undefined) return yield* new UsersError({ reason: new NotFound({ id }) })
  return row
})

// Handle one reason. RateLimited still fails as UsersError in E.
const friendly = load(id).pipe(
  Effect.catchReason("UsersError", "NotFound", (reason) => Effect.succeed(`missing ${reason.id}`))
)

// Several reasons, plus a fallback that sees UsersError with handled reasons removed.
const handled = load(id).pipe(
  Effect.catchReasons("UsersError", {
    NotFound: (reason) => Effect.succeed(`missing ${reason.id}`),
    RateLimited: (reason) => Effect.succeed(`retry in ${reason.retryAfter}ms`),
  }, (_reasons, error) => Effect.fail(error))
)

// Flatten when the caller wants plain catchTags over every reason.
const flat = load(id).pipe(Effect.unwrapReason("UsersError"))
```

- `catchReason`/`catchReasons` keep the parent error for unmatched reasons; `unwrapReason` removes the parent and puts the reason union into `E`. Pick one per boundary and do not mix both habits in the same service.
- Use the pattern where signatures would otherwise grow: HTTP endpoints, one service interface, anything crossing a layer. It is not a licence to nest `reason` inside `reason`.
- The binding error should carry what the reasons do not: ids, the operation, the input. A reason that needs context from its parent is a sign the split is wrong.
- These three functions exist in 4.x only. On 3.x, model the same idea with an explicit union and `catchTags`.

## Time: retry, timeout, repeat

```ts
export const productionRetrySchedule = Schedule.min([
  Schedule.exponential("250 millis"), // grow
  Schedule.spaced("10 seconds"), // capped delay
]).pipe(
  Schedule.jittered,
  Schedule.setInputType<HttpError>(),
  Schedule.while(({ input }) => input.retryable), // do not retry 4xx
);

export const loadUserWithRetry = fetchUserProfile("user-123").pipe(
  Effect.retry(productionRetrySchedule),
  Effect.timeout("5 seconds"),
);
```

- A retry policy is data: build it with `Schedule`, compose it with `min`/`max`, cap it with `spaced` or `recurs`, and make retryability depend on the typed error.
- `Effect.retry` re-runs the effect; `Effect.repeat` re-runs on success. Both consume a `Schedule`.
- `Effect.timeout` fails with the timeout error; `Effect.timeoutOption` returns `Option` when you want absence instead.
- Jitter is not optional for network retries: without it, callers retry in lockstep.
- Polling is `Stream.fromEffectSchedule(effect, schedule)`, not `while (true) { await sleep }`.

## Concurrency

```ts
// Bounded fan-out: the number is part of the contract, not a default.
const results = yield * Effect.forEach(items, processItem, { concurrency: 8 });

// A shared budget across independent call sites.
const semaphore = yield * Semaphore.make(5);
yield *
  Effect.forEach(requests, (request) => semaphore.withPermits(1)(handle(request)), {
    concurrency: "unbounded",
  });

// Structured background work: the child is interrupted with the scope.
yield * Effect.forkScoped(poller);
```

- Always set `concurrency` for work that touches a shared external resource. An unbounded default is fine only for in-memory, independent work.
- `Semaphore` bounds a resource across call sites; `{ concurrency }` bounds one collection.
- `Effect.forkChild` + `Fiber.join` for a child whose result you need; `Effect.forkScoped` for a long-lived child tied to a `Scope`; `Effect.forkDetach` only for work that must outlive its parent, which is rare and must be justified.
- A forked fiber you never join and never scope is a leak. Same for a subscription or a stream consumer.
- Failure in a concurrent fan-out interrupts siblings that already started; if partial results matter, use `mode: "result"` and decide per item.

## Resources

```ts
const withConnection = Effect.acquireRelease(
  Effect.tryPromise({ try: () => pool.connect(), catch: (cause) => new PoolError({ cause }) }),
  (connection) => Effect.sync(() => connection.release()),
);

const program = Effect.scoped(
  Effect.gen(function* () {
    const connection = yield* withConnection;
    return yield* query(connection);
  }),
);
```

- `acquireRelease` guarantees release on success, typed failure, defect, and interruption. `try/finally` does not survive interruption, which is exactly the case that leaks.
- Releases run in reverse acquisition order (LIFO), so dependencies acquired first are released last.
- A resource that must live for the whole app belongs in a `Layer`; one that must live for one operation belongs in `Effect.scoped`.
- `Effect.addFinalizer` attaches cleanup to the current scope when there is no acquire/release pair.
- Prefer a Layer-owned pool over constructing a client per call. For per-key resources (tenant, shard, connection string) use `LayerMap`, which builds, caches, and reaps layers by key.

## Streams, queues, PubSub

```ts
const enriched = orders.pipe(
  Stream.mapEffect(enrichOrder, { concurrency: 4 }), // backpressure-aware
  Stream.take(1_000),
);

yield * Stream.runDrain(enriched); // run for effects
const all = yield * Stream.runCollect(enriched); // collect when the set is bounded
```

- Use a `Stream` when the source is a sequence over time (pagination, polling, SSE, file lines, child-process output) instead of materializing an array.
- `Stream.mapEffect(..., { concurrency })` is the streaming equivalent of bounded fan-out; `Stream.runCollect` is only safe on a bounded stream.
- `Queue` for producer/consumer handoff, `PubSub` for one-to-many fan-out. Bounded queues apply backpressure; sliding and dropping queues do not.
- `Stream.fromAsyncIterable`, `Stream.paginate`, `Stream.callback`, and `Stream.fromEventListener` wrap existing APIs. Do not hand-roll a reader loop plus cancellation flags.

## Batching and caching

```ts
const makeResolver = Effect.gen(function* () {
  return (yield* RequestResolver.make<GetUserById>(
    Effect.fn(function* (entries) {
      // one upstream query for every entry in the batch
      for (const entry of entries) entry.completeUnsafe(/* Exit */);
    }),
  )).pipe(RequestResolver.setDelay("10 millis"), RequestResolver.withCache({ capacity: 1024 }));
});
```

- N+1 lookups inside one request are a `Request` + `RequestResolver` problem, solved by declaring the request and completing entries in a batch, with an optional per-request cache.
- `Cache.make({ lookup, capacity, timeToLive })` when repeated lookups should share one in-flight computation. Cache the effect, not the value, so concurrent misses deduplicate.
- Cache and batch coordination belong inside the service that owns the upstream call. Callers should not know about a cache.

## Testing

```ts
import { assert, describe, it } from "@effect/vitest";

describe("Users", () => {
  it.effect("returns the user", () =>
    Effect.gen(function* () {
      const users = yield* Users;
      const user = yield* users.findById(1);
      assert.strictEqual(user.name, "Ada Lovelace");
    }).pipe(Effect.provide(Users.testLayer)),
  );
});
```

- `it.effect` provides `TestContext` (virtual clock, deterministic ids); `it.live` uses real services.
- `assert` from `@effect/vitest` inside `it.effect`; `expect` does not integrate with the effect runtime.
- Provide a `testLayer` that implements the same service interface with in-memory state, plus test-only services (`Ref`-backed stores) instead of mocking modules.
- `TestClock.adjust` drives `Effect.sleep`, retries, and schedules without wall-clock waiting.
- Provide a fresh layer per test unless the resource is expensive; shared layers (`it.layer`) leak state between tests.

## Running at the edge

```ts
// Process entry point: installs SIGINT/SIGTERM handling and interrupts fibers.
NodeRuntime.runMain(Layer.launch(App.layer), { disableErrorReporting: true });

// Framework bridge: build the runtime once, dispose it on shutdown.
export const runtime = ManagedRuntime.make(TodoRepo.layer, { memoMap: Layer.makeMemoMapUnsafe() });

app.get("/todos", async (context) =>
  context.json(await runtime.runPromise(TodoRepo.use((repo) => repo.getAll))),
);

process.once("SIGTERM", () => void runtime.dispose());
```

- `NodeRuntime.runMain` / `BunRuntime.runMain` own signal handling and graceful interruption. Do not reimplement shutdown.
- `ManagedRuntime` is the seam for a framework that cannot return an `Effect`. Keep handlers thin: one `runPromise` per request, all logic in services.
- Decode request bodies with `Schema.decodeUnknownSync` (or the async decoder) at the handler boundary and return a 4xx on decode failure. Do not pass framework `unknown` inward.
- Disposing the runtime is what releases Layer-owned resources; without it, pools and sockets leak on shutdown.

## Traces and attributes

```ts
// Effect.fn gives the call a span named after the function.
const charge = Effect.fn("Checkout.charge")(function*(orderId: string) {
  const order = yield* repo.find(orderId)
  // Data discovered mid-flight goes on the current span.
  yield* Effect.annotateCurrentSpan({ "order.total": order.total, "customer.tier": order.tier })
  return yield* payments.charge(order)
})

// A named span for a unit of work that is not a service method.
const load = fetchUser(id).pipe(
  Effect.withSpan("Users.fetch", { attributes: { "user.id": id } })
)
```

- Name spans `<domain>.<operation>` (`Checkout.charge`, `Users.fetch`), keep the name low-cardinality, and put ids, emails, and counts in attributes.
- `Effect.fn("Name")` already creates the span; do not wrap it in `withSpan` with the same name. `Effect.fnUntraced` is for hot-path helpers where a span per call costs more than it explains.
- `Effect.withSpan` attributes are fixed at creation; `Effect.annotateCurrentSpan({ ... })` adds what you learn later. Keep values bounded — never whole payloads or unbounded lists.
- `Layer.withSpan("App.layer")` and `Stream.withSpan` cover startup and pipeline segments that have no per-call span.
- A span closes with the effect's `Exit`, so typed failures and defects are recorded without extra code. Do not catch only to "log the error for tracing".
- One span per operation, not per item: inside `Stream.mapEffect` or a loop, wrap the batch and annotate the count.

## Semantics that change your design

- **Defect vs error.** A defect (`Effect.die`, thrown exception, unhandled rejection) skips typed recovery and surfaces as a crash. If a caller is expected to act on it, it belongs in `E`.
- **Interruption is cooperative.** Any effect can be interrupted at an `yield*` boundary, so partial writes need `acquireRelease`, transactions, or idempotent operations.
- **Layers build once.** Memoization is by layer identity, not by call site. Inline `SomeLayer.make(...)` in a request path rebuilds the resource every time.
- **Providing early hides requirements.** `Effect.provide` inside a service makes the dependency invisible in `R` and untestable from outside.
- **`R` should shrink toward the edge.** If a deeply nested helper declares a service, that service is a real dependency of the operation, not an implementation detail.

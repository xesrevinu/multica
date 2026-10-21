# Effect at the edges: HTTP, SQL, platform

Read this when the work adds an API, talks to a database, reads files, spawns processes, or connects a socket. Core rule: **depend on the core service interface, provide the platform layer once at the entry point, and keep definitions separate from implementations.**

## HTTP client

`HttpClient` is a service, so it is a dependency like any other and tests can replace it.

```ts
export class JsonPlaceholder extends Context.Service<JsonPlaceholder, {
  readonly allTodos: Effect.Effect<ReadonlyArray<Todo>, JsonPlaceholderError>
}>()("app/JsonPlaceholder") {
  static readonly layer = Layer.effect(
    JsonPlaceholder,
    Effect.gen(function*() {
      // One client with the cross-cutting behaviour applied once.
      const client = (yield* HttpClient.HttpClient).pipe(
        HttpClient.mapRequest(flow(
          HttpClientRequest.prependUrl("https://jsonplaceholder.typicode.com"),
          HttpClientRequest.acceptJson
        )),
        HttpClient.filterStatusOk,                       // non-2xx becomes an error
        HttpClient.retryTransient({                      // network errors and 5xx only
          schedule: Schedule.exponential(100),
          times: 3
        })
      )

      const allTodos = client.get("/todos").pipe(
        Effect.flatMap(HttpClientResponse.schemaBodyJson(Schema.Array(Todo))),
        Effect.mapError((cause) => new JsonPlaceholderError({ cause })),
        Effect.withSpan("JsonPlaceholder.allTodos")
      )

      return JsonPlaceholder.of({ allTodos })
    })
  )
}
```

- Build the client once in a Layer: base URL, auth header, `filterStatusOk`, retry policy, and telemetry belong there, not in each call.
- Decode bodies with `HttpClientResponse.schemaBodyJson(Schema)`; never `as Todo` on `response.json()`.
- Map transport failures into a domain tagged error at the service boundary. `HttpClientError` should not escape into feature code.
- Provide the implementation by platform: `FetchHttpClient.layer`, `NodeHttpClient.layer`, or `BrowserHttpClient.layer`.

## HttpApi: schema-first server and typed client

```ts
// api.ts — shared by server and client, no server imports here.
export const Api = HttpApi.make("api").add(
  HttpApiGroup.make("users").add(
    HttpApiEndpoint.get("getUser", "/users/:id", {
      params: { id: Schema.FiniteFromString },
      query: { page: Schema.optional(Schema.FiniteFromString) },
      success: User,
      error: UserNotFound.pipe(HttpApiSchema.status(404))
    }),
    HttpApiEndpoint.post("createUser", "/users", {
      payload: UserCreate,
      success: User
    })
  )
)

// server.ts
const UserHandlers = HttpApiBuilder.group(Api, "users", Effect.fn(function*(handlers) {
  return handlers.handleAll({
    getUser: Effect.fnUntraced(function*({ params }) {
      const users = yield* Users
      return yield* users.findById(params.id)
    }),
    createUser: Effect.fnUntraced(function*({ payload }) {
      const users = yield* Users
      return yield* users.create(payload)
    })
  })
}))

const Routes = HttpApiBuilder.layer(Api, { openapiPath: "/openapi.json" }).pipe(
  Layer.provide(UserHandlers)
)
const DocsRoute = HttpApiScalar.layer(Api, { path: "/docs" })
const AllRoutes = Layer.mergeAll(Routes, DocsRoute)

Layer.launch(HttpRouter.serve(AllRoutes).pipe(
  Layer.provide(NodeHttpServer.layer(createServer, { port: 3000 }))
)).pipe(NodeRuntime.runMain)

// Serverless / framework edge:
export const { handler, dispose } = HttpRouter.toWebHandler(
  AllRoutes.pipe(Layer.provide(HttpServer.layerServices))
)

// client.ts
export class ApiClient extends Context.Service<ApiClient, HttpApiClient.ForApi<typeof Api>>()("app/ApiClient") {
  static readonly layer = Layer.effect(
    ApiClient,
    HttpApiClient.make(Api, {
      transformClient: (client) => client.pipe(
        HttpClient.mapRequest(flow(HttpClientRequest.prependUrl("http://localhost:3000"))),
        HttpClient.retryTransient({ schedule: Schedule.exponential(100), times: 3 })
      )
    })
  ).pipe(Layer.provide(FetchHttpClient.layer))
}
```

- The endpoint is the contract: params, query, payload, success, and error are schemas. The handler receives decoded values, and the client gets the same types.
- Middleware (auth, tracing, CORS) is declared with `HttpApiMiddleware` and implemented per side: `HttpApiMiddleware.layerClient` for the client, a server layer for handlers. Do not read headers ad hoc inside every handler.
- `HttpApiBuilder.layer` + `HttpRouter.serve` for a long-running server; `HttpRouter.toWebHandler` when the host owns the server (Next.js, Lambda, Bun).
- Test through the typed client instead of a port: `HttpApiTest.groups(Api, ["users"])` wired to handler layers plus `HttpServer.layerServices` exercises routing, encoding, and decoding in-process.
- Rule of thumb: if the client hand-writes URLs or decodes JSON manually, the API definition is missing an endpoint.

## SQL

```ts
const SqlLayer = SqliteClient.layer({ filename: ":memory:" })   // swap for @effect/sql-pg, @effect/sql-mysql2

const MigratorLayer = SqliteMigrator.layer({
  loader: SqliteMigrator.fromRecord({
    "0001_create_groups": Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      yield* sql`CREATE TABLE groups (id TEXT PRIMARY KEY, name TEXT NOT NULL)`
    })
  })
})

// Anything above this layer sees a migrated database.
const SqlLive = MigratorLayer.pipe(Layer.provideMerge(SqlLayer))

export class Groups extends Context.Service<Groups, {
  create(name: string, slug: string): Effect.Effect<Group>
  findById(id: GroupId): Effect.Effect<Group, GroupNotFound>
  readonly list: Effect.Effect<Array<Group>>
}>()("app/Groups") {
  static readonly layer = Layer.effect(
    Groups,
    Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient

      const repo = yield* SqlModel.makeRepository(Group, {
        tableName: "groups",
        idColumn: "id",
        spanPrefix: "Groups"
      })

      const listAll = SqlSchema.findAll({
        Request: Schema.Void,
        Result: Group,
        execute: () => sql`SELECT * FROM groups ORDER BY createdAt`
      })

      const create = Effect.fn("Groups.create")((name: string, slug: string) =>
        Group.insert.makeEffect({ name, slug }).pipe(Effect.flatMap(repo.insert), Effect.orDie)
      )

      const findById = Effect.fn("Groups.findById")((id: GroupId) =>
        repo.findById(id).pipe(
          Effect.catchTags({
            NoSuchElementError: () => new GroupNotFound({ id }),
            SchemaError: Effect.die,
            SqlError: Effect.die
          })
        )
      )

      return Groups.of({ create, findById, list: listAll().pipe(Effect.orDie) })
    })
  ).pipe(Layer.provide(SqlLive))
}
```

- One model class owns the shape. `Model.Class` with `Model.UuidV4Insert`, `Model.DateTimeInsert/Update`, `FieldExcept`, `FieldOnly`, `Field` derives the select/insert/update/JSON variants instead of four parallel types.
- `SqlModel.makeRepository` covers insert/update/findById/delete. Use `SqlSchema.findAll/findOne/findOneOption` (or `SqlResolver` for batched lookups, `SqlStream` for large results) for everything else — every query decodes into a schema.
- Migrations are effects keyed `<id>_<name>`, run once in id order. `fromFileSystem` loads SQL files instead of an inline record. Compose the migrator with `Layer.provideMerge` so downstream layers cannot see an unmigrated database.
- `Model.insert.makeEffect({...})` fills generated ids and timestamps from the Effect clock, so `TestClock` makes them deterministic in tests.
- Errors: `NoSuchElementError` is the domain "not found" and should become a tagged error. `SqlError` and `SchemaError` are defects unless the caller can act on them; decide once per service, usually `Effect.orDie` in the repository and a typed error at the boundary.
- A transaction is an effect combinator over the client, not a service method that takes a connection. Keep SQL inside the repository; feature code depends on `Groups`, never on `SqlClient`.
- The `sql` tagged template parameterizes values. String concatenation with user input is a bug even when the schema validates it.

## Platform services

Core capabilities live in `effect`; the OS implementations live in a platform package. Depend on the service, provide the layer.

| Capability | Core module | Node implementation | Also |
|---|---|---|---|
| File system | `effect/FileSystem` | `NodeFileSystem.layer` | `@effect/platform-bun` |
| Paths | `effect/Path` | `NodePath.layer` | `Path.layerPosix` / `Path.layerWin32` |
| Child processes | `effect/process` (`ChildProcess`, `ChildProcessSpawner`) | `NodeChildProcessSpawner.layer` | |
| Stdio, terminal | `effect/Stdio`, `effect/Terminal` | `NodeStdio`, `NodeTerminal` | |
| Crypto | `effect/Crypto` | `NodeCrypto` | `BrowserCrypto` |
| HTTP server | `effect/http` (`HttpServer`, `HttpRouter`) | `NodeHttpServer.layer` | `HttpRouter.toWebHandler` |
| Sockets | `effect/socket` | `NodeSocket` | `BrowserSocket` |
| Workers | `effect/workers` | `NodeWorker`, `NodeWorkerRunner` | `BrowserWorker` |

- `NodeServices.layer` provides child process spawner, crypto, filesystem, path, stdio, and terminal at once. Use it for a Node CLI or server; provide individual layers when a program should not have a capability.
- Domain code must not import `node:fs`, `node:path`, `node:child_process`, or `Bun.*`. That coupling is what makes it untestable and platform-locked.
- Tests: provide in-memory implementations (`FileSystem.layerNoop`, `Stdio.layerTest`, the driver's memory/test layer) instead of touching the real filesystem or network.

```ts
// Services come from the context; the entrypoint supplies the Node layers.
const spawner = yield* ChildProcessSpawner.ChildProcessSpawner

// Files: the layer supplies the implementation, the service calls are portable.
const loadConfig = Effect.fn("config.load")(function*(path: string) {
  const fs = yield* FileSystem.FileSystem
  const content = yield* fs.readFileString(path)
  return yield* Schema.decodeUnknownEffect(Config)(JSON.parse(content))
})

// Processes: `string` collects, `lines` splits, `spawn` streams a long-running command.
const version = spawner.string(ChildProcess.make("node", ["--version"]))
const changed = spawner.lines(ChildProcess.make("git", ["diff", "--name-only", "main...HEAD"]))
const handle = yield* spawner.spawn(ChildProcess.make("pnpm", ["lint-fix"]))
yield* handle.all.pipe(Stream.decodeText(), Stream.splitLines, Stream.runForEach(Console.log))
```

- Wrap `JSON.parse` in `Schema.decodeUnknownEffect`/`decodeUnknownSync`: the parse only proves the text is JSON, not that it has your shape.
- For a child process, choose the shape you need: collected string, line stream, or a live handle with `exitCode`. Set `extendEnv: true` when the tool needs the parent environment, and pass credentials through the environment, not argv.

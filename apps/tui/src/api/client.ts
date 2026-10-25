import { Context, Effect, Layer } from "effect"
import { FetchHttpClient, HttpClient, HttpClientRequest } from "effect/http"
import { currentTraceparent } from "../traceparent"
import type { Connection } from "../config"
import { make, type Multica } from "./generated"

export class MulticaClient extends Context.Service<MulticaClient, Multica>()("@opencode-ai/multica-tui/MulticaClient") {}

// Every request to the internal API carries the bearer token and the active
// workspace, matching the Go CLI's `APIClient.setHeaders`.
export const layer = (connection: Connection, workspaceID = connection.workspaceID) =>
  Layer.effect(
    MulticaClient,
    Effect.gen(function* () {
      const httpClient = yield* HttpClient.HttpClient
      return make(
        HttpClient.mapRequestEffect(httpClient, (request) =>
          Effect.gen(function* () {
            const headers: Record<string, string> = {
              Authorization: `Bearer ${connection.token}`,
              "X-Workspace-ID": workspaceID,
              "X-Client-Platform": "multica-tui",
            }
            const traceparent = yield* currentTraceparent
            if (traceparent) headers.traceparent = traceparent
            return request.pipe(
              HttpClientRequest.prependUrl(connection.serverUrl),
              HttpClientRequest.setHeaders(headers),
            )
          }),
        ),
      )
    }),
  ).pipe(Layer.provide(FetchHttpClient.layer))

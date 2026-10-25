import { Option, Schema } from "effect"
import type { IssuePullRequest } from "./api/generated"

export type { IssuePullRequest }

// Kept for unit tests and offline fixtures. Runtime fetch goes through the
// Effect-generated Multica client in `data.ts`.
const IssuePullRequestSchema = Schema.Struct({
  id: Schema.String,
  number: Schema.Number,
  title: Schema.String,
  state: Schema.String,
  html_url: Schema.String,
  repo_owner: Schema.String,
  repo_name: Schema.String,
  branch: Schema.optionalKey(Schema.NullOr(Schema.String)),
  merged_at: Schema.optionalKey(Schema.NullOr(Schema.String)),
})

const IssuePullRequestsResponse = Schema.Struct({
  pull_requests: Schema.optionalKey(Schema.Array(IssuePullRequestSchema)),
})

const decodeResponse = Schema.decodeUnknownOption(Schema.fromJsonString(IssuePullRequestsResponse))

export function decodeIssuePullRequests(text: string): readonly IssuePullRequest[] | undefined {
  const decoded = decodeResponse(text)
  if (Option.isNone(decoded)) return undefined
  return decoded.value.pull_requests ?? []
}

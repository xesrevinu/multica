import { z } from "zod";

// Schemas for /api/orchestrator issue-centric views. Every optional server
// field is optional/nullable here too: issues may have no project, assignee,
// metadata, PR, worktree or runs. Unknown enum values degrade, never throw.

const str = z.string();
const optStr = z.string().nullish().transform((v) => v ?? undefined);
const num = z.number().catch(0);

export const AttentionBucketSchema = z.enum(["needs_you", "running", "queued", "settled"]).catch("queued");

export const OrchRunSchema = z.object({
  task_id: str,
  agent_id: str.catch(""),
  agent: str.catch(""),
  status: str.catch(""),
  created_at: str.catch(""),
  started_at: optStr,
  ended_at: optStr,
  error: optStr,
});

export const OrchPRSummarySchema = z.object({
  number: z.number(),
  url: str.catch(""),
  title: str.catch(""),
  state: str.catch(""),
  branch: optStr,
  head_sha: optStr,
  additions: num,
  deletions: num,
  changed_files: num,
  checks: optStr,
  mergeable: optStr,
  merge_state: optStr,
  repo: str.catch(""),
});

export const OrchIssueSchema = z.object({
  id: str,
  key: str,
  number: num,
  title: str.catch(""),
  status: str.catch("backlog"),
  priority: str.catch("none"),
  project: z.object({ id: str, title: str.catch("") }).nullish().transform((v) => v ?? undefined),
  parent_id: optStr,
  assignee: z.object({ type: str, id: str, name: str.catch("") }).nullish().transform((v) => v ?? undefined),
  labels: z.array(str).nullish().transform((v) => v ?? []),
  orchestrated: z.boolean().catch(false),
  mode: optStr,
  phase: optStr,
  risk: optStr,
  paused: z.boolean().catch(false),
  attempts: z.object({ impl: num, review: num, infra: num }).nullish().transform((v) => v ?? undefined),
  attention: z
    .object({ bucket: AttentionBucketSchema, reason: optStr, since: str.catch("") })
    .catch({ bucket: "queued", reason: undefined, since: "" }),
  running: OrchRunSchema.nullish().catch(undefined).transform((v) => v ?? undefined),
  pr: OrchPRSummarySchema.nullish().catch(undefined).transform((v) => v ?? undefined),
  worktree: z.object({ path: optStr, branch: optStr }).nullish().catch(undefined).transform((v) => v ?? undefined),
  children: z.object({ total: num, done: num }).catch({ total: 0, done: 0 }),
  created_at: str.catch(""),
  updated_at: str.catch(""),
});

export const OrchTransitionSchema = z.object({
  id: z.number(),
  tid: str.catch(""),
  state: str.catch(""),
  why: optStr,
  status: optStr,
  phase: optStr,
  assignee: optStr,
  error: optStr,
  created_at: str.catch(""),
});

export const OrchTimelineSchema = z.object({
  issue: OrchIssueSchema,
  flow: z.record(z.string(), z.string()).nullish().transform((v) => v ?? {}),
  runs: z.array(OrchRunSchema).nullish().catch([]).transform((v) => v ?? []),
  transitions: z.array(OrchTransitionSchema).nullish().catch([]).transform((v) => v ?? []),
  waiting: optStr,
  last_error: optStr,
  backoff_until: optStr,
});

export const OrchPRCheckSchema = z.object({
  name: str.catch(""),
  status: str.catch(""),
  conclusion: optStr,
  url: optStr,
  app: optStr,
});

export const OrchPRCommitSchema = z.object({
  sha: str,
  message: str.catch(""),
  author: str.catch(""),
  date: str.catch(""),
});

export const OrchPRDetailSchema = z.object({
  number: z.number(),
  title: str.catch(""),
  state: str.catch(""),
  draft: z.boolean().catch(false),
  url: str.catch(""),
  head_ref: str.catch(""),
  head_sha: str.catch(""),
  base_ref: str.catch(""),
  mergeable: z.boolean().nullish().catch(null).transform((v) => v ?? null),
  mergeable_state: str.catch("unknown"),
  behind_by: num,
  ahead_by: num,
  additions: num,
  deletions: num,
  changed_files: num,
  commits: z.array(OrchPRCommitSchema).nullish().catch([]).transform((v) => v ?? []),
  checks: z.array(OrchPRCheckSchema).nullish().catch([]).transform((v) => v ?? []),
  merged_at: optStr,
});

const NO_ABILITIES = { read_checks: false, update_branch: false, rerun: false, merge: false };

export const OrchPRCardSchema = z.object({
  mode: z.enum(["github", "local"]).catch("github"),
  orchestrated: z.boolean().catch(false),
  live: z.boolean().catch(false),
  pr: OrchPRDetailSchema.nullish().catch(null).transform((v) => v ?? null),
  summary: OrchPRSummarySchema.nullish().catch(null).transform((v) => v ?? null),
  can: z
    .object({ read_checks: z.boolean().catch(false), update_branch: z.boolean().catch(false), rerun: z.boolean().catch(false), merge: z.boolean().catch(false) })
    .catch(NO_ABILITIES),
  error: optStr,
});

export type OrchIssue = z.infer<typeof OrchIssueSchema>;
export type OrchRun = z.infer<typeof OrchRunSchema>;
export type OrchTransition = z.infer<typeof OrchTransitionSchema>;
export type OrchTimeline = z.infer<typeof OrchTimelineSchema>;
export type OrchPRCard = z.infer<typeof OrchPRCardSchema>;
export type OrchPRDetail = z.infer<typeof OrchPRDetailSchema>;
export type OrchAttentionBucket = z.infer<typeof AttentionBucketSchema>;

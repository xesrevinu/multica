import { Effect, ManagedRuntime, Schedule } from "effect"
import { HttpClientError } from "effect/http"
import { parseOrchIssueList, parseOrchPRCard, parseOrchTimeline, parseOrchWorktrees } from "@multica/core/orchestrator/parse"
import type { OrchFlowAction, OrchPROp } from "@multica/core/orchestrator/flow"
import { layer, MulticaClient } from "./api/client"
import type * as Api from "./api/generated"
import type { Connection } from "./config"

export type { Connection }

// The UI consumes plain promises; Effect stays at the boundary. Each call gets
// a 30s timeout and retries transient transport failures and 429/5xx twice.
const transient = (error: unknown) => {
  if (!(error instanceof HttpClientError.HttpClientError)) return false
  const reason = error.reason
  if (reason._tag === "TransportError") return true
  if (reason._tag === "StatusCodeError") return reason.response.status === 429 || reason.response.status >= 500
  return false
}

const statusOf = (error: unknown) => {
  if (!(error instanceof HttpClientError.HttpClientError)) return undefined
  const reason = error.reason
  return reason._tag === "StatusCodeError" ? reason.response.status : undefined
}

const policy = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.retry({ schedule: Schedule.exponential("250 millis"), times: 2, while: transient }),
    Effect.timeout("30 seconds"),
  )

export type DataClient = ReturnType<typeof makeDataClient>

export function makeDataClient(connection: Connection, workspaceID = connection.workspaceID) {
  const runtime = ManagedRuntime.make(layer(connection, workspaceID))
  const run = <A, E>(effect: Effect.Effect<A, E, MulticaClient>, options?: { signal?: AbortSignal }) =>
    runtime.runPromise(policy(effect), options)

  return {
    connection,
    workspaceID,
    dispose: () => runtime.dispose(),
    getMe: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.getMe(undefined)), options),
    listWorkspaces: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listWorkspaces(undefined)), options),
    getWorkspace: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.getWorkspace(id, undefined)), options),
    listMembers: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listMembers(id, undefined)), options),
    listIssues: (params?: typeof Api.ListIssuesParams.Encoded, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listIssues({ params })), options),
    getIssue: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.getIssue(id, undefined)), options),
    listIssueComments: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listIssueComments(id, undefined)), options),
    listIssueTimeline: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listIssueTimeline(id, undefined)), options),
    listIssueTaskRuns: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listIssueTaskRuns(id, undefined)), options),
    listIssueChildren: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listIssueChildren(id, undefined)), options),
    listIssuePullRequests: async (id: string, options?: { signal?: AbortSignal }) => {
      try {
        const body = await run(
          Effect.flatMap(MulticaClient, (client) => client.listIssuePullRequests(id, undefined)),
          options,
        )
        return body.pull_requests ?? []
      } catch (error) {
        // Older servers omit the route — same empty list the sidebar shows.
        if (statusOf(error) === 404) return []
        throw error
      }
    },
    listIssueStatuses: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listIssueStatuses(undefined)), options),
    listLabels: (params?: typeof Api.ListLabelsParams.Encoded, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listLabels({ params })), options),
    listProjects: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listProjects(undefined)), options),
    getProject: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.getProject(id, undefined)), options),
    listProjectResources: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listProjectResources(id, undefined)), options),
    listAgents: (params?: typeof Api.ListAgentsParams.Encoded, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listAgents({ params })), options),
    getAgent: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.getAgent(id, undefined)), options),
    listAgentTasks: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listAgentTasks(id, undefined)), options),
    listAgentSkills: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listAgentSkills(id, undefined)), options),
    listRuntimes: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listRuntimes(undefined)), options),
    listSkills: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listSkills(undefined)), options),
    listWorkingAgents: async (options?: { signal?: AbortSignal }) => {
      try {
        return await run(Effect.flatMap(MulticaClient, (client) => client.listWorkingAgents(undefined)), options)
      } catch (error) {
        // Older self-hosted builds omit this route; overview treats empty as "none working".
        if (statusOf(error) === 404) return []
        throw error
      }
    },
    // Orchestrator HTTP goes through the Effect client; payload semantics stay
    // on the shared zod parsers so Web and TUI cannot drift.
    listOrchIssues: async (
      params?: { settledLimit?: number; project?: string },
      options?: { signal?: AbortSignal },
    ) => {
      try {
        const raw = await run(
          Effect.flatMap(MulticaClient, (client) =>
            client.listOrchIssues({
              params: {
                settled_limit: params?.settledLimit,
                project: params?.project,
              },
            }),
          ),
          options,
        )
        return parseOrchIssueList(raw)
      } catch (error) {
        const status = statusOf(error)
        if (status === 404 || status === 501) return { issues: [], settledTotal: 0 }
        throw error
      }
    },
    getOrchTimeline: async (key: string, options?: { signal?: AbortSignal }) => {
      try {
        const raw = await run(
          Effect.flatMap(MulticaClient, (client) => client.getOrchIssueTimeline(key, undefined)),
          options,
        )
        return parseOrchTimeline(raw) ?? undefined
      } catch (error) {
        const status = statusOf(error)
        if (status === 404 || status === 409) return undefined
        throw error
      }
    },
    listOrchTeams: async (options?: { signal?: AbortSignal }) => {
      try {
        const body = await run(
          Effect.flatMap(MulticaClient, (client) => client.listOrchTeams(undefined)),
          options,
        )
        return body.teams ?? []
      } catch (error) {
        const status = statusOf(error)
        if (status === 404 || status === 501) return []
        throw error
      }
    },
    orchFlowAction: (key: string, action: OrchFlowAction, body?: { reason?: string }) =>
      run(
        Effect.flatMap(MulticaClient, (client) =>
          client.orchFlowAction(key, action, { payload: body?.reason ? { reason: body.reason } : {} }),
        ),
      ),
    getOrchPRCard: async (key: string, number?: number, options?: { signal?: AbortSignal }) => {
      try {
        const raw = await run(
          Effect.flatMap(MulticaClient, (client) =>
            client.getOrchIssuePR(key, { params: number !== undefined ? { number } : undefined }),
          ),
          options,
        )
        return parseOrchPRCard(raw) ?? undefined
      } catch (error) {
        const status = statusOf(error)
        if (status === 404 || status === 501) return undefined
        throw error
      }
    },
    orchPROp: (
      key: string,
      op: OrchPROp,
      opts?: { number?: number; headSha?: string },
      options?: { signal?: AbortSignal },
    ) =>
      run(
        Effect.flatMap(MulticaClient, (client) =>
          client.orchPROp(key, op, {
            params: opts?.number !== undefined ? { number: opts.number } : undefined,
            payload: opts?.headSha ? { head_sha: opts.headSha } : {},
          }),
        ),
        options,
      ),
    listOrchWorktrees: async (issueKey?: string, options?: { signal?: AbortSignal }) => {
      try {
        const raw = await run(
          Effect.flatMap(MulticaClient, (client) =>
            client.listOrchWorktrees({ params: issueKey ? { issue: issueKey } : undefined }),
          ),
          options,
        )
        return parseOrchWorktrees(raw)
      } catch (error) {
        const status = statusOf(error)
        if (status === 404 || status === 501) return { hubs: [], worktrees: [] }
        throw error
      }
    },
    pauseOrchTeam: (project: string, reason?: string, options?: { signal?: AbortSignal }) =>
      run(
        Effect.flatMap(MulticaClient, (client) =>
          client.pauseOrchTeam(project, { payload: reason ? { reason } : {} }),
        ),
        options,
      ),
    resumeOrchTeam: (project: string, options?: { signal?: AbortSignal }) =>
      run(
        Effect.flatMap(MulticaClient, (client) => client.resumeOrchTeam(project, { payload: {} })),
        options,
      ),
    createIssue: (payload: Api.CreateIssueRequest, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.createIssue({ payload })), options),
    updateIssue: (id: string, payload: Api.UpdateIssueRequest, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.updateIssue(id, { payload })), options),
    createComment: (issueId: string, payload: Api.CreateCommentRequest, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.createComment(issueId, { payload })), options),
    getActiveTasksForIssue: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.getActiveTasksForIssue(id, undefined)), options),
    cancelTask: (issueId: string, taskId: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.cancelTask(issueId, taskId, undefined)), options),
    listTaskMessages: (taskId: string, params?: { since?: number }, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listTaskMessages(taskId, { params })), options),
    getSkill: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.getSkill(id, undefined)), options),
    listInbox: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listInbox(undefined)), options),
    listArchivedInbox: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listArchivedInbox(undefined)), options),
    getInboxUnreadCount: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.getInboxUnreadCount(undefined)), options),
    markAllInboxRead: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.markAllInboxRead(undefined)), options),
    archiveAllInbox: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.archiveAllInbox(undefined)), options),
    archiveAllReadInbox: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.archiveAllReadInbox(undefined)), options),
    archiveCompletedInbox: (options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.archiveCompletedInbox(undefined)), options),
    markInboxRead: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.markInboxRead(id, undefined)), options),
    markInboxUnread: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.markInboxUnread(id, undefined)), options),
    archiveInbox: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.archiveInbox(id, undefined)), options),
    unarchiveInbox: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.unarchiveInbox(id, undefined)), options),
    listChatSessions: (params?: typeof Api.ListChatSessionsParams.Encoded, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listChatSessions({ params })), options),
    createChatSession: (payload: Api.CreateChatSessionRequest, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.createChatSession({ payload })), options),
    getChatSession: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.getChatSession(id, undefined)), options),
    listChatMessages: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listChatMessages(id, undefined)), options),
    sendChatMessage: (id: string, payload: Api.SendChatMessageRequest, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.sendChatMessage(id, { payload })), options),
    setChatSessionPinned: (id: string, pinned: boolean, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.setChatSessionPinned(id, { payload: { pinned } })), options),
    setChatSessionArchived: (id: string, archived: boolean, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.setChatSessionArchived(id, { payload: { archived } })), options),
    listAutopilots: (params?: typeof Api.ListAutopilotsParams.Encoded, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listAutopilots({ params })), options),
    getAutopilot: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.getAutopilot(id, undefined)), options),
    triggerAutopilot: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.triggerAutopilot(id, undefined)), options),
    listAutopilotRuns: (id: string, params?: { limit?: number; offset?: number }, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.listAutopilotRuns(id, { params })), options),
    getDashboardUsageByAgent: (params?: { days?: number; project_id?: string; tz?: string }, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.getDashboardUsageByAgent({ params })), options),
    updateComment: (id: string, payload: Api.UpdateCommentRequest, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.updateComment(id, { payload })), options),
    deleteComment: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.deleteComment(id, undefined)), options),
    resolveComment: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.resolveComment(id, undefined)), options),
    unresolveComment: (id: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.unresolveComment(id, undefined)), options),
    attachLabel: (issueId: string, labelId: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.attachLabel(issueId, { payload: { label_id: labelId } })), options),
    detachLabel: (issueId: string, labelId: string, options?: { signal?: AbortSignal }) =>
      run(Effect.flatMap(MulticaClient, (client) => client.detachLabel(issueId, labelId, undefined)), options),
  }
}

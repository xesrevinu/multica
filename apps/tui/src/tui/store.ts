import type { InboxItem } from "@multica/core/types"
import type {
  Agent,
  Autopilot,
  ChatSession,
  Issue,
  IssuePriority,
  IssueStatusEntry,
  Label,
  MemberWithUser,
  Project,
  Runtime,
  SkillSummary,
  User,
  WorkingAgent,
  Workspace,
} from "../api/generated"
import { formatRelative, shortId } from "../format"
import { bucketLabel, type OrchIssue } from "../orchestrator"
import type { HeaderCell, Row } from "./table"
import { AGENT_STATUS_TONE, CATEGORY_TONE, PRIORITY_TONE, PROJECT_STATUS_TONE } from "./tones"

export type Store = {
  me?: User
  workspace?: Workspace
  members: readonly MemberWithUser[]
  statuses: readonly IssueStatusEntry[]
  issues: readonly Issue[]
  issuesTotal: number
  agents: readonly Agent[]
  runtimes: readonly Runtime[]
  projects: readonly Project[]
  skills: readonly SkillSummary[]
  working: readonly WorkingAgent[]
  inbox: readonly InboxItem[]
  inboxArchived: readonly InboxItem[]
  chats: readonly ChatSession[]
  autopilots: readonly Autopilot[]
  labels: readonly Label[]
  orchIssues: readonly OrchIssue[]
  orchSettledTotal: number
}

export const emptyStore: Store = {
  members: [],
  statuses: [],
  issues: [],
  issuesTotal: 0,
  agents: [],
  runtimes: [],
  projects: [],
  skills: [],
  working: [],
  inbox: [],
  inboxArchived: [],
  chats: [],
  autopilots: [],
  labels: [],
  orchIssues: [],
  orchSettledTotal: 0,
}

export type Lookups = {
  actorName: (type: string | null, id: string | null) => string
  statusEntry: (key: string) => IssueStatusEntry | undefined
  statusLabel: (issue: Issue) => string
  projectTitle: (id: string | null) => string
  runtimeName: (id: string) => string
  runtime: (id: string) => Runtime | undefined
}

export function makeLookups(store: Store): Lookups {
  const agents = new Map(store.agents.map((agent) => [agent.id, agent]))
  // Assignee/creator actor ids for members are user ids, not membership row ids.
  const members = new Map(store.members.map((member) => [member.user_id, member]))
  const statuses = new Map(store.statuses.map((status) => [status.key, status]))
  const projects = new Map(store.projects.map((project) => [project.id, project]))
  const runtimes = new Map(store.runtimes.map((runtime) => [runtime.id, runtime]))
  return {
    actorName: (type, id) => {
      if (!type || !id) return ""
      if (type === "agent") return agents.get(id)?.name ?? shortId(id)
      // Historical assignee_type=squad may still appear on old issues.
      if (type === "squad") return shortId(id)
      return members.get(id)?.name ?? shortId(id)
    },
    statusEntry: (key) => statuses.get(key),
    // The list endpoint returns status_name as "" — treat empty as missing.
    statusLabel: (issue) => issue.status_name || statuses.get(issue.status)?.name || issue.status,
    projectTitle: (id) => (id ? (projects.get(id)?.title ?? shortId(id)) : ""),
    runtimeName: (id) => {
      if (!id) return "unbound"
      const runtime = runtimes.get(id)
      return runtime ? (runtime.custom_name ?? runtime.name) : shortId(id)
    },
    runtime: (id) => runtimes.get(id),
  }
}

export type MachineSummary = {
  name: string
  totalRuntimes: number
  onlineRuntimes: number
  isOnline: boolean
}

export function machineSummaries(runtimes: readonly Runtime[]): MachineSummary[] {
  const map = new Map<string, Runtime[]>()
  for (const r of runtimes) {
    const name = r.custom_name ?? r.name
    const list = map.get(name) ?? []
    list.push(r)
    map.set(name, list)
  }
  return [...map.entries()].map(([name, list]) => {
    const online = list.filter((r) => r.status === "online").length
    return {
      name,
      totalRuntimes: list.length,
      onlineRuntimes: online,
      isOnline: online > 0,
    }
  })
}

export function statusTone(store: Store, issue: Issue) {
  const category = issue.status_category ?? store.statuses.find((status) => status.key === issue.status)?.category
  return category ? CATEGORY_TONE[category] : "default"
}

export function formatPriority(priority: IssuePriority): string {
  if (priority === "urgent") return "▲ urgent"
  if (priority === "high") return "↑ high"
  if (priority === "medium") return "medium"
  if (priority === "low") return "↓ low"
  return "·"
}

export function formatAgentStatus(status: string): string {
  if (status === "working") return "● working"
  if (status === "idle") return "○ idle"
  if (status === "blocked") return "▲ blocked"
  if (status === "error") return "✖ error"
  if (status === "offline") return "○ offline"
  return status
}

export function formatRuntimeStatus(status: string): string {
  return status === "online" ? "● online" : "○ offline"
}

export const TAB_HEADERS: Record<string, HeaderCell[]> = {
  orch: [
    { text: "Issue" },
    { text: "Bucket", width: 12 },
    { text: "Phase", width: 16 },
    { text: "Assignee", width: 16 },
    { text: "Updated", width: 10 },
  ],
  issues: [
    { text: "ID", width: 10 },
    { text: "TITLE" },
    { text: "STATUS", width: 14 },
    { text: "PRIORITY", width: 9 },
    { text: "ASSIGNEE", width: 16 },
    { text: "PROJECT", width: 16 },
    { text: "UPDATED", width: 10 },
  ],
  agents: [
    { text: "NAME", width: 22 },
    { text: "STATUS", width: 11 },
    { text: "OWNER", width: 12 },
    { text: "ACCESS", width: 10 },
    { text: "RUNTIME", width: 22 },
    { text: "MODEL", width: 22 },
    { text: "SKILLS", width: 9 },
    { text: "UPDATED", width: 10 },
  ],
  runtimes: [
    { text: "RUNTIME", width: 24 },
    { text: "STATUS", width: 11 },
    { text: "PROVIDER", width: 12 },
    { text: "MODE", width: 6 },
    { text: "AGENTS", width: 10 },
    { text: "VISIBILITY", width: 10 },
    { text: "LAST SEEN", width: 10 },
  ],
  projects: [
    { text: "PROJECT" },
    { text: "STATUS", width: 12 },
    { text: "PRIORITY", width: 9 },
    { text: "PROGRESS", width: 14 },
    { text: "LEAD", width: 16 },
    { text: "DUE DATE", width: 12 },
    { text: "UPDATED", width: 10 },
  ],
  skills: [
    { text: "SKILL", width: 28 },
    { text: "AGENTS", width: 10 },
    { text: "DESCRIPTION" },
    { text: "UPDATED", width: 10 },
  ],
  inbox: [
    { text: "", width: 2 },
    { text: "TITLE" },
    { text: "TYPE", width: 18 },
    { text: "SEVERITY", width: 16 },
    { text: "WHEN", width: 10 },
  ],
  chat: [
    { text: "", width: 2 },
    { text: "CHAT" },
    { text: "AGENT", width: 18 },
    { text: "PREVIEW" },
    { text: "UPDATED", width: 10 },
  ],
  autopilots: [
    { text: "AUTOPILOT" },
    { text: "STATUS", width: 10 },
    { text: "ASSIGNEE", width: 18 },
    { text: "MODE", width: 14 },
    { text: "TRIGGERS", width: 16 },
    { text: "LAST RUN", width: 10 },
  ],
}

export type IssueFilterOptions = {
  statusCategory?: string
  projectID?: string
}

export function issueRows(
  store: Store,
  lookups: Lookups,
  now: number,
  options: IssueFilterOptions & { issues?: readonly Issue[] } = {},
): Row[] {
  const source = options.issues ?? store.issues
  const filtered = options.issues
    ? source
    : source.filter((issue) => {
        if (options.projectID && options.projectID !== "all" && issue.project_id !== options.projectID) return false
        if (options.statusCategory && options.statusCategory !== "all") {
          const category = issue.status_category ?? store.statuses.find((s) => s.key === issue.status)?.category
          if (category !== options.statusCategory) return false
        }
        return true
      })

  return filtered.map((issue) => ({
    id: issue.id,
    search: `${issue.identifier} ${issue.title} ${issue.status} ${lookups.actorName(issue.assignee_type, issue.assignee_id)} ${lookups.projectTitle(issue.project_id)}`.toLowerCase(),
    cells: [
      { text: issue.identifier, width: 10, tone: "subdued" },
      { text: issue.title },
      { text: lookups.statusLabel(issue), width: 14, tone: statusTone(store, issue) },
      { text: formatPriority(issue.priority), width: 9, tone: PRIORITY_TONE[issue.priority] },
      { text: lookups.actorName(issue.assignee_type, issue.assignee_id) || "—", width: 16 },
      { text: lookups.projectTitle(issue.project_id) || "—", width: 16, tone: "subdued" },
      { text: formatRelative(issue.last_activity_at ?? issue.updated_at, now), width: 10, tone: "subdued" },
    ],
  }))
}

export type AgentFilterOptions = {
  machine?: string
  scope?: "all" | "active" | "mine" | "archived" | "working"
}

export function agentRows(
  store: Store,
  lookups: Lookups,
  now: number,
  options: AgentFilterOptions = {},
): Row[] {
  const working = new Map(store.working.map((agent) => [agent.id, agent.running_task_count]))
  const runtimes = new Map(store.runtimes.map((r) => [r.id, r]))

  const filtered = store.agents.filter((agent) => {
    if (options.scope === "active" && agent.archived_at) return false
    if (options.scope === "mine" && (agent.owner_id !== store.me?.id || agent.archived_at)) return false
    if (options.scope === "archived" && !agent.archived_at) return false
    if (options.scope === "working" && !(working.get(agent.id) ?? 0)) return false

    if (options.machine && options.machine !== "all") {
      const runtime = agent.runtime_id ? runtimes.get(agent.runtime_id) : undefined
      const machineName = runtime ? (runtime.custom_name ?? runtime.name) : undefined
      if (machineName !== options.machine) return false
    }
    return true
  })

  return filtered.map((agent) => {
    const runtime = agent.runtime_id ? runtimes.get(agent.runtime_id) : undefined
    const runtimeDisplay = runtime
      ? `${runtime.status === "online" ? "●" : "○"} ${runtime.custom_name ?? runtime.name} (${runtime.provider})`
      : "— unbound"
    const ownerName = agent.owner_id === store.me?.id
      ? `${store.me?.name ?? "you"} (you)`
      : lookups.actorName("user", agent.owner_id) || "—"

    return {
      id: agent.id,
      search: `${agent.name} ${agent.description} ${agent.model} ${agent.status} ${runtimeDisplay} ${ownerName}`.toLowerCase(),
      cells: [
        { text: agent.name, width: 22 },
        { text: formatAgentStatus(agent.status), width: 11, tone: AGENT_STATUS_TONE[agent.status] ?? "default" },
        { text: ownerName, width: 12 },
        { text: agent.visibility, width: 10, tone: "subdued" },
        {
          text: runtimeDisplay,
          width: 22,
          tone: runtime ? (runtime.status === "online" ? "success" : "subdued") : "warning",
        },
        { text: agent.model || "·", width: 22, tone: "subdued" },
        { text: `${agent.skills?.length ?? 0} skills`, width: 9, tone: "subdued" },
        { text: agent.archived_at ? "archived" : formatRelative(agent.updated_at, now), width: 10, tone: agent.archived_at ? "warning" : "subdued" },
      ],
    }
  })
}

export function runtimeRows(store: Store, now: number): Row[] {
  const agentsByRuntime = new Map<string, number>()
  for (const agent of store.agents) {
    if (!agent.runtime_id || agent.archived_at) continue
    agentsByRuntime.set(agent.runtime_id, (agentsByRuntime.get(agent.runtime_id) ?? 0) + 1)
  }
  return store.runtimes.map((runtime) => ({
    id: runtime.id,
    search: `${runtime.name} ${runtime.custom_name ?? ""} ${runtime.provider} ${runtime.status}`.toLowerCase(),
    cells: [
      { text: runtime.custom_name ?? runtime.name, width: 24 },
      { text: formatRuntimeStatus(runtime.status), width: 11, tone: runtime.status === "online" ? "success" : "subdued" },
      { text: runtime.provider, width: 12 },
      { text: runtime.runtime_mode, width: 6, tone: "subdued" },
      { text: `${agentsByRuntime.get(runtime.id) ?? 0} agents`, width: 10, tone: "subdued" },
      { text: runtime.visibility ?? "private", width: 10, tone: "subdued" },
      { text: formatRelative(runtime.last_seen_at, now), width: 10, tone: "subdued" },
    ],
  }))
}

export type ProjectFilterOptions = {
  status?: string
  priority?: string
}

export function projectRows(
  store: Store,
  lookups: Lookups,
  now: number,
  options: ProjectFilterOptions = {},
): Row[] {
  const filtered = store.projects.filter((project) => {
    if (options.status && options.status !== "all" && project.status !== options.status) return false
    if (options.priority && options.priority !== "all" && project.priority !== options.priority) return false
    return true
  })

  return filtered.map((project) => {
    const percent = project.issue_count > 0 ? Math.round((project.done_count / project.issue_count) * 100) : 0
    return {
      id: project.id,
      search: `${project.title} ${project.description ?? ""} ${project.status}`.toLowerCase(),
      cells: [
        { text: `${project.icon ?? "📁"} ${project.title}`.trim() },
        { text: project.status, width: 12, tone: PROJECT_STATUS_TONE[project.status] ?? "default" },
        { text: formatPriority(project.priority), width: 9, tone: PRIORITY_TONE[project.priority] },
        { text: `${project.done_count}/${project.issue_count} (${percent}%)`, width: 14, tone: percent === 100 ? "success" : "subdued" },
        { text: project.lead_type ? lookups.actorName(project.lead_type, project.lead_id) : "—", width: 16 },
        { text: project.due_date ?? "—", width: 12, tone: "subdued" },
        { text: formatRelative(project.updated_at, now), width: 10, tone: "subdued" },
      ],
    }
  })
}

export function skillRows(store: Store, now: number): Row[] {
  const usedBy = new Map<string, number>()
  for (const agent of store.agents) {
    for (const skill of agent.skills ?? []) {
      usedBy.set(skill.id, (usedBy.get(skill.id) ?? 0) + 1)
    }
  }
  return store.skills.map((skill) => ({
    id: skill.id,
    search: `${skill.name} ${skill.description}`.toLowerCase(),
    cells: [
      { text: skill.name, width: 28 },
      { text: `${usedBy.get(skill.id) ?? 0} agents`, width: 10, tone: "subdued" },
      { text: skill.description || "—", tone: "subdued" },
      { text: formatRelative(skill.updated_at, now), width: 10, tone: "subdued" },
    ],
  }))
}

export function inboxRows(items: readonly InboxItem[], now: number): Row[] {
  return items.map((item) => ({
    id: item.id,
    search: `${item.title} ${item.body ?? ""} ${item.type}`.toLowerCase(),
    cells: [
      { text: item.read ? " " : "●", width: 2, tone: item.read ? "subdued" : "accent" },
      { text: item.title },
      { text: item.type.replaceAll("_", " "), width: 18, tone: "subdued" },
      {
        text: item.severity.replaceAll("_", " "),
        width: 16,
        tone: item.severity === "action_required" ? "error" : item.severity === "attention" ? "warning" : "info",
      },
      { text: formatRelative(item.created_at, now), width: 10, tone: "subdued" },
    ],
  }))
}

export function chatRows(store: Store, lookups: Lookups, now: number): Row[] {
  return [...store.chats]
    .sort((a, b) => {
      if (a.pinned && !b.pinned) return -1
      if (!a.pinned && b.pinned) return 1
      return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
    })
    .map((session) => {
      const preview = session.last_message?.content?.replace(/\s+/g, " ").trim() ?? ""
      const title = session.title
        .replace(/\[([^\]]+)\]\(mention:\/\/[^)]+\)/g, "$1")
        .replace(/\s+/g, " ")
        .trim()
      return {
        id: session.id,
        search: `${title} ${lookups.actorName("agent", session.agent_id)} ${preview}`.toLowerCase(),
        cells: [
          {
            text: session.has_unread || (session.unread_count ?? 0) > 0 ? "●" : session.pinned ? "★" : " ",
            width: 2,
            tone: session.has_unread ? "accent" : "subdued",
          },
          { text: title || "Untitled chat" },
          { text: lookups.actorName("agent", session.agent_id) || "—", width: 18 },
          { text: preview || "—", tone: "subdued" },
          { text: formatRelative(session.updated_at, now), width: 10, tone: "subdued" },
        ],
      }
    })
}

export function autopilotRows(store: Store, lookups: Lookups, now: number): Row[] {
  return store.autopilots.map((autopilot) => ({
    id: autopilot.id,
    search: `${autopilot.title} ${autopilot.status} ${lookups.actorName(autopilot.assignee_type ?? "agent", autopilot.assignee_id)}`.toLowerCase(),
    cells: [
      { text: autopilot.title },
      {
        text: autopilot.status,
        width: 10,
        tone: autopilot.status === "active" ? "success" : autopilot.status === "paused" ? "warning" : "subdued",
      },
      { text: lookups.actorName(autopilot.assignee_type ?? "agent", autopilot.assignee_id) || "—", width: 18 },
      { text: autopilot.execution_mode.replaceAll("_", " "), width: 14, tone: "subdued" },
      { text: (autopilot.trigger_kinds ?? []).join(",") || "—", width: 16, tone: "subdued" },
      { text: autopilot.last_run_at ? formatRelative(autopilot.last_run_at, now) : "—", width: 10, tone: "subdued" },
    ],
  }))
}


export function orchRows(store: Store, now: number): Row[] {
  return store.orchIssues.map((issue) => ({
    id: issue.id,
    search: `${issue.key} ${issue.title} ${issue.phase ?? ""} ${issue.attention.bucket} ${issue.assignee?.name ?? ""}`.toLowerCase(),
    cells: [
      { text: `${issue.key} ${issue.title}${issue.orchestrated ? "" : ""}` },
      {
        text: bucketLabel(issue.attention.bucket),
        width: 12,
        tone:
          issue.attention.bucket === "needs_you"
            ? "warning"
            : issue.attention.bucket === "running"
              ? "accent"
              : issue.attention.bucket === "settled"
                ? "success"
                : "subdued",
      },
      { text: issue.phase || issue.status, width: 16, tone: issue.paused ? "warning" : "default" },
      { text: issue.assignee?.name || "—", width: 16 },
      { text: formatRelative(issue.updated_at, now), width: 10, tone: "subdued" },
    ],
  }))
}

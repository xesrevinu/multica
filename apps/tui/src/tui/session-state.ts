import { readFileSync } from "node:fs"
import { mkdir, rename, rm, writeFile } from "node:fs/promises"
import path from "node:path"
import { ISSUE_MINE_SCOPES, type IssueMineScope } from "./issue-query"
import { INBOX_VIEWS, type InboxView } from "./inbox"
import {
  DEFAULT_SPACE_VIEW,
  ISSUE_ATTENTION,
  SPACE_VIEWS,
  SURFACES,
  migrateLegacySurface,
  type IssueAttention,
  type SpaceView,
  type Surface,
} from "./nav"

export const DETAIL_KINDS = ["issue", "agent", "skill", "chat", "autopilot"] as const
export type DetailKind = (typeof DETAIL_KINDS)[number]
export type WorkspaceDetail = { kind: DetailKind; id: string }

const CHAT_STATUSES = ["active", "archived"] as const
const AGENT_SCOPES = ["active", "all", "mine", "working", "archived"] as const
const AGENT_VIEWS = ["agents", "runtimes"] as const
const PROJECT_STATUSES = ["all", "planned", "in_progress", "paused", "completed", "cancelled"] as const
const PROJECT_PRIORITIES = ["all", "urgent", "high", "medium", "low", "none"] as const

export type WorkspaceView = {
  surface: Surface
  space_view: SpaceView
  project_id: string
  issue_status: string
  issue_mine: IssueMineScope
  issue_attention: IssueAttention
  inbox_view: InboxView
  chat_status: (typeof CHAT_STATUSES)[number]
  agent_machine: string
  agent_scope: (typeof AGENT_SCOPES)[number]
  agents_view: (typeof AGENT_VIEWS)[number]
  project_status: (typeof PROJECT_STATUSES)[number]
  project_priority: (typeof PROJECT_PRIORITIES)[number]
  selected: Partial<Record<Surface | SpaceView, string>>
  detail?: WorkspaceDetail
}

export type SessionState = {
  version: 1
  workspaces: Record<string, WorkspaceView>
}

// Sits next to the multica CLI config (including named profiles) so view state
// is not written into the shared config.json the CLI owns.
export function sessionStatePath(configFile: string) {
  return path.join(path.dirname(configFile), "tui-state.json")
}

export function emptySession(): SessionState {
  return { version: 1, workspaces: {} }
}

export function defaultWorkspaceView(): WorkspaceView {
  return {
    surface: "overview",
    space_view: DEFAULT_SPACE_VIEW,
    project_id: "all",
    issue_status: "all",
    issue_mine: "all",
    issue_attention: "all",
    inbox_view: "all",
    chat_status: "active",
    agent_machine: "all",
    agent_scope: "active",
    agents_view: "agents",
    project_status: "all",
    project_priority: "all",
    selected: {},
  }
}

export function decodeSession(text: string): SessionState {
  if (!text.trim()) return emptySession()
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return emptySession()
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return emptySession()
  const workspaces = (parsed as { workspaces?: unknown }).workspaces
  if (!workspaces || typeof workspaces !== "object" || Array.isArray(workspaces)) return emptySession()
  const next: Record<string, WorkspaceView> = {}
  for (const [id, view] of Object.entries(workspaces)) {
    if (!id || !view || typeof view !== "object" || Array.isArray(view)) continue
    next[id] = normalizeView(view)
  }
  return { version: 1, workspaces: next }
}

export function readSessionStateSync(file: string): SessionState {
  try {
    return decodeSession(readFileSync(file, "utf8"))
  } catch {
    return emptySession()
  }
}

export async function writeSessionState(file: string, state: SessionState) {
  await mkdir(path.dirname(file), { recursive: true })
  const temporary = `${file}.${process.pid}.${crypto.randomUUID()}.tmp`
  try {
    await writeFile(temporary, `${JSON.stringify(state, null, 2)}\n`)
    await rename(temporary, file)
  } catch (error) {
    await rm(temporary, { force: true }).catch(() => undefined)
    throw error
  }
}

export function withWorkspace(state: SessionState, workspaceID: string, view: WorkspaceView): SessionState {
  return {
    version: 1,
    workspaces: {
      ...state.workspaces,
      [workspaceID]: normalizeView(view),
    },
  }
}

// Drop project, status, and machine filters that disappeared with the workspace data.
export function settleWorkspaceView(
  view: WorkspaceView,
  known: { projectIDs: readonly string[]; statusKeys: readonly string[]; machines: readonly string[] },
): WorkspaceView {
  return {
    ...view,
    project_id: view.project_id === "all" || known.projectIDs.includes(view.project_id) ? view.project_id : "all",
    issue_status: view.issue_status === "all" || known.statusKeys.includes(view.issue_status) ? view.issue_status : "all",
    agent_machine: view.agent_machine === "all" || known.machines.includes(view.agent_machine) ? view.agent_machine : "all",
  }
}

export function normalizeView(input: unknown): WorkspaceView {
  const base = defaultWorkspaceView()
  if (!input || typeof input !== "object" || Array.isArray(input)) return base
  const raw = input as Record<string, unknown>

  const migrated = migrateLegacySurface(typeof raw.surface === "string" ? raw.surface : base.surface)
  const surface = migrated.surface
  const space_view = oneOf(SPACE_VIEWS, raw.space_view)
    ? raw.space_view
    : migrated.spaceView ?? base.space_view
  const issue_attention = oneOf(ISSUE_ATTENTION, raw.issue_attention)
    ? raw.issue_attention
    : migrated.attention ?? base.issue_attention

  return {
    surface,
    space_view,
    project_id: text(raw.project_id) ?? base.project_id,
    issue_status: text(raw.issue_status) ?? base.issue_status,
    issue_mine: oneOf(ISSUE_MINE_SCOPES, raw.issue_mine) ? raw.issue_mine : base.issue_mine,
    issue_attention,
    inbox_view: oneOf(INBOX_VIEWS, raw.inbox_view) ? raw.inbox_view : base.inbox_view,
    chat_status: oneOf(CHAT_STATUSES, raw.chat_status) ? raw.chat_status : base.chat_status,
    agent_machine: text(raw.agent_machine) ?? base.agent_machine,
    agent_scope: oneOf(AGENT_SCOPES, raw.agent_scope) ? raw.agent_scope : base.agent_scope,
    agents_view: oneOf(AGENT_VIEWS, raw.agents_view) ? raw.agents_view : base.agents_view,
    project_status: oneOf(PROJECT_STATUSES, raw.project_status) ? raw.project_status : base.project_status,
    project_priority: oneOf(PROJECT_PRIORITIES, raw.project_priority) ? raw.project_priority : base.project_priority,
    selected: normalizeSelected(raw.selected),
    detail: normalizeDetail(raw.detail),
  }
}

function normalizeSelected(value: unknown): Partial<Record<Surface | SpaceView, string>> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {}
  const selected: Partial<Record<Surface | SpaceView, string>> = {}
  const keys = [...SURFACES, ...SPACE_VIEWS] as readonly string[]
  for (const [key, entry] of Object.entries(value)) {
    // Remap legacy selection keys onto space subviews / issues.
    let dest = key
    if (key === "orch") dest = "issues"
    if (key === "chat") dest = "chats"
    if (!keys.includes(dest)) continue
    const id = text(entry)
    if (id) selected[dest as Surface | SpaceView] = id
  }
  return selected
}

function normalizeDetail(value: unknown): WorkspaceDetail | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined
  const raw = value as Record<string, unknown>
  const id = text(raw.id)
  // Drop legacy project detail — projects are list-only now.
  if (raw.kind === "project") return undefined
  if (!oneOf(DETAIL_KINDS, raw.kind) || !id) return undefined
  return { kind: raw.kind, id }
}

function text(value: unknown) {
  return typeof value === "string" && value.trim() ? value : undefined
}

function oneOf<T extends string>(values: readonly T[], value: unknown): value is T {
  return typeof value === "string" && (values as readonly string[]).includes(value)
}

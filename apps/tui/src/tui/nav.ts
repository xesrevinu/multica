/** Flat top-level tabs — no you/work/ops nesting. */
export const SURFACES = ["overview", "inbox", "issues", "space"] as const

export type Surface = (typeof SURFACES)[number]

/**
 * Workspace / configure modes live under the single `space` tab.
 * Projects is a progress list only (no project detail drill-in).
 * Agent DMs sit here as `chats` — issue conversation is issue detail.
 */
export const SPACE_VIEWS = [
  "projects",
  "autopilots",
  "agents",
  "skills",
  "chats",
  "usage",
  "settings",
] as const

export type SpaceView = (typeof SPACE_VIEWS)[number]

/** Attention buckets from orchestrator, as issue-list filters. */
export const ISSUE_ATTENTION = ["all", "needs_you", "running", "queued", "settled"] as const
export type IssueAttention = (typeof ISSUE_ATTENTION)[number]

export const DEFAULT_SPACE_VIEW: SpaceView = "projects"

/** Map legacy surfaces (pre-flatten) onto the new IA. */
export function migrateLegacySurface(raw: string): {
  surface: Surface
  spaceView?: SpaceView
  attention?: IssueAttention
} {
  if ((SURFACES as readonly string[]).includes(raw)) return { surface: raw as Surface }
  if (raw === "orch") return { surface: "issues", attention: "needs_you" }
  if (raw === "chat") return { surface: "space", spaceView: "chats" }
  if (raw === "projects") return { surface: "space", spaceView: "projects" }
  if (raw === "autopilots") return { surface: "space", spaceView: "autopilots" }
  if (raw === "agents") return { surface: "space", spaceView: "agents" }
  if (raw === "skills") return { surface: "space", spaceView: "skills" }
  if (raw === "usage") return { surface: "space", spaceView: "usage" }
  if (raw === "settings") return { surface: "space", spaceView: "settings" }
  return { surface: "overview" }
}

export function neighborSurface(surface: Surface, delta: number): Surface {
  const index = SURFACES.indexOf(surface)
  return SURFACES[(index + delta + SURFACES.length) % SURFACES.length] ?? surface
}

export function neighborSpaceView(view: SpaceView, delta: number): SpaceView {
  const index = SPACE_VIEWS.indexOf(view)
  return SPACE_VIEWS[(index + delta + SPACE_VIEWS.length) % SPACE_VIEWS.length] ?? view
}

export function surfaceFromDigit(digit: string): Surface | undefined {
  const index = Number(digit) - 1
  if (!Number.isInteger(index) || index < 0) return undefined
  return SURFACES[index]
}

export function spaceViewFromDigit(digit: string): SpaceView | undefined {
  const index = Number(digit) - 1
  if (!Number.isInteger(index) || index < 0) return undefined
  return SPACE_VIEWS[index]
}

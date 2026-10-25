import { useQuery } from "@tanstack/react-query"
import { TextAttributes } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import {
  availablePROps,
  flowCardMode,
  orchPRCardOptions,
  orchTimelineOptions,
  shouldOfferResolveConflict,
  useOrchFlowAction,
  useOrchPROp,
  type OrchFlowAction,
  type OrchPRCard,
  type OrchPROp,
} from "@multica/core/orchestrator"
import { useTheme } from "./context/theme"
import { openTextPrompt } from "./compose-dialog"
import { openPicker } from "./picker"
import { useDialog } from "./ui/dialog"

function prOpen(card: OrchPRCard): boolean {
  const pr = card.pr
  const sum = card.summary
  if (pr?.merged_at) return false
  const state = (pr?.state ?? sum?.state ?? "").toLowerCase()
  return state === "open" || Boolean(pr?.draft)
}

function prConflict(card: OrchPRCard): boolean {
  const pr = card.pr
  const sum = card.summary
  const ms = (pr?.mergeable_state ?? sum?.merge_state ?? "").toLowerCase()
  return ms === "dirty" || pr?.mergeable === false || (sum?.mergeable ?? "").toUpperCase() === "CONFLICTING"
}

function failedChecks(card: OrchPRCard): number {
  const checks = card.pr?.checks ?? []
  return checks.filter((c) => {
    const conclusion = (c.conclusion ?? "").toLowerCase()
    return conclusion === "failure" || conclusion === "timed_out" || conclusion === "cancelled"
  }).length
}

/**
 * Compact orchestrator decision card — same queryOptions / mutations as Web.
 */
export function OrchFlowPanel(props: {
  workspaceId: string
  issueKey: string
  focused?: boolean
  onChanged?: () => void
}) {
  const theme = useTheme()
  const dialog = useDialog()
  const timelineQ = useQuery({
    ...orchTimelineOptions(props.workspaceId, props.issueKey),
    refetchInterval: 5_000,
  })
  const prQ = useQuery({
    ...orchPRCardOptions(props.workspaceId, props.issueKey),
    refetchInterval: 5_000,
  })
  const flow = useOrchFlowAction(props.workspaceId, props.issueKey)
  const prOp = useOrchPROp(props.workspaceId, props.issueKey)

  const timeline = timelineQ.data ?? undefined
  const prCard = prQ.data ?? undefined
  const error =
    timelineQ.error instanceof Error
      ? timelineQ.error.message
      : prQ.error instanceof Error
        ? prQ.error.message
        : flow.error instanceof Error
          ? flow.error.message
          : prOp.error instanceof Error
            ? prOp.error.message
            : undefined
  const busy = flow.isPending || prOp.isPending

  const mode = flowCardMode(timeline)
  const open = prCard ? prOpen(prCard) : false
  const conflict = prCard ? prConflict(prCard) : false
  const prOps = prCard
    ? availablePROps(prCard.can, {
        live: prCard.live,
        open,
        orchestrated: prCard.orchestrated,
        conflict,
        failedChecks: failedChecks(prCard),
        behind: prCard.pr?.behind_by ?? 0,
        headDeleted: prCard.pr?.head_deleted,
        head: prCard.pr?.head_ref ?? prCard.summary?.branch ?? "",
        base: prCard.pr?.base_ref || "main",
      })
    : []
  const offerResolve = prCard
    ? shouldOfferResolveConflict({
        live: prCard.live,
        open,
        orchestrated: prCard.orchestrated,
        conflict,
      })
    : false

  const runFlow = async (action: OrchFlowAction, reason?: string) => {
    await flow.mutateAsync({ action, reason })
    props.onChanged?.()
  }

  const runPR = async (op: OrchPROp) => {
    const number = prCard?.pr?.number ?? prCard?.summary?.number
    const headSha = prCard?.pr?.head_sha || undefined
    await prOp.mutateAsync({ op, number, headSha })
    props.onChanged?.()
  }

  const openActions = () => {
    if (!timeline?.issue.orchestrated || busy) return
    const options = [] as Array<{ id: string; title: string; detail?: string; search: string }>
    if (mode === "acceptance") {
      options.push(
        { id: "accept", title: "Accept", detail: "Merge / complete acceptance", search: "accept" },
        { id: "reject", title: "Reject…", detail: "Requires a reason", search: "reject" },
      )
    } else if (mode === "blocked") {
      options.push(
        { id: "unblock", title: "Unblock…", detail: "Optional note", search: "unblock" },
        { id: "cancel", title: "Cancel issue", search: "cancel" },
      )
    } else if (mode === "cancel_proposed") {
      options.push({ id: "cancel", title: "Confirm cancel", search: "cancel confirm" })
    } else {
      options.push(
        { id: "replay", title: "Replay", detail: "Clear backoff and re-evaluate", search: "replay" },
        { id: "pause", title: "Pause flow", search: "pause" },
        { id: "resume", title: "Resume flow", search: "resume" },
      )
    }
    if (offerResolve) {
      options.push({
        id: "resolve-conflict",
        title: "Resolve conflicts",
        detail: "Ask agent to merge base",
        search: "resolve conflict",
      })
    }
    for (const op of prOps) {
      options.push({ id: `pr:${op}`, title: op, detail: "Pull request", search: op })
    }
    openPicker(dialog, {
      title: `Orchestrator · ${props.issueKey}`,
      options: options.map((o) => ({ ...o, category: "orch" })),
      onSelect: (id) => {
        if (id.startsWith("pr:")) {
          void runPR(id.slice(3) as OrchPROp)
          return
        }
        const action = id as OrchFlowAction
        if (action === "reject") {
          openTextPrompt(dialog, {
            title: "Reject acceptance",
            placeholder: "Reason…",
            onSubmit: async (value) => {
              await runFlow("reject", value)
            },
          })
          return
        }
        if (action === "unblock") {
          openTextPrompt(dialog, {
            title: "Unblock",
            placeholder: "Note (optional)…",
            onSubmit: async (value) => {
              await runFlow("unblock", value.trim() || undefined)
            },
          })
          return
        }
        void runFlow(action)
      },
    })
  }

  useKeyboard((evt) => {
    if (!props.focused || busy || !timeline?.issue.orchestrated) return
    if ((evt.name ?? "").toLowerCase() !== "o") return
    evt.preventDefault?.()
    openActions()
  })

  if (error && !timeline) {
    return (
      <box flexDirection="column" gap={0}>
        <text fg={theme.text.muted}>Orchestrator</text>
        <text fg={theme.text.feedback.error.base}>{error}</text>
      </box>
    )
  }
  if (!timeline?.issue.orchestrated) return null

  const issue = timeline.issue
  const reason = timeline.flow.blocked_reason || timeline.waiting
  const pr = prCard?.pr ?? prCard?.summary
  const wt = issue.worktree

  return (
    <box flexDirection="column" gap={0} paddingBottom={1}>
      <text fg={theme.text.action.primary.base} attributes={TextAttributes.BOLD}>
        Orchestrator · {issue.phase || issue.status}
        {issue.paused ? " · paused" : ""}
      </text>
      <text fg={theme.text.muted}>
        {issue.attention.bucket}
        {issue.attention.reason ? `/${issue.attention.reason}` : ""}
        {issue.attempts ? ` · i${issue.attempts.impl}/r${issue.attempts.review}/x${issue.attempts.infra}` : ""}
        {issue.running ? ` · ${issue.running.agent}` : ""}
      </text>
      {pr ? (
        <text fg={theme.text.base} wrapMode="word">
          {[
            `PR #${pr.number}`,
            pr.state || undefined,
            prCard?.pr?.checks?.length
              ? failedChecks(prCard) > 0
                ? `${failedChecks(prCard)} failed`
                : `${prCard.pr.checks.length} checks`
              : undefined,
            conflict ? "conflict" : undefined,
            prCard && !prCard.live ? "snapshot" : undefined,
          ]
            .filter(Boolean)
            .join(" · ")}
        </text>
      ) : null}
      {wt?.path || wt?.branch ? (
        <text fg={theme.text.muted} wrapMode="word">
          worktree {wt.branch ? `${wt.branch} ` : ""}
          {wt.path ?? ""}
        </text>
      ) : null}
      {reason ? <text fg={theme.text.feedback.warning.base}>{reason}</text> : null}
      {timeline.last_error ? <text fg={theme.text.feedback.error.base}>{timeline.last_error}</text> : null}
      {error ? <text fg={theme.text.feedback.error.base}>{error}</text> : null}
      <text fg={theme.text.muted}>
        {busy
          ? "…"
          : mode === "acceptance"
            ? "o actions (accept/reject)"
            : mode === "blocked"
              ? "o actions (unblock/cancel)"
              : mode === "cancel_proposed"
                ? "o actions (confirm cancel)"
                : prOps.length || offerResolve
                  ? "o actions (flow/PR)"
                  : "o actions (replay/pause)"}
      </text>
    </box>
  )
}

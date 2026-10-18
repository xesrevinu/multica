"use client";

import { useMemo, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { ChevronRight, Clock, Loader2 } from "lucide-react";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
} from "@multica/ui/components/ui/sidebar";
import { cn } from "@multica/ui/lib/utils";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { Agent, AgentTask, Issue } from "@multica/core/types";
import { AppLink } from "../navigation";
import { useT } from "../i18n";

// The sidebar's live "who is doing what right now" section. It reads the same
// workspace task snapshot the presence dots already use (active tasks only —
// the terminal rows the endpoint also carries belong to the hover cards), so
// the section costs no extra request and inherits the WS task invalidation
// that keeps presence honest.
//
// Rows stay short on purpose: agent, task title, one line of context. The
// section disappears entirely when nothing is in flight — an empty "Working
// now" header would just be chrome on every quiet workspace.

/** Statuses the server keeps in-flight; everything else is terminal. */
const ACTIVE_STATUSES = new Set([
  "running",
  "dispatched",
  "waiting_local_directory",
  "queued",
]);

// Running first — that is the answer to "who is working right now". Queued
// runs are shown because a stalled queue is exactly what a glance should
// catch, but they never outrank something that is actually executing.
const STATUS_RANK: Record<string, number> = {
  running: 0,
  dispatched: 1,
  waiting_local_directory: 2,
  queued: 3,
};

const PREVIEW_LIMIT = 3;
const SUMMARY_MAX = 140;

const EMPTY_TASKS: AgentTask[] = [];
const EMPTY_AGENTS: Agent[] = [];

export function isActiveTask(task: AgentTask): boolean {
  return ACTIVE_STATUSES.has(task.status);
}

function lastTouch(task: AgentTask): string {
  return task.started_at ?? task.dispatched_at ?? task.created_at;
}

/**
 * In-flight tasks, newest run first within a status. Exported for the unit
 * tests: ordering is the part users read as the section's meaning ("who is
 * working, not who just finished"), so it is worth pinning without a render.
 */
export function orderActiveTasks(tasks: readonly AgentTask[]): AgentTask[] {
  return tasks
    .filter(isActiveTask)
    .sort((a, b) => {
      const rank = (STATUS_RANK[a.status] ?? 99) - (STATUS_RANK[b.status] ?? 99);
      if (rank !== 0) return rank;
      return lastTouch(b).localeCompare(lastTouch(a));
    });
}

/** Distinct issue ids in row order — one detail query each, for the visible rows only. */
export function distinctIssueIds(tasks: readonly AgentTask[]): string[] {
  const seen = new Set<string>();
  const ids: string[] = [];
  for (const task of tasks) {
    if (!task.issue_id || seen.has(task.issue_id)) continue;
    seen.add(task.issue_id);
    ids.push(task.issue_id);
  }
  return ids;
}

/**
 * First readable line of a markdown-ish string, clamped for a 240px column.
 * Task summaries are snapshots of a comment or an autopilot title, and issue
 * descriptions are full markdown — both need the same flattening before they
 * can sit on one line. Headings are skipped because a description that opens
 * with `## Why` says nothing on its own; the first real sentence underneath
 * does. A heading-only text still falls back to its own words.
 */
export function plainFirstLine(text: string | null | undefined, max = SUMMARY_MAX): string {
  if (!text) return "";
  const strip = (line: string) => line.replace(/^\s*(?:[#>*-]+|\d+\.)\s*/, "").trim();
  const lines = text
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
  const line =
    lines
      .filter((l) => !l.startsWith("#"))
      .map(strip)
      .find((l) => l.length > 0) ??
    lines.map(strip).find((l) => l.length > 0) ??
    "";
  if (!line) return "";
  const collapsed = line.replace(/\s+/g, " ");
  return collapsed.length > max ? `${collapsed.slice(0, max - 1).trimEnd()}…` : collapsed;
}

export type ActiveWorkKind = "issue" | "chat" | "autopilot" | "task";

export interface ActiveWorkContent {
  kind: ActiveWorkKind;
  /** Task name. Empty when neither the issue nor the trigger summary is known yet. */
  title: string;
  /** One line of context under the title; empty when there is nothing to add. */
  summary: string;
}

/**
 * What a row says about one run. Issue-backed work is named by the issue and
 * explained by the comment that started the run (falling back to the issue
 * description); a chat or autopilot run has no issue to borrow from, so its
 * trigger summary *is* the name, and the kind only shows up as the fallback
 * label when even that is missing.
 */
export function deriveActiveWorkContent(
  task: AgentTask,
  issue: Issue | null | undefined,
): ActiveWorkContent {
  const trigger = plainFirstLine(task.trigger_summary);
  if (task.issue_id) {
    const title = issue?.title?.trim() || trigger;
    const summary = issue ? trigger || plainFirstLine(issue.description) : "";
    return { kind: "issue", title, summary: summary === title ? "" : summary };
  }
  if (task.chat_session_id) return { kind: "chat", title: trigger, summary: "" };
  if (task.autopilot_run_id || task.kind === "autopilot") {
    return { kind: "autopilot", title: trigger, summary: "" };
  }
  return { kind: "task", title: trigger, summary: "" };
}

function initialsOf(name: string): string {
  return name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

function StatusGlyph({ status }: { status: AgentTask["status"] }) {
  if (status === "running") {
    return <Loader2 className="size-3 shrink-0 animate-spin text-brand" aria-hidden />;
  }
  const tone =
    status === "queued" || status === "waiting_local_directory"
      ? "text-muted-foreground"
      : "text-info";
  return <Clock className={cn("size-3 shrink-0", tone)} aria-hidden />;
}

function statusKey(status: AgentTask["status"]) {
  switch (status) {
    case "running":
      return "running" as const;
    case "dispatched":
      return "dispatched" as const;
    case "waiting_local_directory":
      return "waiting_local_directory" as const;
    default:
      return "queued" as const;
  }
}

export function ActiveWorkSection() {
  const { t } = useT("layout");
  // Mirrors AppSidebar: the shell also renders before the workspace context
  // resolves, so read the workspace optionally and let `enabled` gate the
  // queries instead of throwing from a required-id hook.
  const wsId = useCurrentWorkspace()?.id ?? "";
  const p = useWorkspacePaths();
  const [expanded, setExpanded] = useState(false);

  const { data: snapshot = EMPTY_TASKS } = useQuery({
    ...agentTaskSnapshotOptions(wsId),
    enabled: !!wsId,
  });
  const { data: agents = EMPTY_AGENTS } = useQuery({
    ...agentListOptions(wsId),
    enabled: !!wsId,
  });

  const active = useMemo(() => orderActiveTasks(snapshot), [snapshot]);
  const visible = expanded ? active : active.slice(0, PREVIEW_LIMIT);

  // Only the rows on screen are worth resolving: the hidden tail costs nothing
  // until the user asks for it.
  const issueIds = useMemo(() => distinctIssueIds(visible), [visible]);
  const issueResults = useQueries({
    queries: issueIds.map((id) => ({ ...issueDetailOptions(wsId, id), enabled: !!wsId })),
  });
  const issuesById = useMemo(() => {
    const map = new Map<string, Issue>();
    issueIds.forEach((id, index) => {
      const issue = issueResults[index]?.data;
      if (issue) map.set(id, issue);
    });
    return map;
  }, [issueIds, issueResults]);

  const agentsById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);

  if (active.length === 0) return null;

  return (
    <Collapsible defaultOpen className="group-data-[collapsible=icon]:hidden">
      <SidebarGroup>
        <SidebarGroupLabel
          render={<CollapsibleTrigger />}
          className="group/trigger cursor-pointer hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground"
        >
          <span>{t(($) => $.active_work.label)}</span>
          <ChevronRight className="!size-3 ml-1 stroke-[2.5] transition-transform duration-200 group-data-[panel-open]/trigger:rotate-90" />
          <span className="ml-auto text-micro text-muted-foreground">{active.length}</span>
        </SidebarGroupLabel>
        <CollapsibleContent>
          <SidebarGroupContent>
            <ul className="flex flex-col gap-0.5">
              {visible.map((task) => {
                const agent = agentsById.get(task.agent_id);
                const content = deriveActiveWorkContent(
                  task,
                  task.issue_id ? issuesById.get(task.issue_id) : null,
                );
                const fallback =
                  content.kind === "chat"
                    ? t(($) => $.active_work.chat_run)
                    : content.kind === "autopilot"
                      ? t(($) => $.active_work.autopilot_run)
                      : t(($) => $.active_work.task_run);
                const agentName = agent?.name ?? t(($) => $.active_work.unknown_agent);
                const statusLabel = t(($) => $.active_work.status[statusKey(task.status)]);
                return (
                  <li key={task.id}>
                    <AppLink
                      href={task.issue_id ? p.issueDetail(task.issue_id) : p.agentDetail(task.agent_id)}
                      className="flex items-start gap-2 rounded-md px-2 py-1.5 text-muted-foreground transition-colors duration-75 hover:bg-sidebar-accent/70 active:bg-sidebar-accent"
                    >
                      <ActorAvatar
                        name={agentName}
                        initials={initialsOf(agentName)}
                        avatarUrl={resolvePublicFileUrl(agent?.avatar_url)}
                        isAgent
                        size="xs"
                        className="mt-0.5"
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex min-w-0 items-center gap-1 text-micro text-muted-foreground">
                          <StatusGlyph status={task.status} />
                          <span className="truncate">{agentName}</span>
                          <span aria-hidden>·</span>
                          <span className="shrink-0">{statusLabel}</span>
                        </div>
                        <p className="truncate text-caption text-sidebar-foreground">{content.title || fallback}</p>
                        {content.summary ? (
                          <p className="truncate text-micro text-muted-foreground">{content.summary}</p>
                        ) : null}
                      </div>
                    </AppLink>
                  </li>
                );
              })}
            </ul>
            {active.length > PREVIEW_LIMIT && (
              <button
                type="button"
                aria-expanded={expanded}
                onClick={() => setExpanded((v) => !v)}
                className="mt-0.5 w-full rounded-md px-2 py-1 pl-7 text-left text-caption text-muted-foreground transition-colors duration-75 hover:bg-sidebar-accent/70"
              >
                {expanded
                  ? t(($) => $.active_work.show_fewer)
                  : t(($) => $.active_work.show_more, { count: active.length - PREVIEW_LIMIT })}
              </button>
            )}
          </SidebarGroupContent>
        </CollapsibleContent>
      </SidebarGroup>
    </Collapsible>
  );
}

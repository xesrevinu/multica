// Fixture scene for the orchestrated issue chat timeline (?orch-timeline).
// Renders the OX-180 incident (same-error failures from three agents with
// engine churn between them) twice: mode=before uses the per-item components,
// mode=after the grouped feed. Used for screenshots and visual review.
import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { setApiInstance } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { setCurrentWorkspace } from "@multica/core/platform";
import type { TimelineEntry } from "@multica/core/types";
import { RESOURCES } from "@multica/views/locales";
import { NavigationProvider, type NavigationAdapter } from "@multica/views/navigation";
import { TooltipProvider } from "@multica/ui/components/ui/tooltip";
import { ActivitiesFold, AgentRunComment, EventsFeed, FailureGroupCard, groupChatItems, TransitionDivider, WorkedForFold, type ChatItem } from "@multica/views/orch/lab";
import { createFixtureApi, memoryStorage, user, workspace } from "./product-fixtures";

const AG = { reviewer: "a0000000-0000-4000-8000-000000000001", lead: "a0000000-0000-4000-8000-000000000002", eng: "a0000000-0000-4000-8000-000000000003" };
const agents = [
  { id: AG.reviewer, name: "yielded-reviewer" },
  { id: AG.lead, name: "yielded-lead" },
  { id: AG.eng, name: "heavy-engineer" },
].map((a) => ({ ...a, workspace_id: workspace.id, status: "idle", visibility: "workspace", runtime_id: "r", instructions: "", avatar_url: null }));
const base = createFixtureApi(() => "zh");
const api = new Proxy(base, {
  get(target, prop: string) {
    if (prop === "listAgents") return async () => agents;
    if (prop === "listTaskMessages") return async () => [];
    if (prop === "rerunIssue") return async () => Promise.reject(new Error("this issue is an epic: retry the failed sub-issue's run instead"));
    return (target as unknown as Record<string, unknown>)[prop];
  },
});
setApiInstance(api);
const auth = createAuthStore({ api, storage: memoryStorage() });
auth.setState({ user, isLoading: false, status: "authenticated" });
registerAuthStore(auth);
setCurrentWorkspace(workspace.slug, workspace.id);

const T0 = Date.parse("2026-10-22T12:39:52Z");
const at = (sec: number) => new Date(T0 + sec * 1000).toISOString();
let n = 0;
const ent = (sec: number, action: string, details: Record<string, unknown>, actor: [string, string] = ["system", ""]): TimelineEntry =>
  ({ id: `e${++n}`, type: "activity", action, actor_type: actor[0], actor_id: actor[1], created_at: at(sec), details }) as unknown as TimelineEntry;
const occ = (task: string) =>
  `prepare execution environment: [branch_occupied] orchestrator branch agent/ox-180 is held by the worktree at (none: the branch exists without the daemon's ownership record; delete or re-record it, then unblock); not forking a task branch and not touching it (execenv: wt switch agent/ox-180: ✗ Branch agent/ox-180 already exists) [${task}]`;
const failed = (id: string, agent: string, sec: number): ChatItem => {
  const task = { id, agent_id: agent, issue_id: "i", status: "failed", error: occ(id), failure_reason: "environment_prepare_failed", created_at: at(sec), started_at: null, completed_at: at(sec + 1), priority: 0, dispatched_at: at(sec), runtime_id: "r", result: null };
  const reply = { id: `c-${id}`, type: "comment", comment_type: "system", actor_type: "agent", actor_id: agent, source_task_id: id, content: "无法准备执行环境", created_at: at(sec + 1) } as unknown as TimelineEntry;
  return { kind: "run", id: reply.id, run: { task: task as never, hasReply: true, commentId: reply.id }, reply };
};
const transition = (sec: number, tid: string, why: string, status?: string, assignee?: string): ChatItem => ({ kind: "transition", id: `t${++n}`, entry: ent(sec, "orchestrator_transition", { tid, why, status, assignee }) });
const acts = (_sec: number, ...es: TimelineEntry[]): ChatItem => ({ kind: "activities", id: `a${++n}`, entries: es });

const items: ChatItem[] = [
  transition(0, "T2_review", "", "in_review", "yielded-reviewer"),
  acts(1, ent(1, "status_changed", { from: "todo", to: "in_review" }, ["agent", AG.eng]), ent(1, "assignee_changed", { to_type: "agent", to_id: AG.reviewer }, ["agent", AG.eng])),
  { kind: "run", id: "r-ok", run: { hasReply: false, task: { id: "01a163a0-019c", agent_id: AG.reviewer, status: "completed", created_at: at(2), started_at: at(2), completed_at: at(1078) } as never } },
  transition(1079, "T3b_rereview", "reviewer run ended without verdict"),
  failed("01a163b0-8f00", AG.reviewer, 1080),
  transition(1083, "T3b_rereview", "reviewer run failed (infra)"),
  failed("01a163b0-96ee", AG.reviewer, 1084),
  transition(1085, "T3b_rereview", "reviewer run failed (infra)"),
  acts(1086, ent(1086, "status_changed", { from: "in_review", to: "blocked" }, ["agent", AG.reviewer])),
  transition(1087, "T6_block", "infra attempts exhausted on review (2/2)", "blocked", "yielded-lead"),
  failed("01a163b1-0b3c", AG.lead, 1118),
  failed("01a163b4-5b94", AG.reviewer, 1335),
  failed("01a163b4-7374", AG.reviewer, 1341),
  failed("01a163b4-7d91", AG.lead, 1343),
  transition(2255, "T7_unblock", "", "todo", "heavy-engineer"),
  acts(2256, ent(2256, "status_changed", { from: "blocked", to: "todo" }, ["member", user.id])),
  failed("01a163c2-a1", AG.eng, 2257),
  transition(2259, "T6_block", "agent/ox-180 is checked out by another worktree at (none: the branch exists without the daemon's ownership record; delete or re-record it, then unblock): the orchestrator owns this branch and never forks or takes it over; free it (finish or remove that worktree, and do not use the agent/ prefix or an agent-<key> path for other work), then `multica flow unblock OX-180`", "blocked", "yielded-lead"),
];

function Before() {
  return (
    <>
      {items.map((it) => {
        const k = `${it.kind}-${it.id}`;
        if (it.kind === "transition") return <TransitionDivider key={k} entry={it.entry} />;
        if (it.kind === "activities") return <ActivitiesFold key={k} entries={it.entries} />;
        if (it.kind === "run" && it.reply) return <div key={k} className="my-2"><AgentRunComment run={it.run} standalone /></div>;
        if (it.kind === "run") return <WorkedForFold key={k} run={it.run} entering={false} agentName={agents.find((a) => a.id === it.run.task.agent_id)!.name} />;
        return null;
      })}
    </>
  );
}

function After() {
  return (
    <>
      {groupChatItems(items).map((it) => {
        const k = `${it.kind}-${it.id}`;
        if (it.kind === "events") return <EventsFeed key={k} entries={it.entries} />;
        if (it.kind === "failures") return <FailureGroupCard key={k} runs={it.runs} between={it.between} issueId="i" />;
        if (it.kind === "run" && it.reply) return <div key={k} className="my-2"><AgentRunComment run={it.run} standalone /></div>;
        if (it.kind === "run") return <WorkedForFold key={k} run={it.run} entering={false} agentName={agents.find((a) => a.id === it.run.task.agent_id)!.name} />;
        return null;
      })}
    </>
  );
}

const nav: NavigationAdapter = {
  pathname: "/ui-lab/issues/i",
  searchParams: new URLSearchParams(),
  hash: "",
  push: () => {},
  replace: () => {},
  back: () => {},
  getShareableUrl: (v) => v,
};

export function OrchTimelinePreview() {
  const params = new URLSearchParams(location.search);
  const mode = params.get("mode") === "before" ? "before" : "after";
  const locale = params.get("lang") === "en" ? "en" : "zh-Hans";
  const [client] = useState(() => {
    const q = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    q.setQueryData(workspaceKeys.list(), [workspace]);
    return q;
  });
  return (
    <QueryClientProvider client={client}>
      <I18nProvider locale={locale} resources={{ en: RESOURCES.en, "zh-Hans": RESOURCES["zh-Hans"] }}>
        <WorkspaceSlugProvider slug={workspace.slug}>
          <NavigationProvider value={nav}>
          <TooltipProvider>
            <div className="min-h-svh bg-background px-6 py-6 text-foreground">
              <div className="mx-auto flex w-full max-w-3xl flex-col" data-testid="orch-timeline-preview">
                <div className="mb-3 text-caption text-muted-foreground">OX-180 · {mode}</div>
                {mode === "before" ? <Before /> : <After />}
              </div>
            </div>
          </TooltipProvider>
          </NavigationProvider>
        </WorkspaceSlugProvider>
      </I18nProvider>
    </QueryClientProvider>
  );
}

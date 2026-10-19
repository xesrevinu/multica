"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, GitPullRequest, Pause, Search } from "lucide-react";
import {
  filterOrchIssues,
  groupOrchIssues,
  orchIssuesOptions,
  useOrchRealtime,
  type OrchAttentionBucket,
  type OrchIssueNode,
} from "@multica/core/orchestrator";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import { SidebarGroup, SidebarGroupContent, SidebarGroupLabel, SidebarMenu } from "@multica/ui/components/ui/sidebar";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { StatusIcon } from "../../issues/components/status-icon";
import { checksTone, formatDuration, since, TONE_DOT } from "../format";
import { usePhaseLabel } from "../labels";
import { OpenTabButton } from "../open-tab";

export type IssueAttentionListProps = {
  wsId: string;
  pathname: string;
  issueHref: (key: string) => string;
};

const BUCKET_TONE: Record<OrchAttentionBucket, string> = {
  needs_you: "text-amber-600 dark:text-amber-400",
  running: "text-emerald-600 dark:text-emerald-400",
  queued: "text-muted-foreground",
  settled: "text-muted-foreground",
};

// The attention-sorted issue list (server order within buckets). Settled is
// collapsed by default; sub-issues expand under their parent.
export function IssueAttentionList({ wsId, pathname, issueHref }: IssueAttentionListProps) {
  const { t } = useT("orch");
  useOrchRealtime(wsId);
  const { data: issues, isLoading } = useQuery(orchIssuesOptions(wsId));
  const [query, setQuery] = useState("");
  const [project, setProject] = useState<string>("");
  const projects = useMemo(() => {
    const m = new Map<string, string>();
    for (const i of issues ?? []) if (i.project) m.set(i.project.id, i.project.title);
    return [...m.entries()].sort((a, b) => a[1].localeCompare(b[1]));
  }, [issues]);
  const groups = useMemo(() => groupOrchIssues(filterOrchIssues(issues ?? [], query, project || null)), [issues, query, project]);

  return (
    <SidebarGroup className="pt-0" data-testid="orch-issue-list">
      <SidebarGroupLabel>{t(($) => $.sidebar.issues)}</SidebarGroupLabel>
      <div className="mb-1 flex items-center gap-1 px-1">
        <label className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md border border-sidebar-border/70 bg-background/40 px-2 py-1 focus-within:border-ring">
          <Search className="size-3 shrink-0 text-muted-foreground" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t(($) => $.sidebar.filter_placeholder)}
            aria-label={t(($) => $.sidebar.filter_placeholder)}
            className="min-w-0 flex-1 bg-transparent text-caption outline-none placeholder:text-muted-foreground"
          />
        </label>
        {projects.length > 1 && (
          <select
            value={project}
            onChange={(e) => setProject(e.target.value)}
            aria-label={t(($) => $.sidebar.all_projects)}
            className="max-w-24 truncate rounded-md border border-sidebar-border/70 bg-background/40 px-1 py-1 text-caption text-muted-foreground outline-none"
          >
            <option value="">{t(($) => $.sidebar.all_projects)}</option>
            {projects.map(([id, title]) => (
              <option key={id} value={id}>
                {title}
              </option>
            ))}
          </select>
        )}
      </div>
      <SidebarGroupContent>
        {isLoading && <p className="px-2 py-1 text-caption text-muted-foreground">{t(($) => $.sidebar.loading)}</p>}
        {!isLoading && groups.every((g) => g.count === 0) && (
          <p className="px-2 py-1 text-caption text-muted-foreground">{t(($) => $.sidebar.empty)}</p>
        )}
        {groups
          .filter((g) => g.count > 0)
          .map((g) => (
            <Collapsible key={g.bucket} defaultOpen={g.bucket !== "settled"}>
              <CollapsibleTrigger
                className="group/trigger flex w-full items-center gap-1 rounded-md px-2 pt-2 pb-1 text-micro font-medium uppercase tracking-wide text-muted-foreground hover:text-foreground"
                data-testid={`orch-bucket-${g.bucket}`}
              >
                <ChevronRight className="size-3 stroke-[2.5] transition-transform duration-200 group-data-[panel-open]/trigger:rotate-90" />
                <span className={BUCKET_TONE[g.bucket]}>{t(($) => $.sidebar.bucket[g.bucket])}</span>
                <span className="ml-auto tabular-nums">{g.count}</span>
              </CollapsibleTrigger>
              <CollapsibleContent>
                <SidebarMenu className="gap-px">
                  {g.nodes.map((n) => (
                    <IssueRow key={`${g.bucket}-${n.id}`} node={n} depth={0} pathname={pathname} issueHref={issueHref} />
                  ))}
                </SidebarMenu>
              </CollapsibleContent>
            </Collapsible>
          ))}
      </SidebarGroupContent>
    </SidebarGroup>
  );
}

function IssueRow({ node, depth, pathname, issueHref }: { node: OrchIssueNode; depth: number; pathname: string; issueHref: (key: string) => string }) {
  const { t } = useT("orch");
  const phaseLabel = usePhaseLabel();
  const [open, setOpen] = useState(false);
  const href = issueHref(node.key);
  const active = pathname === href || pathname === issueHref(node.id);
  const settled = node.attention.bucket === "settled";
  const reason = node.attention.reason as keyof typeof REASONS | undefined;
  const hasKids = node.subIssues.length > 0;
  const meta: React.ReactNode[] = [];
  if (node.running) {
    meta.push(
      <span key="run" className="inline-flex items-center gap-1 truncate">
        <span className="size-1.5 shrink-0 animate-pulse rounded-full bg-emerald-500" />
        {node.running.agent} · {formatDuration(since(node.running.started_at ?? node.running.created_at))}
      </span>,
    );
  } else if (node.phase && !settled && node.attention.bucket !== "needs_you") {
    meta.push(
      <span key="phase" className="truncate">
        {phaseLabel(node.phase)}
      </span>,
    );
  }
  if (node.pr) {
    const tone = node.pr.state === "merged" ? "ok" : checksTone(node.pr.checks);
    meta.push(
      <span key="pr" className="inline-flex shrink-0 items-center gap-0.5 tabular-nums">
        <GitPullRequest className="size-3" />#{node.pr.number}
        <span className={cn("size-1.5 rounded-full", TONE_DOT[tone])} />
      </span>,
    );
  }
  if (node.children.total > 0) {
    meta.push(
      <span key="kids" className="shrink-0 tabular-nums">
        {node.children.done}/{node.children.total}
      </span>,
    );
  }
  return (
    <li className="list-none">
      <div
        className={cn(
          "group/row relative flex items-start gap-1.5 rounded-md py-1 pr-1.5 text-body transition-colors duration-75",
          active ? "bg-sidebar-accent text-sidebar-accent-foreground" : "text-muted-foreground hover:bg-sidebar-accent/70",
        )}
        style={{ paddingLeft: 6 + depth * 14 }}
      >
        {hasKids ? (
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-label={t(($) => $.sidebar.toggle_sub_issues)}
            aria-expanded={open}
            className="mt-0.5 -ml-0.5 shrink-0 rounded-sm p-0.5 hover:bg-sidebar-accent"
          >
            <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
          </button>
        ) : (
          <span className="w-4 shrink-0" />
        )}
        <AppLink href={href} className="flex min-w-0 flex-1 flex-col gap-0.5" data-testid="orch-issue-row" data-key={node.key}>
          <span className="flex min-w-0 items-center gap-1.5">
            <StatusIcon status={node.status} className="size-3.5 shrink-0" />
            <span className={cn("truncate", !settled && "text-foreground", active && "text-foreground")}>{node.title || node.key}</span>
            {node.paused && <Pause className="size-3 shrink-0 text-muted-foreground" />}
            {reason && REASONS[reason] && node.attention.bucket === "needs_you" && (
              <span className="ml-auto shrink-0 rounded-sm bg-amber-500/15 px-1 text-micro font-medium text-amber-700 dark:text-amber-300">
                {t(($) => $.sidebar.reason[reason])}
              </span>
            )}
          </span>
          {!settled && (
            <span className="flex min-w-0 items-center gap-1.5 pl-5 text-micro text-muted-foreground">
              <span className="shrink-0 font-mono">{node.key}</span>
              {meta.map((m, i) => (
                <span key={i} className="flex min-w-0 items-center gap-1.5">
                  <span className="text-faint-foreground">·</span>
                  {m}
                </span>
              ))}
            </span>
          )}
        </AppLink>
        <OpenTabButton
          issueKey={node.key}
          title={node.title}
          className="absolute top-1 right-1 hidden bg-sidebar group-hover/row:block"
        />
      </div>
      {hasKids && open && (
        <ul className="flex flex-col gap-px">
          {node.subIssues.map((c) => (
            <IssueRow key={c.id} node={c} depth={depth + 1} pathname={pathname} issueHref={issueHref} />
          ))}
        </ul>
      )}
    </li>
  );
}

const REASONS = { acceptance: 1, blocked: 1, cancel_proposed: 1, review: 1, paused: 1 } as const;

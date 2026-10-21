"use client";

import { useCallback, useMemo, useState, type KeyboardEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bell, ChevronRight, CircleAlert, GitBranch, Pause, Pin, PinOff, Search, SlidersHorizontal } from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import {
  applyOrchView,
  DEFAULT_ORCH_VIEW,
  diffOf,
  filterOrchIssues,
  groupOrchIssues,
  orchIssuesOptions,
  useOrchRealtime,
  type OrchAttentionBucket,
  type OrchIssueNode,
  type OrchSort,
  type OrchAssigneeFilter,
} from "@multica/core/orchestrator";
import { inboxListOptions } from "@multica/core/inbox/queries";
import { pinListOptions, useCreatePin, useDeletePin } from "@multica/core/pins";
import { projectListOptions } from "@multica/core/projects/queries";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { SidebarGroup, SidebarGroupContent, SidebarGroupLabel } from "@multica/ui/components/ui/sidebar";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../../navigation";
import { useRegisterOrchPalette, type OrchPaletteItem } from "../palette";
import { useOrchNeedsYouNotify } from "../use-orch-notify";
import { useT } from "../../i18n";
import { formatDuration, shortAgo, since } from "../format";
import { usePhaseLabel } from "../labels";
import { OpenTabButton } from "../open-tab";
import { DiffStat, PRChip, ProjectBadge, WorkingLabel } from "../parts/chips";
import { Collapse } from "../parts/collapse";
import { useOrchViewOptions } from "./view-options";

export type IssueAttentionListProps = {
  wsId: string;
  pathname: string;
  issueHref: (key: string) => string;
};

const PRIORITIES = ["urgent", "high", "medium", "low", "none"] as const;
const SORTS: OrchSort[] = ["attention", "updated", "created", "priority"];
const ASSIGNEES: OrchAssigneeFilter[] = ["any", "me", "agents", "unassigned"];

type RowCtx = {
  pathname: string;
  issueHref: (key: string) => string;
  projectIcon: (id?: string) => string | null | undefined;
  pinned: Set<string>;
  unread: Set<string>;
  togglePin: (id: string, pinned: boolean) => void;
  nest: boolean;
};

// The attention-sorted issue list, t3code style: two-line rows with a
// project badge, live status on the right and PR / diff chips underneath.
// Settled issues fold into a "Settled (n)" footer.
export function IssueAttentionList({ wsId, pathname, issueHref }: IssueAttentionListProps) {
  const { t } = useT("orch");
  useOrchRealtime(wsId);
  const userId = useAuthStore((s) => s.user?.id ?? "");
  const [settledLimit, setSettledLimit] = useState(30);
  const [showSettled, setShowSettled] = useState(false);
  const { data, isLoading } = useQuery(orchIssuesOptions(wsId, showSettled ? settledLimit : 30));
  const issues = data?.issues;
  const { view, update, reset } = useOrchViewOptions(wsId);
  const [query, setQuery] = useState("");
  const { data: projectList = [] } = useQuery(projectListOptions(wsId));
  const { data: pins = [] } = useQuery({ ...pinListOptions(wsId, userId), enabled: !!userId });
  const createPin = useCreatePin();
  const deletePin = useDeletePin();
  const pinned = useMemo(() => new Set(pins.filter((p) => p.item_type === "issue").map((p) => p.item_id)), [pins]);
  const { data: inbox = [] } = useQuery(inboxListOptions(wsId));
  const unread = useMemo(() => new Set(inbox.filter((i) => !i.read && !i.archived && i.issue_id).map((i) => i.issue_id as string)), [inbox]);
  const nav = useNavigation();
  const openIssue = useCallback((key: string) => nav.push(issueHref(key)), [nav, issueHref]);
  useOrchNeedsYouNotify(issues, openIssue);
  const paletteItems = useMemo<OrchPaletteItem[]>(
    () =>
      (issues ?? [])
        .filter((i) => i.attention.bucket === "needs_you")
        .slice(0, 8)
        .map((i) => ({
          key: `orch-needs-${i.key}`,
          label: t(($) => $.palette.needs_you, { key: i.key, title: i.title }),
          icon: CircleAlert,
          keywords: ["needs", "attention", "accept", i.key.toLowerCase(), ...i.title.toLowerCase().split(/\s+/)],
          onSelect: () => openIssue(i.key),
        })),
    [issues, openIssue, t],
  );
  useRegisterOrchPalette("sidebar-needs-you", paletteItems);
  const [notifyPerm, setNotifyPerm] = useState<NotificationPermission | "unsupported">(() =>
    typeof Notification === "undefined" ? "unsupported" : Notification.permission,
  );
  const enableNotifications = () => {
    if (typeof Notification === "undefined") return;
    if (Notification.permission === "denied") {
      toast.error(t(($) => $.notify.denied));
      return;
    }
    void Notification.requestPermission().then(setNotifyPerm);
  };

  const projects = useMemo(() => {
    const m = new Map<string, string>();
    for (const i of issues ?? []) if (i.project) m.set(i.project.id, i.project.title);
    return [...m.entries()].sort((a, b) => a[1].localeCompare(b[1]));
  }, [issues]);
  const iconById = useMemo(() => new Map(projectList.map((p) => [p.id, p.icon])), [projectList]);

  const groups = useMemo(() => {
    const filtered = applyOrchView(filterOrchIssues(issues ?? [], query, view.project || null), view, userId);
    const flat = view.showSubIssues ? filtered : filtered.map((i) => ({ ...i, parent_id: undefined }));
    return groupOrchIssues(flat);
  }, [issues, query, view, userId]);
  const open = groups.filter((g) => g.bucket !== "settled");
  const settled = groups.find((g) => g.bucket === "settled");
  const settledTotal = Math.max(data?.settledTotal ?? 0, settled?.count ?? 0);
  const customized = JSON.stringify({ ...view, project: undefined }) !== JSON.stringify({ ...DEFAULT_ORCH_VIEW, project: undefined }) || !!view.project;

  const ctx: RowCtx = {
    pathname,
    issueHref,
    projectIcon: (id) => (id ? iconById.get(id) : undefined),
    pinned,
    unread,
    togglePin: (id, isPinned) =>
      isPinned ? deletePin.mutate({ itemType: "issue", itemId: id }) : createPin.mutate({ item_type: "issue", item_id: id }),
    nest: view.showSubIssues,
  };

  const onListKeyDown = (e: KeyboardEvent) => {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    const tag = (e.target as HTMLElement).tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
    const root = e.currentTarget;
    const rows = [...root.querySelectorAll<HTMLElement>('[data-testid="orch-issue-row"]')];
    if (rows.length === 0) return;
    const current = rows.findIndex((r) => r === document.activeElement);
    const next = e.key === "ArrowDown" ? Math.min(rows.length - 1, current + 1) : Math.max(0, current < 0 ? 0 : current - 1);
    e.preventDefault();
    rows[next]?.focus();
  };

  return (
    <SidebarGroup className="min-w-0 max-w-full overflow-x-hidden pt-0" data-testid="orch-issue-list" onKeyDown={onListKeyDown}>
      <SidebarGroupLabel className="sticky top-0 z-10 bg-sidebar">{t(($) => $.sidebar.issues)}</SidebarGroupLabel>
      <div className="mb-1 flex w-full min-w-0 items-center gap-1 px-1" data-testid="orch-filter-row">
        <label className="flex h-8 min-w-0 flex-1 items-center gap-1.5 overflow-hidden rounded-md border border-sidebar-border bg-background/60 px-2 focus-within:border-ring">
          <Search className="size-3 shrink-0 text-muted-foreground" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t(($) => $.sidebar.filter_placeholder)}
            aria-label={t(($) => $.sidebar.filter_placeholder)}
            className="w-0 min-w-0 flex-1 bg-transparent text-caption outline-none placeholder:text-faint-foreground"
          />
        </label>
        <DropdownMenu>
          <DropdownMenuTrigger
            aria-label={t(($) => $.sidebar.view_options)}
            title={t(($) => $.sidebar.view_options)}
            data-testid="orch-view-options"
            className={cn(
              "relative inline-flex size-8 shrink-0 items-center justify-center rounded-md border border-sidebar-border bg-background/60 text-muted-foreground hover:text-foreground",
              customized && "text-foreground",
            )}
          >
            <SlidersHorizontal className="size-3.5" />
            {customized && <span className="absolute top-1 right-1 size-1.5 rounded-full bg-brand" />}
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuGroup>
              <DropdownMenuLabel>{t(($) => $.sidebar.sort_label)}</DropdownMenuLabel>
              <DropdownMenuRadioGroup value={view.sort} onValueChange={(v) => update({ sort: v as OrchSort })}>
                {SORTS.map((s) => (
                  <DropdownMenuRadioItem key={s} value={s}>
                    {t(($) => $.sidebar.sort[s])}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel>{t(($) => $.sidebar.assignee_label)}</DropdownMenuLabel>
              <DropdownMenuRadioGroup value={view.assignee} onValueChange={(v) => update({ assignee: v as OrchAssigneeFilter })}>
                {ASSIGNEES.map((a) => (
                  <DropdownMenuRadioItem key={a} value={a}>
                    {t(($) => $.sidebar.assignee[a])}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel>{t(($) => $.sidebar.priority_label)}</DropdownMenuLabel>
              {PRIORITIES.map((p) => (
                <DropdownMenuCheckboxItem
                  key={p}
                  checked={view.priorities.includes(p)}
                  onCheckedChange={(on) =>
                    update({ priorities: on ? [...view.priorities, p] : view.priorities.filter((x) => x !== p) })
                  }
                >
                  <PriorityLabel value={p} />
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuGroup>
            {projects.length > 0 && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuLabel>{t(($) => $.sidebar.project_label)}</DropdownMenuLabel>
                  <DropdownMenuRadioGroup value={view.project} onValueChange={(v) => update({ project: v as string })}>
                    <DropdownMenuRadioItem value="">{t(($) => $.sidebar.all_projects)}</DropdownMenuRadioItem>
                    {projects.map(([id, title]) => (
                      <DropdownMenuRadioItem key={id} value={id}>
                        <ProjectBadge title={title} icon={iconById.get(id)} />
                        <span className="truncate">{title}</span>
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuGroup>
              </>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuCheckboxItem checked={view.orchestratedOnly} onCheckedChange={(on) => update({ orchestratedOnly: !!on })}>
              {t(($) => $.sidebar.orchestrated_only)}
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem checked={view.showSubIssues} onCheckedChange={(on) => update({ showSubIssues: !!on })}>
              {t(($) => $.sidebar.show_sub_issues)}
            </DropdownMenuCheckboxItem>
            {notifyPerm !== "unsupported" && (
              <DropdownMenuCheckboxItem checked={notifyPerm === "granted"} disabled={notifyPerm === "granted"} onCheckedChange={enableNotifications}>
                <Bell className="size-3.5" />
                {t(($) => $.notify.enable)}
              </DropdownMenuCheckboxItem>
            )}
            {customized && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={reset}>{t(($) => $.sidebar.reset_view)}</DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <SidebarGroupContent className="flex flex-col">
        {isLoading && <p className="px-2.5 py-1 text-caption text-muted-foreground">{t(($) => $.sidebar.loading)}</p>}
        {!isLoading && groups.every((g) => g.count === 0) && (
          <p className="px-2.5 py-1 text-caption text-muted-foreground">{t(($) => $.sidebar.empty)}</p>
        )}
        {open
          .filter((g) => g.count > 0)
          .map((g) => (
            <section key={g.bucket} data-testid={`orch-bucket-${g.bucket}`} className="flex flex-col">
              <BucketHeader bucket={g.bucket} count={g.count} />
              <ul className="flex flex-col gap-px">
                {g.nodes.map((n) => (
                  <IssueRow key={`${g.bucket}-${n.id}`} node={n} depth={0} ctx={ctx} />
                ))}
              </ul>
            </section>
          ))}
        {settledTotal > 0 && (
          <section data-testid="orch-bucket-settled" className="mt-1 flex flex-col">
            <button
              type="button"
              onClick={() => setShowSettled((v) => !v)}
              aria-expanded={showSettled}
              className="flex h-8 w-full items-center gap-1.5 rounded-md px-2.5 text-left text-label text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground"
            >
              <ChevronRight className={cn("size-3.5 transition-transform", showSettled && "rotate-90")} />
              {t(($) => $.sidebar.settled_footer, { count: settledTotal })}
            </button>
            <Collapse open={showSettled && !!settled}>
              {settled && (
                <ul className="flex flex-col gap-px">
                  {settled.nodes.map((n) => (
                    <IssueRow key={`s-${n.id}`} node={n} depth={0} ctx={ctx} />
                  ))}
                  {settledTotal > settled.count && settledLimit < 500 && (
                    <li>
                      <button
                        type="button"
                        onClick={() => setSettledLimit((n) => Math.min(500, n + 50))}
                        className="w-full rounded-md px-2.5 py-1 text-left text-caption text-muted-foreground hover:text-foreground"
                      >
                        {t(($) => $.sidebar.load_more)}
                      </button>
                    </li>
                  )}
                </ul>
              )}
            </Collapse>
          </section>
        )}
      </SidebarGroupContent>
    </SidebarGroup>
  );
}

function PriorityLabel({ value }: { value: (typeof PRIORITIES)[number] }) {
  const { t } = useT("issues");
  return <span>{t(($) => $.priority[value])}</span>;
}

const BUCKET_DOT: Record<OrchAttentionBucket, string> = {
  needs_you: "bg-amber-500",
  running: "bg-sky-500",
  queued: "bg-border",
  settled: "bg-border",
};

function BucketHeader({ bucket, count }: { bucket: OrchAttentionBucket; count: number }) {
  const { t } = useT("orch");
  return (
    <div className="sticky top-7 z-[1] flex h-7 items-center gap-1.5 bg-sidebar px-2.5 text-caption font-medium text-muted-foreground">
      <span className={cn("size-1.5 rounded-full", BUCKET_DOT[bucket])} />
      <span>{t(($) => $.sidebar.bucket[bucket])}</span>
      <span className="ml-auto tabular-nums text-faint-foreground">{count}</span>
    </div>
  );
}

const REASONS = { acceptance: 1, blocked: 1, cancel_proposed: 1, review: 1, paused: 1 } as const;

function IssueRow({ node, depth, ctx }: { node: OrchIssueNode; depth: number; ctx: RowCtx }) {
  const { t } = useT("orch");
  const phaseLabel = usePhaseLabel();
  const [open, setOpen] = useState(false);
  const href = ctx.issueHref(node.key);
  const active = ctx.pathname === href || ctx.pathname === ctx.issueHref(node.id);
  const settled = node.attention.bucket === "settled";
  const reason = node.attention.reason as keyof typeof REASONS | undefined;
  const hasKids = ctx.nest && node.subIssues.length > 0;
  const isPinned = ctx.pinned.has(node.id);
  const isUnread = ctx.unread.has(node.id) && !active;
  const diff = diffOf(node);
  const branch = node.worktree?.branch ?? node.pr?.branch;

  let status: React.ReactNode;
  if (node.attention.bucket === "needs_you" && reason && REASONS[reason]) {
    status = (
      <span className="rounded-[4px] bg-amber-500/15 px-1 text-micro font-medium text-amber-700 dark:text-amber-300">
        {t(($) => $.sidebar.reason[reason])}
      </span>
    );
  } else if (node.running) {
    status = (
      <WorkingLabel
        label={t(($) => $.sidebar.working, { duration: formatDuration(since(node.running.started_at ?? node.running.created_at)) })}
      />
    );
  } else if (node.paused) {
    status = (
      <span className="inline-flex items-center gap-1 text-muted-foreground">
        <Pause className="size-3" />
        {t(($) => $.sidebar.paused_short)}
      </span>
    );
  } else {
    status = <span className="tabular-nums text-faint-foreground">{shortAgo(node.updated_at)}</span>;
  }

  return (
    <li className="list-none">
      <div
        data-active={active || undefined}
        className={cn(
          "group/row relative rounded-md transition-colors duration-75",
          active ? "bg-sidebar-accent" : "hover:bg-sidebar-accent/60",
        )}
        style={{ paddingLeft: depth * 12 }}
      >
        <AppLink
          href={href}
          title={node.title || node.key}
          className="flex min-h-9 min-w-0 flex-col justify-center gap-0.5 overflow-hidden px-2.5 py-1 outline-none focus-visible:bg-sidebar-accent"
          data-testid="orch-issue-row"
          data-key={node.key}
        >
          <span className="flex h-5 min-w-0 items-center gap-1.5">
            <ProjectBadge title={node.project?.title} icon={ctx.projectIcon(node.project?.id)} className={cn(settled && !active && "opacity-50 grayscale")} />
            <span className={cn("min-w-0 flex-1 truncate text-label font-medium", settled ? "text-muted-foreground" : "text-foreground", isUnread && "font-semibold")}>
              {node.title || node.key}
            </span>
            {isUnread && <span className="size-1.5 shrink-0 rounded-full bg-brand" aria-label={t(($) => $.sidebar.unread)} data-testid="orch-unread" />}
            {isPinned && <Pin className="size-3 shrink-0 text-faint-foreground" />}
            <span className="flex shrink-0 items-center text-caption transition-opacity group-hover/row:opacity-0">{status}</span>
          </span>
          {!settled || active ? (
            <span className="flex h-4 min-w-0 items-center gap-1.5 pl-[22px] text-caption text-muted-foreground">
              <span className="shrink-0 font-mono text-micro">{node.key}</span>
              {branch ? (
                <span className="flex min-w-0 flex-1 items-center gap-1 text-faint-foreground">
                  <GitBranch className="size-3 shrink-0" />
                  <span className="truncate font-mono text-micro">{branch}</span>
                </span>
              ) : node.phase && !node.running && node.attention.bucket !== "needs_you" ? (
                <span className="min-w-0 flex-1 truncate text-faint-foreground">{phaseLabel(node.phase)}</span>
              ) : (
                <span className="flex-1" />
              )}
              {node.pr && <PRChip number={node.pr.number} state={node.pr.state} />}
              <DiffStat additions={diff.additions} deletions={diff.deletions} className="text-micro" />
              {node.children.total > 0 && (
                <span className="shrink-0 tabular-nums">
                  {node.children.done}/{node.children.total}
                </span>
              )}
            </span>
          ) : null}
        </AppLink>
        <span className="absolute top-1.5 right-1.5 hidden items-center gap-0.5 rounded-md bg-sidebar-accent group-hover/row:flex">
          <button
            type="button"
            aria-label={isPinned ? t(($) => $.sidebar.unpin) : t(($) => $.sidebar.pin)}
            title={isPinned ? t(($) => $.sidebar.unpin) : t(($) => $.sidebar.pin)}
            data-testid="orch-pin"
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              ctx.togglePin(node.id, isPinned);
            }}
            className="rounded-sm p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            {isPinned ? <PinOff className="size-3.5" /> : <Pin className="size-3.5" />}
          </button>
          <OpenTabButton issueKey={node.key} title={node.title} />
        </span>
        {hasKids && (
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-label={t(($) => $.sidebar.toggle_sub_issues)}
            aria-expanded={open}
            data-testid="orch-toggle-sub"
            className="absolute top-[26px] left-1 rounded-sm p-0.5 text-faint-foreground hover:bg-muted hover:text-foreground"
            style={{ left: depth * 12 + 4 }}
          >
            <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
          </button>
        )}
      </div>
      {hasKids && (
        <Collapse open={open}>
          <ul className="flex flex-col gap-px">
            {node.subIssues.map((c) => (
              <IssueRow key={c.id} node={c} depth={depth + 1} ctx={ctx} />
            ))}
          </ul>
        </Collapse>
      )}
    </li>
  );
}

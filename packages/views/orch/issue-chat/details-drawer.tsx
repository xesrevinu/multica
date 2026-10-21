"use client";

import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ExternalLink, Pencil } from "lucide-react";
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { orchIssuesOptions, useOrchSetMeta, useOrchSetRisk, type OrchTimeline } from "@multica/core/orchestrator";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@multica/ui/components/ui/sheet";
import { Switch } from "@multica/ui/components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { cn } from "@multica/ui/lib/utils";
import { ContentEditor, ReadonlyContent } from "../../editor";
import { useT, useTimeAgo } from "../../i18n";
import { AppLink } from "../../navigation";
import { StatusIcon } from "../../issues/components/status-icon";
import { LabelPicker, PriorityPicker } from "../../issues/components/pickers";
import { ProjectPicker } from "../../projects/components/project-picker";
import { shortSha } from "../format";
import { usePhaseLabel } from "../labels";
import { OpenTabButton } from "../open-tab";
import { FilesTab } from "./drawer-files";
import { TerminalTab, WorktreesTab } from "./drawer-worktrees";
import { useStatusText } from "./timeline-parts";

export type DrawerTab = "details" | "files" | "worktrees" | "terminal";
const RISKS = ["low", "medium", "high"] as const;
const DRAWER_WIDTH_KEY = "orch-drawer-width";
const DRAWER_WIDTH_MIN = 320;
const DRAWER_WIDTH_MAX = 720;

function useDrawerWidth() {
  const [width, setWidth] = useState(512);
  const widthRef = useRef(width);
  widthRef.current = width;
  useEffect(() => {
    const stored = Number(localStorage.getItem(DRAWER_WIDTH_KEY));
    if (stored >= DRAWER_WIDTH_MIN && stored <= DRAWER_WIDTH_MAX) setWidth(stored);
  }, []);
  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const startX = e.clientX;
    const startW = widthRef.current;
    const move = (ev: PointerEvent) => {
      const next = Math.min(DRAWER_WIDTH_MAX, Math.max(DRAWER_WIDTH_MIN, startW + (startX - ev.clientX)));
      widthRef.current = next;
      setWidth(next);
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      localStorage.setItem(DRAWER_WIDTH_KEY, String(widthRef.current));
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  return { width, onPointerDown };
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid min-h-8 grid-cols-[7.5rem_1fr] items-center gap-2 text-caption">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 break-words">{children}</span>
    </div>
  );
}

function Section({ title, children, className }: { title: string; children: React.ReactNode; className?: string }) {
  return (
    <section className={cn("border-t border-border py-3 first:border-t-0", className)}>
      <h3 className="mb-1.5 text-caption font-medium text-muted-foreground">{title}</h3>
      {children}
    </section>
  );
}

// DetailsDrawer: description, properties (edited in place), flow state,
// sub-issues — plus Files changed, Worktrees and a Terminal placeholder.
export function DetailsDrawer({
  open,
  onOpenChange,
  wsId,
  issue,
  timeline,
  tab = "details",
  onTabChange,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  wsId: string;
  issue: Issue;
  timeline: OrchTimeline | null | undefined;
  tab?: DrawerTab;
  onTabChange?: (t: DrawerTab) => void;
}) {
  const { t } = useT("orch");
  const isMobile = useIsMobile();
  const { width, onPointerDown } = useDrawerWidth();
  const p = useWorkspacePaths();
  const [localTab, setLocalTab] = useState<DrawerTab>(tab);
  const current = onTabChange ? tab : localTab;
  const setTab = (v: DrawerTab) => (onTabChange ? onTabChange(v) : setLocalTab(v));
  const v = timeline?.issue;
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side={isMobile ? "bottom" : "right"}
        className={isMobile ? "max-h-[88vh] rounded-t-2xl pb-[env(safe-area-inset-bottom)]" : "sm:max-w-none"}
        style={isMobile ? undefined : { width, maxWidth: width }}
        data-testid="orch-details-drawer"
      >
        {!isMobile && (
          <div
            role="separator"
            aria-orientation="vertical"
            aria-label={t(($) => $.drawer.resize)}
            onPointerDown={onPointerDown}
            className="absolute inset-y-0 left-0 z-10 w-1.5 cursor-col-resize touch-none hover:bg-border"
          />
        )}
        <SheetHeader className="gap-1 border-b border-border pb-0">
          <p className="font-mono text-micro text-muted-foreground">{issue.identifier}</p>
          <SheetTitle className="pr-8 text-title-sm">{issue.title}</SheetTitle>
          <AppLink href={`${p.issueDetail(issue.identifier)}?view=classic`} className="inline-flex w-fit items-center gap-1 text-caption text-muted-foreground hover:text-foreground">
            <ExternalLink className="size-3" />
            {t(($) => $.drawer.open_classic)}
          </AppLink>
          <Tabs value={current} onValueChange={(x) => setTab(x as DrawerTab)} className="mt-2">
            <TabsList variant="line" className="h-8 w-full justify-start gap-3 overflow-x-auto p-0">
              <TabsTrigger value="details" className="flex-none shrink-0 px-0 text-label" data-testid="orch-drawer-tab-details">{t(($) => $.drawer.tab_details)}</TabsTrigger>
              <TabsTrigger value="files" className="flex-none shrink-0 px-0 text-label" data-testid="orch-drawer-tab-files">{t(($) => $.drawer.tab_files)}</TabsTrigger>
              <TabsTrigger value="worktrees" className="flex-none shrink-0 px-0 text-label" data-testid="orch-drawer-tab-worktrees">{t(($) => $.drawer.tab_worktrees)}</TabsTrigger>
              <TabsTrigger value="terminal" className="flex-none shrink-0 px-0 text-label" data-testid="orch-drawer-tab-terminal">{t(($) => $.drawer.tab_terminal)}</TabsTrigger>
            </TabsList>
          </Tabs>
        </SheetHeader>
        <div className="min-h-0 flex-1 overflow-y-auto">
          {current === "details" && <DetailsTab wsId={wsId} issue={issue} timeline={timeline} open={open} />}
          {current === "files" && <FilesTab wsId={wsId} issueKey={issue.identifier} enabled={open && !!v?.pr} />}
          {current === "worktrees" && <WorktreesTab wsId={wsId} issueKey={issue.identifier} />}
          {current === "terminal" && <TerminalTab path={v?.worktree?.path} />}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function DetailsTab({ wsId, issue, timeline, open }: { wsId: string; issue: Issue; timeline: OrchTimeline | null | undefined; open: boolean }) {
  const { t } = useT("orch");
  const p = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const phaseLabel = usePhaseLabel();
  const statusText = useStatusText();
  const updateIssue = useUpdateIssue();
  const setRisk = useOrchSetRisk(wsId, issue.id);
  const setMeta = useOrchSetMeta(wsId, issue.id);
  const { data: list } = useQuery({ ...orchIssuesOptions(wsId), enabled: open });
  const all = list?.issues;
  const v = timeline?.issue;
  const kids = (all ?? []).filter((i) => i.parent_id === issue.id);
  const parent = issue.parent_issue_id ? (all ?? []).find((i) => i.id === issue.parent_issue_id) : undefined;
  const none = t(($) => $.drawer.none);
  const flow = timeline?.flow ?? {};
  const meta = (issue.metadata ?? {}) as Record<string, unknown>;
  const accReq = ["true", "1", "yes"].includes(String(meta.acceptance_required ?? "").toLowerCase());
  const update = (u: Partial<UpdateIssueRequest>) =>
    updateIssue.mutate({ id: issue.id, ...u }, { onError: (e) => toast.error(e instanceof Error ? e.message : String(e)) });

  return (
    <div className="px-4 pb-6">
      <DescriptionEditor issue={issue} onSave={(description) => update({ description })} />
      <Section title={t(($) => $.drawer.properties)}>
        <Row label={t(($) => $.drawer.status)}>
          <span className="inline-flex items-center gap-1">
            <StatusIcon status={issue.status} className="size-3.5" />
            {statusText(issue.status)}
          </span>
        </Row>
        <Row label={t(($) => $.drawer.priority)}>
          <span data-testid="orch-drawer-priority"><PriorityPicker priority={issue.priority} onUpdate={update} align="start" /></span>
        </Row>
        <Row label={t(($) => $.drawer.risk)}>
          <span className="inline-flex flex-wrap gap-1" data-testid="orch-drawer-risk">
            {(["", ...RISKS] as const).map((r) => {
              const active = (v?.risk ?? "") === r;
              return (
                <button
                  key={r || "none"}
                  type="button"
                  onClick={() => setRisk.mutate(r ? r : null)}
                  className={cn(
                    "rounded-full border px-2 py-0.5 text-caption",
                    active ? "border-foreground bg-foreground text-background" : "border-border text-muted-foreground hover:text-foreground",
                  )}
                >
                  {r === "" ? t(($) => $.actions.risk_none) : r === "low" ? t(($) => $.actions.risk_low) : r === "medium" ? t(($) => $.actions.risk_medium) : t(($) => $.actions.risk_high)}
                </button>
              );
            })}
          </span>
        </Row>
        <Row label={t(($) => $.drawer.acceptance)}>
          <span className="flex items-center gap-2" data-testid="orch-drawer-acceptance">
            <Switch
              checked={accReq}
              onCheckedChange={(on) => setMeta.mutate({ key: "acceptance_required", value: on ? "true" : null })}
              aria-label={t(($) => $.drawer.acceptance)}
            />
            <span className="text-muted-foreground" title={t(($) => $.drawer.acceptance_hint)}>
              {accReq ? t(($) => $.drawer.acceptance_required) : t(($) => $.drawer.acceptance_auto)}
            </span>
          </span>
        </Row>
        <Row label={t(($) => $.drawer.assignee)}>{v?.assignee?.name || none}</Row>
        <Row label={t(($) => $.drawer.project)}>
          <span data-testid="orch-drawer-project"><ProjectPicker projectId={issue.project_id ?? null} onUpdate={update} /></span>
        </Row>
        <Row label={t(($) => $.drawer.labels)}>
          <span data-testid="orch-drawer-labels"><LabelPicker issueId={issue.id} /></span>
        </Row>
        <Row label={t(($) => $.drawer.parent)}>
          {parent ? (
            <AppLink href={p.issueDetail(parent.key)} className="hover:underline">
              {parent.key} {parent.title}
            </AppLink>
          ) : (
            none
          )}
        </Row>
        <Row label={t(($) => $.drawer.created)}>{timeAgo(issue.created_at)}</Row>
        <Row label={t(($) => $.drawer.updated)}>{timeAgo(issue.updated_at)}</Row>
      </Section>
      {v?.orchestrated && (
        <Section title={t(($) => $.drawer.flow)}>
          <Row label={t(($) => $.drawer.phase)}>{v.phase ? phaseLabel(v.phase) : none}</Row>
          <Row label={t(($) => $.drawer.owner)}>{flow.owner || none}</Row>
          {flow.expert && <Row label={t(($) => $.drawer.expert)}>{flow.expert}</Row>}
          <Row label={t(($) => $.drawer.verdict)}>{flow.verdict || none}</Row>
          <Row label={t(($) => $.drawer.head)}>
            <span className="font-mono">{shortSha(flow.head_sha) || none}</span>
          </Row>
          {v.worktree?.branch && (
            <Row label={t(($) => $.drawer.branch)}>
              <span className="font-mono text-micro">{v.worktree.branch}</span>
            </Row>
          )}
        </Section>
      )}
      {kids.length > 0 && (
        <Section title={t(($) => $.drawer.sub_issues)}>
          <ul className="flex flex-col gap-0.5">
            {kids.map((k) => (
              <li key={k.id} className="group/sub flex items-center gap-1">
                <AppLink href={p.issueDetail(k.key)} className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md px-1 py-0.5 text-caption hover:bg-muted">
                  <StatusIcon status={k.status} className="size-3.5 shrink-0" />
                  <span className="font-mono text-micro text-muted-foreground">{k.key}</span>
                  <span className="truncate">{k.title}</span>
                </AppLink>
                <OpenTabButton issueKey={k.key} title={k.title} className="opacity-0 group-hover/sub:opacity-100 focus-visible:opacity-100" />
              </li>
            ))}
          </ul>
        </Section>
      )}
    </div>
  );
}

function DescriptionEditor({ issue, onSave }: { issue: Issue; onSave: (md: string) => void }) {
  const { t } = useT("orch");
  const [editing, setEditing] = useState(false);
  const draft = useRef(issue.description ?? "");
  const save = () => {
    if (draft.current !== (issue.description ?? "")) {
      onSave(draft.current);
      toast.success(t(($) => $.drawer.saved));
    }
    setEditing(false);
  };
  return (
    <Section title={t(($) => $.drawer.description)} className="group/desc">
      {editing ? (
        <div className="flex flex-col gap-2" data-testid="orch-drawer-description-editor">
          <div className="max-h-[50vh] min-h-24 overflow-y-auto rounded-lg border border-border px-3 py-2 focus-within:border-ring">
            <ContentEditor
              defaultValue={issue.description ?? ""}
              placeholder={t(($) => $.drawer.add_description)}
              onUpdate={(md) => (draft.current = md)}
              onSubmit={save}
              debounceMs={50}
              currentIssueId={issue.id}
              flushPendingOnUnmount
            />
          </div>
          <div className="flex justify-end gap-1.5">
            <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>{t(($) => $.drawer.cancel)}</Button>
            <Button size="sm" onClick={save} data-testid="orch-drawer-description-save">{t(($) => $.drawer.save)}</Button>
          </div>
        </div>
      ) : (
        <div className="relative">
          <button
            type="button"
            onClick={() => {
              draft.current = issue.description ?? "";
              setEditing(true);
            }}
            className="absolute -top-7 right-0 inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-caption text-muted-foreground hover:bg-muted hover:text-foreground"
            data-testid="orch-drawer-description-edit"
          >
            <Pencil className="size-3" />
            {t(($) => $.drawer.edit)}
          </button>
          {issue.description ? (
            <ReadonlyContent content={issue.description} className="text-body" />
          ) : (
            <button type="button" onClick={() => setEditing(true)} className="text-caption text-faint-foreground hover:text-muted-foreground">
              {t(($) => $.drawer.add_description)}
            </button>
          )}
        </div>
      )}
    </Section>
  );
}

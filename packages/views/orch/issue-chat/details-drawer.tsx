"use client";

import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import type { Issue } from "@multica/core/types";
import { orchIssuesOptions, type OrchTimeline } from "@multica/core/orchestrator";
import { useWorkspacePaths } from "@multica/core/paths";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { ReadonlyContent } from "../../editor";
import { useT, useTimeAgo } from "../../i18n";
import { AppLink } from "../../navigation";
import { StatusIcon } from "../../issues/components/status-icon";
import { shortSha } from "../format";
import { usePhaseLabel } from "../labels";
import { OpenTabButton } from "../open-tab";
import { useStatusText } from "./timeline-parts";

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[7rem_1fr] items-baseline gap-2 py-1 text-caption">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 break-words">{children}</span>
    </div>
  );
}

// DetailsDrawer replaces the old right properties sidebar: description,
// properties, flow state, worktree and sub-issues. Editing beyond the fixed
// actions happens on the full page (?view=classic).
export function DetailsDrawer({ open, onOpenChange, wsId, issue, timeline }: { open: boolean; onOpenChange: (v: boolean) => void; wsId: string; issue: Issue; timeline: OrchTimeline | null | undefined }) {
  const { t } = useT("orch");
  const { t: ti } = useT("issues");
  const isMobile = useIsMobile();
  const p = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const phaseLabel = usePhaseLabel();
  const statusText = useStatusText();
  const { data: list } = useQuery({ ...orchIssuesOptions(wsId), enabled: open });
  const all = list?.issues;
  const v = timeline?.issue;
  const kids = (all ?? []).filter((i) => i.parent_id === issue.id);
  const parent = issue.parent_issue_id ? (all ?? []).find((i) => i.id === issue.parent_issue_id) : undefined;
  const none = t(($) => $.drawer.none);
  const flow = timeline?.flow ?? {};
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side={isMobile ? "bottom" : "right"}
        className={isMobile ? "max-h-[85vh] rounded-t-2xl" : "w-[28rem] sm:max-w-md"}
        data-testid="orch-details-drawer"
      >
        <SheetHeader className="border-b border-border/60 pb-3">
          <p className="font-mono text-micro text-muted-foreground">{issue.identifier}</p>
          <SheetTitle className="pr-8 text-title-sm">{issue.title}</SheetTitle>
          <AppLink href={`${p.issueDetail(issue.identifier)}?view=classic`} className="inline-flex w-fit items-center gap-1 text-caption text-muted-foreground hover:text-foreground">
            <ExternalLink className="size-3" />
            {t(($) => $.drawer.open_classic)}
          </AppLink>
        </SheetHeader>
        <div className="flex-1 overflow-y-auto px-4 pb-6">
          <section className="py-2">
            <h3 className="mb-1 text-micro font-medium uppercase tracking-wide text-muted-foreground">{t(($) => $.drawer.description)}</h3>
            {issue.description ? <ReadonlyContent content={issue.description} className="text-body" /> : <p className="text-caption text-muted-foreground">{none}</p>}
          </section>
          <section className="border-t border-border/60 py-2">
            <h3 className="mb-1 text-micro font-medium uppercase tracking-wide text-muted-foreground">{t(($) => $.drawer.properties)}</h3>
            <Row label={t(($) => $.drawer.status)}>
              <span className="inline-flex items-center gap-1">
                <StatusIcon status={issue.status} className="size-3.5" />
                {statusText(issue.status)}
              </span>
            </Row>
            <Row label={t(($) => $.drawer.priority)}>{ti(($) => $.priority[issue.priority])}</Row>
            <Row label={t(($) => $.drawer.assignee)}>{v?.assignee?.name || none}</Row>
            <Row label={t(($) => $.drawer.project)}>{v?.project?.title || none}</Row>
            <Row label={t(($) => $.drawer.labels)}>{v?.labels.length ? v.labels.join(", ") : none}</Row>
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
          </section>
          {v?.orchestrated && (
            <section className="border-t border-border/60 py-2">
              <h3 className="mb-1 text-micro font-medium uppercase tracking-wide text-muted-foreground">{t(($) => $.drawer.flow)}</h3>
              <Row label={t(($) => $.drawer.phase)}>{v.phase ? phaseLabel(v.phase) : none}</Row>
              <Row label={t(($) => $.drawer.owner)}>{flow.owner || none}</Row>
              <Row label={t(($) => $.drawer.verdict)}>{flow.verdict || none}</Row>
              <Row label={t(($) => $.drawer.head)}>
                <span className="font-mono">{shortSha(flow.head_sha) || none}</span>
              </Row>
            </section>
          )}
          {v?.worktree && (
            <section className="border-t border-border/60 py-2">
              <h3 className="mb-1 text-micro font-medium uppercase tracking-wide text-muted-foreground">{t(($) => $.chat.worktree)}</h3>
              <Row label={t(($) => $.drawer.branch)}>
                <span className="font-mono">{v.worktree.branch || none}</span>
              </Row>
              <Row label={t(($) => $.drawer.path)}>
                <span className="font-mono text-micro">{v.worktree.path || none}</span>
              </Row>
            </section>
          )}
          {kids.length > 0 && (
            <section className="border-t border-border/60 py-2">
              <h3 className="mb-1 text-micro font-medium uppercase tracking-wide text-muted-foreground">{t(($) => $.drawer.sub_issues)}</h3>
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
            </section>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

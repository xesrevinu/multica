"use client";

import { GitPullRequest, GitMerge, GitPullRequestClosed, GitPullRequestDraft, Loader2 } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { compactCount, hueOf, prStateTone, PR_STATE_TEXT, type PRStateTone } from "../format";

// DiffStat: "+9 −2" in diff colours, compact and tabular (t3code DiffStatLabel).
export function DiffStat({ additions, deletions, className }: { additions: number; deletions: number; className?: string }) {
  if (!additions && !deletions) return null;
  return (
    <span className={cn("inline-flex shrink-0 items-center gap-1 font-mono tabular-nums", className)} aria-label={`+${additions} −${deletions}`}>
      <span className="text-emerald-600 dark:text-emerald-400">+{compactCount(additions)}</span>
      {deletions > 0 && <span className="text-red-600 dark:text-red-400">−{compactCount(deletions)}</span>}
    </span>
  );
}

const PR_ICON: Record<PRStateTone, typeof GitPullRequest> = {
  open: GitPullRequest,
  merged: GitMerge,
  closed: GitPullRequestClosed,
  draft: GitPullRequestDraft,
};

export function PRStateIcon({ state, draft, className }: { state?: string; draft?: boolean; className?: string }) {
  const tone = prStateTone(state, draft);
  const Icon = PR_ICON[tone];
  return <Icon className={cn("size-3 shrink-0", PR_STATE_TEXT[tone], className)} aria-hidden />;
}

// PRChip: state icon + #number, the sidebar/header chip.
export function PRChip({ number, state, draft, className }: { number: number; state?: string; draft?: boolean; className?: string }) {
  return (
    <span className={cn("inline-flex shrink-0 items-center gap-0.5 tabular-nums", className)}>
      <PRStateIcon state={state} draft={draft} />
      <span>#{number}</span>
    </span>
  );
}

// ProjectBadge: the project's emoji, or a coloured monogram (t3code's
// project favicon slot). Renders a neutral dot when there is no project.
export function ProjectBadge({ title, icon, className }: { title?: string; icon?: string | null; className?: string }) {
  if (icon) {
    return (
      <span aria-hidden className={cn("inline-flex size-4 shrink-0 items-center justify-center text-[12px] leading-none", className)}>
        {icon}
      </span>
    );
  }
  if (!title) {
    return <span aria-hidden className={cn("inline-flex size-4 shrink-0 items-center justify-center", className)}><span className="size-1.5 rounded-full bg-border" /></span>;
  }
  const hue = hueOf(title);
  return (
    <span
      aria-hidden
      className={cn("inline-flex size-4 shrink-0 items-center justify-center rounded-[4px] text-[9px] font-semibold leading-none text-white uppercase", className)}
      style={{ backgroundColor: `oklch(0.62 0.14 ${hue})` }}
    >
      {title.trim().charAt(0)}
    </span>
  );
}

// WorkingLabel: spinner + "Working 26m" (t3code's live thread status).
export function WorkingLabel({ label, className }: { label: string; className?: string }) {
  return (
    <span className={cn("inline-flex shrink-0 items-center gap-1 text-sky-600 dark:text-sky-400", className)}>
      <Loader2 className="size-3 animate-spin" aria-hidden />
      <span className="tabular-nums">{label}</span>
    </span>
  );
}

// BranchChip: monospace branch pill.
export function BranchChip({ name, className }: { name: string; className?: string }) {
  return (
    <span className={cn("inline-flex max-w-full min-w-0 items-center rounded-md border border-border bg-muted/50 px-1.5 py-px font-mono text-micro text-muted-foreground", className)}>
      <span className="truncate">{name}</span>
    </span>
  );
}

"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Copy, ExternalLink, FolderGit2, GitBranch, Loader2, Lock, TerminalSquare, Trash2 } from "lucide-react";
import { orchWorktreesOptions, useOrchRemoveWorktree, type OrchWorktree } from "@multica/core/orchestrator";
import { useWorkspacePaths } from "@multica/core/paths";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import { DiffStat } from "../parts/chips";

function copy(text: string, done: string) {
  void navigator.clipboard?.writeText(text).then(() => toast.success(done));
}

// WorktreesTab: git worktrees on the team hubs (via wt layout) — this
// issue's by default, or all of them — with status / diff stat, open in
// editor, copy path and remove (refused for running issues; dirty needs a
// second confirmation).
export function WorktreesTab({ wsId, issueKey }: { wsId: string; issueKey: string }) {
  const { t } = useT("orch");
  const [scope, setScope] = useState<"issue" | "all">("issue");
  const { data, isLoading } = useQuery(orchWorktreesOptions(wsId, scope === "issue" ? issueKey : undefined));
  const remove = useOrchRemoveWorktree(wsId);
  const p = useWorkspacePaths();
  const onRemove = async (w: OrchWorktree) => {
    if (!window.confirm(t(($) => $.drawer.remove_confirm, { path: w.path }))) return;
    const force = w.dirty > 0 && window.confirm(t(($) => $.drawer.remove_dirty_confirm, { path: w.path }));
    if (w.dirty > 0 && !force) return;
    try {
      await remove.mutateAsync({ path: w.path, force });
      toast.success(t(($) => $.drawer.removed));
    } catch (err) {
      toast.error(t(($) => $.actions.failed, { error: err instanceof Error ? err.message : String(err) }));
    }
  };
  const hubsDown = (data?.hubs ?? []).filter((h) => !h.available);
  const list = data?.worktrees ?? [];
  return (
    <div className="flex flex-col gap-2 px-4 py-3" data-testid="orch-worktrees">
      <div className="flex items-center gap-1 text-caption">
        {(["issue", "all"] as const).map((s) => (
          <button
            key={s}
            type="button"
            onClick={() => setScope(s)}
            className={cn(
              "rounded-full px-2 py-0.5",
              scope === s ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground",
            )}
          >
            {s === "issue" ? t(($) => $.drawer.this_issue) : t(($) => $.drawer.all_worktrees)}
          </button>
        ))}
      </div>
      {hubsDown.length > 0 && (
        <p className="text-micro text-muted-foreground">
          {t(($) => $.drawer.hub_unavailable)}: {hubsDown.map((h) => h.project || h.hub).join(", ")}
        </p>
      )}
      {isLoading && <Skeleton className="h-16" />}
      {!isLoading && list.length === 0 && <p className="py-4 text-center text-caption text-muted-foreground">{t(($) => $.drawer.no_worktrees)}</p>}
      <ul className="flex flex-col gap-1.5">
        {list.map((w) => (
          <li key={w.path} className="rounded-lg border border-border px-3 py-2" data-testid="orch-worktree-row">
            <div className="flex items-center gap-1.5 text-caption">
              <GitBranch className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="min-w-0 truncate font-mono text-micro text-foreground">{w.branch || w.head?.slice(0, 7) || "—"}</span>
              {w.issue_key && (
                <AppLink href={p.issueDetail(w.issue_key)} className="shrink-0 font-mono text-micro text-muted-foreground hover:text-foreground">
                  {w.issue_key}
                </AppLink>
              )}
              {w.running && (
                <span className="inline-flex shrink-0 items-center gap-1 text-micro text-sky-700 dark:text-sky-300">
                  <Loader2 className="size-3 animate-spin" />
                  {t(($) => $.drawer.running)}
                </span>
              )}
              {w.locked && <Lock className="size-3 shrink-0 text-muted-foreground" />}
              <span className="ml-auto flex shrink-0 items-center gap-2 text-micro text-muted-foreground">
                {w.dirty > 0 && <span className="text-amber-700 dark:text-amber-300">{w.dirty} {t(($) => $.drawer.dirty)}</span>}
                {(w.ahead > 0 || w.behind > 0) && <span className="tabular-nums">{t(($) => $.drawer.ahead_behind, { ahead: w.ahead, behind: w.behind })}</span>}
                <DiffStat additions={w.additions} deletions={w.deletions} />
              </span>
            </div>
            {w.title && <p className="mt-0.5 truncate text-caption text-foreground">{w.title}</p>}
            <div className="mt-1 flex items-center gap-1">
              <span className="min-w-0 flex-1 truncate font-mono text-micro text-faint-foreground" title={w.path}>
                {w.path}
              </span>
              <a
                href={`vscode://file${w.path}`}
                className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
                title={t(($) => $.drawer.open_editor)}
                aria-label={t(($) => $.drawer.open_editor)}
              >
                <ExternalLink className="size-3.5" />
              </a>
              <button
                type="button"
                onClick={() => copy(w.path, t(($) => $.chat.copied))}
                className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
                title={t(($) => $.chat.copy_path)}
                aria-label={t(($) => $.chat.copy_path)}
              >
                <Copy className="size-3.5" />
              </button>
              <button
                type="button"
                onClick={() => void onRemove(w)}
                disabled={w.running || remove.isPending}
                className="rounded-md p-1 text-muted-foreground hover:bg-red-500/10 hover:text-red-600 disabled:opacity-40 disabled:hover:bg-transparent"
                title={t(($) => $.drawer.remove)}
                aria-label={t(($) => $.drawer.remove)}
                data-testid="orch-worktree-remove"
              >
                <Trash2 className="size-3.5" />
              </button>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

// TerminalTab: placeholder until an embedded terminal lands — shows the
// command to open a shell in the issue's worktree.
export function TerminalTab({ path }: { path?: string }) {
  const { t } = useT("orch");
  const cmd = path ? `cd ${JSON.stringify(path)}` : "wt list";
  return (
    <div className="flex flex-col gap-3 px-4 py-4" data-testid="orch-terminal">
      <div className="flex items-start gap-2 text-caption text-muted-foreground">
        <TerminalSquare className="mt-0.5 size-4 shrink-0" />
        <p>{t(($) => $.drawer.terminal_soon)}</p>
      </div>
      <div className="flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2">
        <FolderGit2 className="size-3.5 shrink-0 text-muted-foreground" />
        <code className="min-w-0 flex-1 truncate font-mono text-micro text-foreground">{cmd}</code>
        <button
          type="button"
          onClick={() => copy(cmd, t(($) => $.chat.copied))}
          className="rounded-md px-1.5 py-0.5 text-caption text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          {t(($) => $.drawer.copy)}
        </button>
      </div>
    </div>
  );
}

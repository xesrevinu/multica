"use client";

import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  ArrowDown,
  ArrowUp,
  Ban,
  CheckCircle2,
  Copy,
  ExternalLink,
  FileDiff,
  FolderGit2,
  PanelRight,
  Pause,
  Play,
  RefreshCw,
  RotateCcw,
} from "lucide-react";
import { inboxListOptions } from "@multica/core/inbox/queries";
import { useMarkInboxRead } from "@multica/core/inbox/mutations";
import { orchIssuesOptions, useOrchPROp, type OrchPRCard, type OrchTimeline } from "@multica/core/orchestrator";
import { useWorkspacePaths } from "@multica/core/paths";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { useRegisterOrchPalette, type OrchPaletteItem } from "../palette";
import type { DrawerTab } from "./details-drawer";
import { flowCardMode, useFlowAction } from "./flow-cards";

function typing(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  if (!el) return false;
  return el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName) || !!el.closest("[contenteditable=true],[role=dialog],[role=menu]");
}

// useIssueCommands wires the issue chat into ⌘K (every orchestrator action
// for this issue), single-key shortcuts (D details, F files, Alt+↑/↓ issue
// navigation) and clears the issue's unread inbox items on open.
export function useIssueCommands({
  wsId,
  issueId,
  issueKey,
  timeline,
  prCard,
  openDrawer,
}: {
  wsId: string;
  issueId: string;
  issueKey: string;
  timeline: OrchTimeline | null | undefined;
  prCard: OrchPRCard | null | undefined;
  openDrawer: (tab: DrawerTab) => void;
}) {
  const { t } = useT("orch");
  const nav = useNavigation();
  const paths = useWorkspacePaths();
  const { run } = useFlowAction(wsId, issueKey);
  const prOp = useOrchPROp(wsId, issueKey);
  const { data: list } = useQuery(orchIssuesOptions(wsId));
  const v = timeline?.issue;

  // Issue order for Alt+↑/↓: the open issues in attention order.
  const order = useMemo(() => (list?.issues ?? []).filter((i) => i.attention.bucket !== "settled").map((i) => i.key), [list]);
  const step = (dir: 1 | -1) => {
    if (order.length === 0) return;
    const at = order.indexOf(issueKey);
    const next = order[(at + dir + order.length) % order.length];
    if (next && next !== issueKey) nav.push(paths.issueDetail(next));
  };
  const stepRef = useRef(step);
  stepRef.current = step;
  const drawerRef = useRef(openDrawer);
  drawerRef.current = openDrawer;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || typing(e.target)) return;
      if (e.altKey && !e.metaKey && !e.ctrlKey && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
        e.preventDefault();
        stepRef.current(e.key === "ArrowDown" ? 1 : -1);
        return;
      }
      if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
      if (e.key === "d") {
        e.preventDefault();
        drawerRef.current("details");
      } else if (e.key === "f") {
        e.preventDefault();
        drawerRef.current("files");
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Opening the conversation reads its inbox notifications.
  const { data: inbox = [] } = useQuery(inboxListOptions(wsId));
  const markRead = useMarkInboxRead();
  const marked = useRef(new Set<string>());
  useEffect(() => {
    for (const item of inbox) {
      if (item.issue_id !== issueId || item.read || item.archived || marked.current.has(item.id)) continue;
      marked.current.add(item.id);
      markRead.mutate(item.id);
    }
  }, [inbox, issueId, markRead]);

  const items = useMemo<OrchPaletteItem[]>(() => {
    const out: OrchPaletteItem[] = [];
    const kw = [issueKey.toLowerCase(), "orchestrator", "flow"];
    const add = (key: string, label: string, icon: OrchPaletteItem["icon"], words: string[], onSelect: () => void) =>
      out.push({ key: `orch-${key}`, label, icon, keywords: [...words, ...kw], onSelect });
    add("details", t(($) => $.palette.open_details), PanelRight, ["details", "properties", "edit", "drawer"], () => openDrawer("details"));
    if (v?.pr) add("files", t(($) => $.palette.open_files), FileDiff, ["files", "diff", "changes"], () => openDrawer("files"));
    add("worktrees", t(($) => $.palette.open_worktrees), FolderGit2, ["worktree", "wt", "checkout", "remove"], () => openDrawer("worktrees"));
    const mode = flowCardMode(timeline);
    if (mode === "acceptance") add("accept", t(($) => $.palette.accept), CheckCircle2, ["accept", "merge", "approve", "ship"], () => void run("accept"));
    const settled = v?.status === "done" || v?.status === "cancelled";
    if (v?.orchestrated && !settled) {
      if (v.paused) add("resume", t(($) => $.palette.resume), Play, ["resume", "continue", "unpause"], () => void run("resume"));
      else add("pause", t(($) => $.palette.pause), Pause, ["pause", "stop", "hold"], () => void run("pause"));
      add("cancel", t(($) => $.palette.cancel), Ban, ["cancel", "abort", "drop"], () => {
        if (window.confirm(t(($) => $.actions.cancel_confirm, { key: issueKey }))) void run("cancel");
      });
    }
    const pr = prCard?.pr;
    const prOpen = pr && !pr.merged_at && pr.state === "open";
    const doOp = (op: "update-branch" | "rerun-checks") =>
      prOp.mutateAsync({ op, headSha: pr?.head_sha || undefined }).catch((err: unknown) => toast.error(t(($) => $.actions.failed, { error: err instanceof Error ? err.message : String(err) })));
    if (prOpen && prCard?.can.update_branch) add("update-branch", t(($) => $.palette.update_branch), RefreshCw, ["update", "branch", "merge", "rebase", "sync"], () => void doOp("update-branch"));
    if (prOpen && prCard?.can.rerun) add("rerun", t(($) => $.palette.rerun), RotateCcw, ["rerun", "retry", "checks", "ci"], () => void doOp("rerun-checks"));
    if (v?.pr?.url) add("open-pr", t(($) => $.palette.open_pr), ExternalLink, ["pr", "github", "pull"], () => window.open(v.pr!.url, "_blank", "noopener"));
    const branch = v?.worktree?.branch || v?.pr?.branch;
    if (branch)
      add("copy-branch", t(($) => $.palette.copy_branch), Copy, ["copy", "branch", "git"], () => {
        void navigator.clipboard?.writeText(branch).then(() => toast.success(t(($) => $.chat.copied)));
      });
    add("next", t(($) => $.palette.next_issue), ArrowDown, ["next", "down"], () => stepRef.current(1));
    add("prev", t(($) => $.palette.prev_issue), ArrowUp, ["previous", "up", "back"], () => stepRef.current(-1));
    return out;
    // run/prOp are re-created each render; the palette only needs a fresh
    // list when the issue state changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [issueKey, timeline, prCard, openDrawer, t]);
  useRegisterOrchPalette("issue-chat", items);
}

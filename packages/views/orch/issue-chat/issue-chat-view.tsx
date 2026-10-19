"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowDown, GitPullRequest, PanelRight } from "lucide-react";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions, issueTasksOptions } from "@multica/core/issues/queries";
import { orchTimelineOptions, useOrchRealtime } from "@multica/core/orchestrator";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { SidebarTrigger } from "@multica/ui/components/ui/sidebar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";
import { CommentCard, AgentRunComment } from "../../issues/components/comment-card";
import { CommentInput } from "../../issues/components/comment-input";
import { EMPTY_COMMENT_RUNS, buildCommentRunView, type CommentRun } from "../../issues/components/comment-runs";
import { CurrentIssueRenderContextProvider } from "../../issues/current-issue-render-context";
import { useIssueTimeline } from "../../issues/hooks/use-issue-timeline";
import { ReadonlyContent } from "../../editor";
import { checksTone, TONE_DOT } from "../format";
import { usePhaseLabel } from "../labels";
import { buildChatItems, type ChatItem } from "./build-items";
import { DetailsDrawer } from "./details-drawer";
import { FlowStateCard } from "./flow-cards";
import { PRCard } from "./pr-card";
import { StatusStrip } from "./status-strip";
import { ActivitiesFold, TransitionDivider, WorkedForFold } from "./timeline-parts";

const EMPTY_REPLIES: never[] = [];

// IssueChatView: the issue as a conversation (newest at the bottom), with
// the one pending decision pinned above a fixed composer and a status strip
// below it. Replaces the document-style IssueDetail on /issues/:id.
export function IssueChatView({ issueId }: { issueId: string }) {
  const wsId = useWorkspaceId();
  const { t } = useT("orch");
  const user = useAuthStore((s) => s.user);
  const phaseLabel = usePhaseLabel();
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  useOrchRealtime(wsId);

  const { data: issue, isLoading } = useQuery(issueDetailOptions(wsId, issueId));
  const key = issue?.identifier ?? "";
  const { data: orch } = useQuery(orchTimelineOptions(wsId, key));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const canModerate = useMemo(() => {
    const me = members.find((m) => m.user_id === user?.id);
    return me?.role === "owner" || me?.role === "admin";
  }, [members, user?.id]);

  const { timeline, loading, submitComment, submitReply, editComment, deleteComment, toggleResolveComment, toggleReaction } = useIssueTimeline(issueId, user?.id);
  const { data: tasks } = useQuery(issueTasksOptions(issueId));
  const prevRuns = useRef(new Map<string, CommentRun[]>());
  const runView = useMemo(() => {
    const next = buildCommentRunView(tasks ?? [], timeline, prevRuns.current);
    prevRuns.current = next.runs;
    return next;
  }, [tasks, timeline]);
  const model = useMemo(() => buildChatItems(runView.timeline, runView.standaloneRuns), [runView]);

  const [drawerOpen, setDrawerOpen] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const [atBottom, setAtBottom] = useState(true);
  const stick = useRef(true);
  const scrollToBottom = useCallback((smooth = false) => {
    const el = scrollRef.current;
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: smooth ? "smooth" : "auto" });
  }, []);
  // Keep the newest message in view while the user is at the bottom.
  useLayoutEffect(() => {
    if (stick.current) scrollToBottom();
  }, [model.items.length, loading, scrollToBottom]);
  useEffect(() => {
    stick.current = true;
    requestAnimationFrame(() => scrollToBottom());
  }, [issueId, scrollToBottom]);
  // Late content (PR card, run transcripts, images) grows the column after
  // the first paint; follow it while the reader is pinned to the bottom.
  useEffect(() => {
    const el = contentRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => {
      if (stick.current) scrollToBottom();
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, [scrollToBottom, isLoading]);
  const onScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    const near = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    stick.current = near;
    setAtBottom(near);
  };

  const renderContext = useMemo(() => (issue ? { id: issue.id, identifier: issue.identifier } : null), [issue]);

  if (isLoading || !issue) {
    return (
      <div className="flex h-full flex-col gap-3 p-6">
        <Skeleton className="h-6 w-1/2" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-4/5" />
      </div>
    );
  }

  const v = orch?.issue;
  const commonCommentProps = {
    issueId,
    currentUserId: user?.id,
    canModerate,
    onReply: submitReply,
    onReplyAccepted: () => scrollToBottom(true),
    onEdit: editComment,
    onDelete: deleteComment,
    onToggleReaction: toggleReaction,
    onResolveToggle: toggleResolveComment,
    enteringRunIds: undefined,
  };

  const renderItem = (item: ChatItem) => {
    switch (item.kind) {
      case "transition":
        return <TransitionDivider entry={item.entry} />;
      case "activities":
        return <ActivitiesFold entries={item.entries} />;
      case "comment":
        return (
          <CommentCard
            {...commonCommentProps}
            entry={item.entry}
            replies={model.threadReplies.get(item.id) ?? EMPTY_REPLIES}
            runs={runView.runs.get(item.id) ?? EMPTY_COMMENT_RUNS}
          />
        );
      case "run": {
        if (item.reply) {
          return (
            <AgentRunComment
              run={item.run}
              standalone
              commentProps={{
                ...commonCommentProps,
                entry: item.reply,
                replies: model.threadReplies.get(item.reply.id) ?? EMPTY_REPLIES,
                runs: runView.runs.get(item.reply.id) ?? EMPTY_COMMENT_RUNS,
              }}
            />
          );
        }
        const agentName = getActorName("agent", item.run.task.agent_id);
        return <WorkedForFold run={item.run} entering={false} agentName={agentName} />;
      }
    }
  };

  const prTone = v?.pr ? (v.pr.state === "merged" ? "ok" : checksTone(v.pr.checks)) : "none";
  return (
    <CurrentIssueRenderContextProvider value={renderContext}>
      <div className="relative flex h-full min-h-0 min-w-0 flex-1 flex-col bg-background" data-testid="orch-issue-chat">
        <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border/60 px-4">
          <SidebarTrigger className="-ml-1 md:hidden" />
          <button
            type="button"
            onClick={() => setDrawerOpen(true)}
            className="flex min-w-0 items-center gap-2 rounded-md px-1 py-0.5 text-left hover:bg-muted/60"
            data-testid="orch-title"
          >
            <span className="shrink-0 font-mono text-caption text-muted-foreground">{issue.identifier}</span>
            <span className="truncate text-body font-medium">{issue.title}</span>
          </button>
          <div className="ml-auto flex shrink-0 items-center gap-1.5">
            {v?.orchestrated && v.phase && (
              <span
                className={cn(
                  "hidden rounded-full border px-2 py-0.5 text-micro sm:inline",
                  v.attention.bucket === "needs_you" ? "border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-300" : "border-border text-muted-foreground",
                )}
              >
                {phaseLabel(v.phase)}
              </span>
            )}
            {v?.pr && (
              <a
                href={v.pr.url}
                target="_blank"
                rel="noreferrer"
                className="hidden items-center gap-1 rounded-full border border-border px-2 py-0.5 text-micro text-muted-foreground hover:text-foreground sm:inline-flex"
              >
                <GitPullRequest className="size-3" />#{v.pr.number}
                <span className={cn("size-1.5 rounded-full", TONE_DOT[prTone])} />
              </a>
            )}
            <button
              type="button"
              onClick={() => setDrawerOpen(true)}
              aria-label={t(($) => $.chat.details)}
              title={t(($) => $.chat.details)}
              className="rounded-md p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <PanelRight className="size-4" />
            </button>
          </div>
        </header>

        <div ref={scrollRef} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto" data-testid="orch-chat-scroll">
          <div ref={contentRef} className="mx-auto flex w-full max-w-3xl flex-col gap-2 px-4 py-6">
            {/* The issue description opens the conversation, like the first message. */}
            <div className="mb-2 rounded-2xl border border-border/60 bg-muted/30 p-4">
              <p className="mb-1 text-micro text-muted-foreground">
                {getActorName(issue.creator_type, issue.creator_id)} · {timeAgo(issue.created_at)}
              </p>
              <h1 className="text-title-sm font-semibold">{issue.title}</h1>
              {issue.description ? <ReadonlyContent content={issue.description} className="mt-2 text-body" /> : null}
            </div>
            {loading && <Skeleton className="h-16 w-full" />}
            {!loading && model.items.length === 0 && <p className="py-6 text-center text-caption text-muted-foreground">{t(($) => $.chat.empty)}</p>}
            {model.items.map((item) => (
              <div key={`${item.kind}-${item.id}`} id={item.kind === "comment" ? `comment-${item.id}` : undefined}>
                {renderItem(item)}
              </div>
            ))}
            {v?.pr && <PRCard wsId={wsId} issueKey={key} hasPR={!!v.pr} />}
          </div>
        </div>

        {!atBottom && (
          <button
            type="button"
            onClick={() => scrollToBottom(true)}
            className="absolute right-6 bottom-40 z-10 inline-flex items-center gap-1 rounded-full border border-border bg-background/95 px-2.5 py-1 text-caption text-muted-foreground shadow-sm hover:text-foreground"
          >
            <ArrowDown className="size-3" />
            {t(($) => $.chat.jump_latest)}
          </button>
        )}

        <footer className="shrink-0 border-t border-border/60 bg-background">
          <div className="mx-auto flex w-full max-w-3xl flex-col gap-2 px-4 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
            {v?.paused && <p className="rounded-md bg-muted px-2 py-1 text-caption text-muted-foreground">{t(($) => $.chat.paused_banner)}</p>}
            {orch && <FlowStateCard wsId={wsId} timeline={orch} />}
            <CommentInput key={issueId} issueId={issueId} onSubmit={submitComment} onAccepted={() => scrollToBottom(true)} onEditAnnotation={() => false} />
            <StatusStrip wsId={wsId} issue={issue} timeline={orch} />
          </div>
        </footer>
        <DetailsDrawer open={drawerOpen} onOpenChange={setDrawerOpen} wsId={wsId} issue={issue} timeline={orch} />
      </div>
    </CurrentIssueRenderContextProvider>
  );
}


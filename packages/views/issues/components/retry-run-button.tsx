"use client";

import { useState } from "react";
import { Loader2, RotateCcw, X } from "lucide-react";
import { api, dispatchReasonCode } from "@multica/core/api";
import type { AgentTask } from "@multica/core/types";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";

/** Retry only makes sense for terminal-but-not-success runs. */
export function canRetryRun(task: AgentTask): boolean {
  return task.status === "failed" || task.status === "cancelled";
}

// Re-runs one specific past run. Shared by the execution log's rows and the
// Runs dialog so a failed run retries the same way wherever it is listed.
export function RetryRunButton({
  task,
  issueId,
}: {
  task: AgentTask;
  issueId: string;
}) {
  const { t } = useT("issues");
  const [retrying, setRetrying] = useState(false);
  // Shown inline next to the button until dismissed or the next attempt: a
  // toast vanished before anyone could read why the retry was refused.
  const [error, setError] = useState<string | null>(null);

  const handleRetry = async () => {
    if (retrying) return;
    setRetrying(true);
    setError(null);
    try {
      // Passing task.id targets this row's agent — without it, the rerun
      // endpoint would fall back to the issue's current assignee and the wrong
      // agent would fire on rows whose agent has since been displaced
      // (reassignment, squad worker, or a one-off @-mention agent).
      await api.rerunIssue(issueId, task.id);
    } catch (e) {
      // A rerun is re-gated on the operator's invoke permission (MUL-4525): a
      // structured 403 means the agent can't be triggered, not a transient
      // failure — localize it instead of echoing the server's generic message.
      setError(
        dispatchReasonCode(e) === "invocation_not_allowed"
          ? t(($) => $.execution_log.retry_blocked)
          : e instanceof Error
            ? e.message
            : t(($) => $.execution_log.retry_failed),
      );
    } finally {
      // Reset on both success and failure: the row stays mounted (its task.id
      // is unchanged), so leaving `retrying` true on success would pin the
      // button as a permanent spinner.
      setRetrying(false);
    }
  };

  return (
    <span className="inline-flex min-w-0 items-center gap-1">
      {error && (
        <span
          role="alert"
          title={error}
          className="flex min-w-0 max-w-[28rem] items-center gap-1 rounded-xs bg-destructive/10 px-1.5 py-0.5 text-xs text-destructive"
        >
          <span className="truncate">{error}</span>
          <button
            type="button"
            onClick={() => setError(null)}
            aria-label={t(($) => $.execution_log.retry_dismiss)}
            className="shrink-0 rounded-xs hover:bg-destructive/20"
          >
            <X className="h-3 w-3" />
          </button>
        </span>
      )}
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              onClick={handleRetry}
              disabled={retrying}
              aria-label={t(($) => $.execution_log.retry_task_aria)}
            />
          }
          className="flex items-center justify-center rounded-xs p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
        >
          {retrying ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <RotateCcw className="h-3.5 w-3.5" />
          )}
        </TooltipTrigger>
        <TooltipContent>
          {t(($) => $.execution_log.retry_task_tooltip)}
        </TooltipContent>
      </Tooltip>
    </span>
  );
}

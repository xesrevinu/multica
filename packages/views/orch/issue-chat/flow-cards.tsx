"use client";

import { useState } from "react";
import { toast } from "sonner";
import { AlertTriangle, CircleCheck, OctagonX } from "lucide-react";
import { useOrchFlowAction, type OrchFlowAction, type OrchTimeline } from "@multica/core/orchestrator";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { shortSha } from "../format";

export function useFlowAction(wsId: string, key: string) {
  const { t } = useT("orch");
  const m = useOrchFlowAction(wsId, key);
  const run = (action: OrchFlowAction, reason?: string) =>
    m.mutateAsync({ action, reason }).then(
      () => true,
      (err: unknown) => {
        toast.error(t(($) => $.actions.failed, { error: err instanceof Error ? err.message : String(err) }));
        return false;
      },
    );
  return { run, pending: m.isPending };
}

type Mode = "acceptance" | "blocked" | "cancel_proposed" | null;

export function flowCardMode(tl: OrchTimeline | null | undefined): Mode {
  const i = tl?.issue;
  if (!i?.orchestrated || i.status === "done" || i.status === "cancelled") return null;
  if (i.phase === "awaiting_acceptance") return "acceptance";
  if (i.phase === "cancel_proposed") return "cancel_proposed";
  if (i.status === "blocked") return "blocked";
  return null;
}

// FlowStateCard: the one decision the issue is waiting on, pinned above the
// composer — accept / reject with reason, unblock with a note, or cancel.
export function FlowStateCard({ wsId, timeline }: { wsId: string; timeline: OrchTimeline }) {
  const { t } = useT("orch");
  const mode = flowCardMode(timeline);
  const key = timeline.issue.key;
  const { run, pending } = useFlowAction(wsId, key);
  const [form, setForm] = useState<null | "reject" | "unblock" | "cancel">(null);
  const [text, setText] = useState("");
  if (!mode) return null;
  const flow = timeline.flow;
  const tone = mode === "acceptance" ? "border-amber-500/40 bg-amber-500/5" : "border-red-500/30 bg-red-500/5";
  const Icon = mode === "acceptance" ? CircleCheck : mode === "blocked" ? OctagonX : AlertTriangle;
  const submit = async (action: OrchFlowAction, reason?: string) => {
    if (await run(action, reason)) {
      setForm(null);
      setText("");
    }
  };
  return (
    <div className={cn("rounded-xl border p-3", tone)} data-testid="orch-flow-card" data-mode={mode}>
      <div className="flex items-start gap-2">
        <Icon className={cn("mt-0.5 size-4 shrink-0", mode === "acceptance" ? "text-amber-600" : "text-red-600")} />
        <div className="min-w-0 flex-1">
          <p className="text-body font-medium">
            {mode === "acceptance"
              ? t(($) => $.cards.acceptance_title)
              : mode === "blocked"
                ? t(($) => $.cards.blocked_title)
                : t(($) => $.cards.cancel_proposed_title)}
          </p>
          <p className="mt-0.5 text-caption text-muted-foreground">
            {mode === "acceptance"
              ? t(($) => $.cards.acceptance_body, { sha: shortSha(flow.reviewed_sha || flow.head_sha) || "—" })
              : flow.blocked_reason || timeline.waiting || ""}
          </p>
        </div>
      </div>
      {form && (
        <textarea
          autoFocus
          value={text}
          onChange={(e) => setText(e.target.value)}
          rows={2}
          placeholder={
            form === "reject" ? t(($) => $.actions.reject_placeholder) : form === "unblock" ? t(($) => $.actions.unblock_placeholder) : t(($) => $.actions.cancel_confirm, { key })
          }
          aria-label={form === "reject" ? t(($) => $.actions.reject_placeholder) : t(($) => $.actions.unblock_placeholder)}
          readOnly={form === "cancel"}
          className="mt-2 w-full resize-none rounded-md border border-border bg-background px-2 py-1.5 text-body outline-none focus:border-ring"
        />
      )}
      <div className="mt-2 flex flex-wrap items-center justify-end gap-1.5">
        {form ? (
          <>
            <Button size="sm" variant="ghost" onClick={() => setForm(null)} disabled={pending}>
              {t(($) => $.actions.keep)}
            </Button>
            {form === "reject" && (
              <Button size="sm" variant="destructive" disabled={pending || !text.trim()} onClick={() => submit("reject", text.trim())}>
                {t(($) => $.actions.reject_submit)}
              </Button>
            )}
            {form === "unblock" && (
              <Button size="sm" disabled={pending} onClick={() => submit("unblock", text.trim() || undefined)}>
                {t(($) => $.actions.unblock_submit)}
              </Button>
            )}
            {form === "cancel" && (
              <Button size="sm" variant="destructive" disabled={pending} onClick={() => submit("cancel")}>
                {t(($) => $.actions.cancel_submit)}
              </Button>
            )}
          </>
        ) : mode === "acceptance" ? (
          <>
            <Button size="sm" variant="outline" onClick={() => setForm("reject")} disabled={pending}>
              {t(($) => $.actions.reject)}
            </Button>
            <Button size="sm" onClick={() => submit("accept")} disabled={pending} data-testid="orch-accept">
              {t(($) => $.actions.accept)}
            </Button>
          </>
        ) : (
          <>
            <Button size="sm" variant="outline" onClick={() => setForm("cancel")} disabled={pending}>
              {t(($) => $.actions.cancel)}
            </Button>
            <Button size="sm" onClick={() => setForm("unblock")} disabled={pending}>
              {t(($) => $.actions.unblock)}
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

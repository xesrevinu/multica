"use client";

import { useEffect, useRef } from "react";
import { toast } from "sonner";
import type { OrchIssue } from "@multica/core/orchestrator";
import { useT } from "../i18n";

// Notify when an issue newly lands in "needs you": an in-app toast, plus a
// desktop notification when the page is hidden and permission is granted.
// The first list seen is the baseline, so a reload does not replay alerts.
export function useOrchNeedsYouNotify(issues: OrchIssue[] | undefined, open: (key: string) => void) {
  const { t } = useT("orch");
  const seen = useRef<Set<string> | null>(null);
  useEffect(() => {
    if (!issues) return;
    const now = new Set(issues.filter((i) => i.attention.bucket === "needs_you").map((i) => i.id));
    const prev = seen.current;
    seen.current = now;
    if (!prev) return;
    for (const i of issues) {
      if (!now.has(i.id) || prev.has(i.id)) continue;
      const title = t(($) => $.notify.needs_you_title, { key: i.key });
      toast(title, { description: i.title, action: { label: i.key, onClick: () => open(i.key) } });
      if (typeof document !== "undefined" && document.hidden && typeof Notification !== "undefined" && Notification.permission === "granted") {
        try {
          const n = new Notification(title, { body: i.title, tag: `orch-${i.id}` });
          n.onclick = () => {
            window.focus();
            open(i.key);
            n.close();
          };
        } catch {
          /* some browsers only allow notifications from a service worker */
        }
      }
    }
  }, [issues, open, t]);
}

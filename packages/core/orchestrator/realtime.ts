"use client";

import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useWS } from "../realtime/provider";
import type { WSEventType } from "../types";
import { orchKeys } from "./queries";

const EVENTS: WSEventType[] = [
  "issue:created",
  "issue:updated",
  "issue:deleted",
  "issue_metadata:changed",
  "issue_labels:changed",
  "task:queued",
  "task:running",
  "task:completed",
  "task:failed",
  "task:cancelled",
  "activity:created",
  "pull_request:linked",
  "pull_request:updated",
  "pull_request:unlinked",
];

// useOrchRealtime refreshes the orchestrator views (debounced) whenever an
// issue, run or PR in the workspace changes.
export function useOrchRealtime(wsId: string) {
  const qc = useQueryClient();
  const { subscribe } = useWS();
  useEffect(() => {
    if (!wsId) return;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const kick = () => {
      if (timer) return;
      timer = setTimeout(() => {
        timer = null;
        void qc.invalidateQueries({ queryKey: orchKeys.all(wsId) });
      }, 400);
    };
    const unsubs = EVENTS.map((e) => subscribe(e, kick));
    return () => {
      unsubs.forEach((u) => u());
      if (timer) clearTimeout(timer);
    };
  }, [qc, subscribe, wsId]);
}

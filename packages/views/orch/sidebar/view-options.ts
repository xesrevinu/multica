"use client";

import { useCallback, useEffect, useState } from "react";
import { DEFAULT_ORCH_VIEW, type OrchViewOptions } from "@multica/core/orchestrator";

const keyOf = (wsId: string) => `multica.orch.sidebar-view.${wsId}`;

function read(wsId: string): OrchViewOptions & { project: string } {
  const fallback = { ...DEFAULT_ORCH_VIEW, project: "" };
  if (typeof window === "undefined" || !wsId) return fallback;
  try {
    const raw = window.localStorage.getItem(keyOf(wsId));
    if (!raw) return fallback;
    const v = JSON.parse(raw) as Partial<OrchViewOptions & { project: string }>;
    return { ...fallback, ...v, priorities: Array.isArray(v.priorities) ? v.priorities : [] };
  } catch {
    return fallback;
  }
}

// Sidebar view options, persisted per workspace in localStorage.
export function useOrchViewOptions(wsId: string) {
  const [view, setView] = useState(() => read(wsId));
  useEffect(() => setView(read(wsId)), [wsId]);
  const update = useCallback(
    (patch: Partial<OrchViewOptions & { project: string }>) => {
      setView((prev) => {
        const next = { ...prev, ...patch };
        try {
          window.localStorage.setItem(keyOf(wsId), JSON.stringify(next));
        } catch {
          /* storage full or disabled: keep in memory */
        }
        return next;
      });
    },
    [wsId],
  );
  const reset = useCallback(() => update({ ...DEFAULT_ORCH_VIEW, project: "" }), [update]);
  return { view, update, reset };
}

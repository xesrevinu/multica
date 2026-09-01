"use client";

import { queryOptions, useMutation } from "@tanstack/react-query";
import { api } from "../api";
import type { PtySession, WorkspaceTerminalState } from "./session-store";

export function emptyTerminalWorkspaceState(): WorkspaceTerminalState {
  return { sessions: [], activeSessionId: null, sidebarCollapsed: false };
}

export function normalizeTerminalWorkspaceState(raw: unknown): WorkspaceTerminalState {
  if (!raw || typeof raw !== "object") return emptyTerminalWorkspaceState();
  const value = raw as Record<string, unknown>;
  if (!Array.isArray(value.sessions)) return emptyTerminalWorkspaceState();
  return {
    sessions: value.sessions as PtySession[],
    activeSessionId: typeof value.activeSessionId === "string" ? value.activeSessionId : null,
    sidebarCollapsed: value.sidebarCollapsed === true,
  };
}

export const terminalWorkspaceKeys = {
  all: (wsId: string) => ["terminal-workspace-state", wsId] as const,
};

export function terminalWorkspaceStateOptions(wsId: string) {
  return queryOptions({
    queryKey: terminalWorkspaceKeys.all(wsId),
    queryFn: async () => {
      const raw = await api.getTerminalWorkspaceState();
      return normalizeTerminalWorkspaceState(raw.state);
    },
    enabled: !!wsId,
  });
}

export function usePersistTerminalWorkspaceState(wsId: string) {
  return useMutation({
    mutationFn: (state: WorkspaceTerminalState) => api.putTerminalWorkspaceState(state),
  });
}

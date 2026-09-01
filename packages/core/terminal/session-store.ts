"use client";

import { create } from "zustand";
import {
  cloneLayout,
  closeLeaf,
  collectLeafIds,
  equalizeTree,
  setSplitRatio as setNodeSplitRatio,
  splitLeaf,
  type PtyLayoutNode,
  type PtySplitDirection,
} from "./layout";

export type PtyPaneKind = "idle" | "shell" | "agent";

export interface PtyPane {
  id: string;
  kind: PtyPaneKind;
  title: string;
  projectId: string | null;
  daemonId: string | null;
  agentId: string | null;
  argv: string[];
  cwd: string;
}

export interface PtySession {
  id: string;
  title: string;
  createdAt: number;
  layout: PtyLayoutNode;
  activeLeafId: string;
  expandedLeafId: string | null;
  panes: Record<string, PtyPane>;
}

export interface WorkspaceTerminalState {
  sessions: PtySession[];
  activeSessionId: string | null;
  sidebarCollapsed: boolean;
}

interface TerminalSessionState {
  byWorkspace: Record<string, WorkspaceTerminalState>;
  hydrateWorkspace: (wsId: string, snapshot: WorkspaceTerminalState) => void;
  createSession: (wsId: string, title: string, pane?: Partial<PtyPane>) => string;
  closeSession: (wsId: string, sessionId: string) => void;
  setActiveSession: (wsId: string, sessionId: string | null) => void;
  setSidebarCollapsed: (wsId: string, collapsed: boolean) => void;
  splitPane: (wsId: string, sessionId: string, leafId: string, direction: PtySplitDirection) => void;
  closePane: (wsId: string, sessionId: string, leafId: string) => void;
  updatePane: (wsId: string, sessionId: string, leafId: string, patch: Partial<PtyPane>) => void;
  setActiveLeaf: (wsId: string, sessionId: string, leafId: string) => void;
  setExpandedLeaf: (wsId: string, sessionId: string, leafId: string | null) => void;
  setSplitRatio: (wsId: string, sessionId: string, splitId: string, ratio: number) => void;
  equalizePane: (wsId: string, sessionId: string, leafId: string) => void;
  renameSession: (wsId: string, sessionId: string, title: string) => void;
  duplicateSession: (wsId: string, sessionId: string) => string | null;
}

const EMPTY: WorkspaceTerminalState = {
  sessions: [],
  activeSessionId: null,
  sidebarCollapsed: false,
};

function newId(): string {
  return crypto.randomUUID();
}

function workspaceOf(
  state: TerminalSessionState,
  wsId: string,
): WorkspaceTerminalState {
  return state.byWorkspace[wsId] ?? EMPTY;
}

function idlePane(id: string, title: string, extras?: Partial<PtyPane>): PtyPane {
  return {
    id,
    kind: extras?.kind ?? "idle",
    title,
    projectId: extras?.projectId ?? null,
    daemonId: extras?.daemonId ?? null,
    agentId: extras?.agentId ?? null,
    argv: extras?.argv ?? [],
    cwd: extras?.cwd ?? "",
  };
}

export const useTerminalSessionStore = create<TerminalSessionState>((set, get) => ({
  byWorkspace: {},

  hydrateWorkspace: (wsId, snapshot) => {
    set((state) => ({
      byWorkspace: {
        ...state.byWorkspace,
        [wsId]: {
          sessions: snapshot.sessions ?? [],
          activeSessionId: snapshot.activeSessionId ?? null,
          sidebarCollapsed: snapshot.sidebarCollapsed ?? false,
        },
      },
    }));
  },

  createSession: (wsId, title, pane) => {
    const leafId = newId();
    const session: PtySession = {
      id: newId(),
      title,
      createdAt: Date.now(),
      layout: { type: "leaf", id: leafId },
      activeLeafId: leafId,
      expandedLeafId: null,
      panes: { [leafId]: idlePane(leafId, title, pane) },
    };
    set((state) => {
      const current = workspaceOf(state, wsId);
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: {
            ...current,
            sessions: [session, ...current.sessions],
            activeSessionId: session.id,
          },
        },
      };
    });
    return session.id;
  },

  closeSession: (wsId, sessionId) => {
    set((state) => {
      const current = workspaceOf(state, wsId);
      const sessions = current.sessions.filter((session) => session.id !== sessionId);
      const activeSessionId =
        current.activeSessionId === sessionId
          ? (sessions[0]?.id ?? null)
          : current.activeSessionId;
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: { ...current, sessions, activeSessionId },
        },
      };
    });
  },

  setActiveSession: (wsId, sessionId) => {
    set((state) => {
      const current = workspaceOf(state, wsId);
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: { ...current, activeSessionId: sessionId },
        },
      };
    });
  },

  setSidebarCollapsed: (wsId, collapsed) => {
    set((state) => {
      const current = workspaceOf(state, wsId);
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: { ...current, sidebarCollapsed: collapsed },
        },
      };
    });
  },

  splitPane: (wsId, sessionId, leafId, direction) => {
    set((state) => {
      const current = workspaceOf(state, wsId);
      const session = current.sessions.find((item) => item.id === sessionId);
      if (!session) return state;
      const source = session.panes[leafId];
      if (!source) return state;
      const newLeafId = newId();
      const layout = splitLeaf(session.layout, leafId, direction, newLeafId, newId());
      const pane = idlePane(newLeafId, source.title, {
        ...source,
        id: newLeafId,
        kind: source.kind === "idle" ? "idle" : "shell",
        agentId: null,
        argv: [],
      });
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: {
            ...current,
            sessions: current.sessions.map((item) =>
              item.id === sessionId
                ? {
                    ...item,
                    layout,
                    activeLeafId: newLeafId,
                    expandedLeafId: null,
                    panes: { ...item.panes, [newLeafId]: pane },
                  }
                : item,
            ),
          },
        },
      };
    });
  },

  closePane: (wsId, sessionId, leafId) => {
    const current = workspaceOf(get(), wsId);
    const session = current.sessions.find((item) => item.id === sessionId);
    if (!session) return;
    const remaining = collectLeafIds(session.layout).filter((id) => id !== leafId);
    if (remaining.length === 0) {
      get().closeSession(wsId, sessionId);
      return;
    }
    set((state) => {
      const next = workspaceOf(state, wsId);
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: {
            ...next,
            sessions: next.sessions.map((item) => {
              if (item.id !== sessionId) return item;
              const layout = closeLeaf(item.layout, leafId);
              if (!layout) return item;
              const { [leafId]: _removed, ...panes } = item.panes;
              return {
                ...item,
                layout,
                panes,
                activeLeafId:
                  item.activeLeafId === leafId ? remaining[0] : item.activeLeafId,
                expandedLeafId:
                  item.expandedLeafId && remaining.includes(item.expandedLeafId)
                    ? item.expandedLeafId
                    : null,
              };
            }),
          },
        },
      };
    });
  },

  updatePane: (wsId, sessionId, leafId, patch) => {
    set((state) => {
      const current = workspaceOf(state, wsId);
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: {
            ...current,
            sessions: current.sessions.map((session) => {
              if (session.id !== sessionId) return session;
              const pane = session.panes[leafId];
              if (!pane) return session;
              const nextPane = { ...pane, ...patch, id: leafId };
              const title =
                session.layout.type === "leaf" && patch.title
                  ? patch.title
                  : session.title;
              return {
                ...session,
                title,
                panes: { ...session.panes, [leafId]: nextPane },
              };
            }),
          },
        },
      };
    });
  },

  setActiveLeaf: (wsId, sessionId, leafId) => {
    set((state) => {
      const current = workspaceOf(state, wsId);
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: {
            ...current,
            sessions: current.sessions.map((session) =>
              session.id === sessionId ? { ...session, activeLeafId: leafId } : session,
            ),
          },
        },
      };
    });
  },

  setSplitRatio: (wsId, sessionId, splitId, ratio) => {
    set((state) => {
      const current = workspaceOf(state, wsId);
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: {
            ...current,
            sessions: current.sessions.map((session) =>
              session.id === sessionId
                ? { ...session, layout: setNodeSplitRatio(session.layout, splitId, ratio) }
                : session,
            ),
          },
        },
      };
    });
  },

  equalizePane: (wsId, sessionId, _leafId) => {
    set((state) => {
      const current = workspaceOf(state, wsId);
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: {
            ...current,
            sessions: current.sessions.map((session) =>
              session.id === sessionId
                ? { ...session, layout: equalizeTree(session.layout) }
                : session,
            ),
          },
        },
      };
    });
  },

  setExpandedLeaf: (wsId, sessionId, leafId) => {
    set((state) => {
      const current = workspaceOf(state, wsId);
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: {
            ...current,
            sessions: current.sessions.map((session) =>
              session.id === sessionId ? { ...session, expandedLeafId: leafId } : session,
            ),
          },
        },
      };
    });
  },

  duplicateSession: (wsId, sessionId) => {
    const current = workspaceOf(get(), wsId);
    const source = current.sessions.find((session) => session.id === sessionId);
    if (!source) return null;
    const { node, idMap } = cloneLayout(source.layout);
    const panes: Record<string, PtyPane> = {};
    for (const [oldId, pane] of Object.entries(source.panes)) {
      const nextId = idMap[oldId];
      if (!nextId) continue;
      panes[nextId] = { ...pane, id: nextId };
    }
    const activeLeafId = idMap[source.activeLeafId] ?? collectLeafIds(node)[0];
    const session: PtySession = {
      id: newId(),
      title: source.title,
      createdAt: Date.now(),
      layout: node,
      activeLeafId,
      expandedLeafId: source.expandedLeafId ? (idMap[source.expandedLeafId] ?? null) : null,
      panes,
    };
    set((state) => {
      const next = workspaceOf(state, wsId);
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: {
            ...next,
            sessions: [session, ...next.sessions],
            activeSessionId: session.id,
          },
        },
      };
    });
    return session.id;
  },

  renameSession: (wsId, sessionId, title) => {
    set((state) => {
      const current = workspaceOf(state, wsId);
      return {
        byWorkspace: {
          ...state.byWorkspace,
          [wsId]: {
            ...current,
            sessions: current.sessions.map((session) =>
              session.id === sessionId ? { ...session, title } : session,
            ),
          },
        },
      };
    });
  },
}));

export function selectWorkspaceTerminal(
  state: TerminalSessionState,
  wsId: string,
): WorkspaceTerminalState {
  return state.byWorkspace[wsId] ?? EMPTY;
}

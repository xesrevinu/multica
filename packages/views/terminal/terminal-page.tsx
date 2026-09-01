"use client";

import { useCallback, useEffect, useMemo, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useRequiredWorkspaceSlug } from "@multica/core/paths";
import { projectListOptions, projectResourcesOptions } from "@multica/core/projects";
import { runtimeListOptions, runtimeProfileListOptions } from "@multica/core/runtimes";
import {
  adjacentLeaf,
  collectLeafIds,
  selectWorkspaceTerminal,
  terminalWorkspaceStateOptions,
  usePersistTerminalWorkspaceState,
  useTerminalSessionStore,
} from "@multica/core/terminal";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { AgentRuntime, ProjectResource } from "@multica/core/types";
import { getShortcutPlatform, isEditableShortcutTarget } from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@multica/ui/components/ui/resizable";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import { useT } from "../i18n";
import { buildRuntimeMachines } from "../runtimes/components/runtime-machines";
import { TerminalLauncher, type TerminalLauncherRow } from "./terminal-launcher";
import { TerminalNewMenu } from "./terminal-commands";
import { TerminalSessionRail } from "./session-rail";
import { TerminalSplitLayout } from "./split-layout";
import {
  cliArgvForRuntime,
  localPathForDaemon,
  onlineAgentsForDaemon,
} from "./session";
import { closePtyProcess, closePtyProcesses } from "./pty-lifetime";

export function TerminalPage() {
  const { t } = useT("layout");
  const wsId = useWorkspaceId();
  const slug = useRequiredWorkspaceSlug();
  const queryClient = useQueryClient();
  const isCompact = useIsCompact();

  const terminalState = useTerminalSessionStore((state) => selectWorkspaceTerminal(state, wsId));
  const createSession = useTerminalSessionStore((state) => state.createSession);
  const closeSession = useTerminalSessionStore((state) => state.closeSession);
  const setActiveSession = useTerminalSessionStore((state) => state.setActiveSession);
  const setSidebarCollapsed = useTerminalSessionStore((state) => state.setSidebarCollapsed);
  const splitPane = useTerminalSessionStore((state) => state.splitPane);
  const closePane = useTerminalSessionStore((state) => state.closePane);
  const updatePane = useTerminalSessionStore((state) => state.updatePane);
  const setActiveLeaf = useTerminalSessionStore((state) => state.setActiveLeaf);
  const setExpandedLeaf = useTerminalSessionStore((state) => state.setExpandedLeaf);
  const renameSession = useTerminalSessionStore((state) => state.renameSession);
  const setSplitRatio = useTerminalSessionStore((state) => state.setSplitRatio);
  const equalizePane = useTerminalSessionStore((state) => state.equalizePane);
  const duplicateSession = useTerminalSessionStore((state) => state.duplicateSession);
  const hydrateWorkspace = useTerminalSessionStore((state) => state.hydrateWorkspace);

  const { sessions, activeSessionId, sidebarCollapsed } = terminalState;
  const { data: savedState, isSuccess: stateReady } = useQuery(terminalWorkspaceStateOptions(wsId));
  const persistState = usePersistTerminalWorkspaceState(wsId);
  const persistMutate = persistState.mutate;
  const skipPersist = useRef(true);
  const activeSession = sessions.find((session) => session.id === activeSessionId) ?? null;

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: profiles = [] } = useQuery(runtimeProfileListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const machines = useMemo(
    () =>
      buildRuntimeMachines(runtimes, { now: Date.now() }).filter(
        (machine) => machine.health === "online" && machine.daemonId,
      ),
    [runtimes],
  );
  const runtimeById = useMemo(() => {
    const map = new Map<string, AgentRuntime>();
    for (const runtime of runtimes) map.set(runtime.id, runtime);
    return map;
  }, [runtimes]);

  const defaultDaemonId = machines[0]?.daemonId ?? null;
  const didBoot = useRef(false);
  const wasCompact = useRef(isCompact);

  useEffect(() => {
    if (isCompact && !wasCompact.current && activeSessionId) {
      setSidebarCollapsed(wsId, true);
    }
    wasCompact.current = isCompact;
  }, [activeSessionId, isCompact, setSidebarCollapsed, wsId]);

  useEffect(() => {
    if (!stateReady || !savedState) return;
    hydrateWorkspace(wsId, savedState);
    skipPersist.current = false;
  }, [hydrateWorkspace, savedState, stateReady, wsId]);

  useEffect(() => {
    let timer: number | null = null;
    const unsub = useTerminalSessionStore.subscribe((next, prev) => {
      if (skipPersist.current) return;
      if (next.byWorkspace[wsId] === prev.byWorkspace[wsId]) return;
      if (timer !== null) window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        persistMutate(selectWorkspaceTerminal(useTerminalSessionStore.getState(), wsId));
      }, 400);
    });
    return () => {
      unsub();
      if (timer !== null) window.clearTimeout(timer);
    };
  }, [persistMutate, wsId]);

  useEffect(() => {
    if (!stateReady) return;
    if (didBoot.current) return;
    didBoot.current = true;
    if ((savedState?.sessions.length ?? 0) > 0) return;
    if (!defaultDaemonId) return;
    createSession(wsId, t(($) => $.terminal.home), {
      kind: "shell",
      daemonId: defaultDaemonId,
      argv: [],
    });
  }, [createSession, defaultDaemonId, savedState, stateReady, t, wsId]);
  const machineAgents = useMemo(
    () => onlineAgentsForDaemon(agents, runtimeById, defaultDaemonId),
    [agents, runtimeById, defaultDaemonId],
  );

  const resourcesForProject = useCallback(
    async (id: string | null): Promise<ProjectResource[] | undefined> => {
      if (!id) return undefined;
      return queryClient.fetchQuery(projectResourcesOptions(wsId, id));
    },
    [queryClient, wsId],
  );

  const assignPaneTarget = useCallback(
    async (
      sessionId: string,
      leafId: string,
      next: {
        kind: "shell" | "agent";
        projectId: string | null;
        daemonId: string;
        argv: string[];
        title: string;
      },
    ) => {
      const resources = await resourcesForProject(next.projectId);
      updatePane(wsId, sessionId, leafId, {
        kind: next.kind,
        title: next.title,
        projectId: next.projectId,
        daemonId: next.daemonId,
        argv: next.argv,
        cwd: localPathForDaemon(resources, next.daemonId),
      });
    },
    [resourcesForProject, updatePane, wsId],
  );

  const openShellInPane = useCallback(
    async (sessionId: string, leafId: string, projectId: string | null) => {
      if (!defaultDaemonId) return;
      const project = projects.find((item) => item.id === projectId);
      const title = project?.title ?? t(($) => $.terminal.home);
      await assignPaneTarget(sessionId, leafId, {
        kind: "shell",
        projectId,
        daemonId: defaultDaemonId,
        argv: [],
        title,
      });
    },
    [assignPaneTarget, defaultDaemonId, projects, t],
  );

  const startAgentInPane = useCallback(
    async (sessionId: string, leafId: string, projectId: string | null, agentId: string) => {
      const agent = agents.find((item) => item.id === agentId);
      const runtime = agent ? runtimeById.get(agent.runtime_id) : undefined;
      if (!agent || !runtime?.daemon_id) return;
      const argv = cliArgvForRuntime(runtime, profiles);
      if (argv.length === 0) return;
      const project = projects.find((item) => item.id === projectId);
      await assignPaneTarget(sessionId, leafId, {
        kind: "agent",
        projectId,
        daemonId: runtime.daemon_id,
        argv,
        title: project ? `${agent.name} · ${project.title}` : agent.name,
      });
      updatePane(wsId, sessionId, leafId, { agentId });
    },
    [agents, assignPaneTarget, profiles, projects, runtimeById, updatePane, wsId],
  );

  const handleLauncherShell = useCallback(
    async (projectId: string | null) => {
      if (activeSession) {
        await openShellInPane(activeSession.id, activeSession.activeLeafId, projectId);
        return;
      }
      const project = projects.find((item) => item.id === projectId);
      const title = project?.title ?? t(($) => $.terminal.home);
      const sessionId = createSession(wsId, title);
      const created = useTerminalSessionStore.getState().byWorkspace[wsId]?.sessions[0];
      const leafId = created?.activeLeafId;
      if (leafId) await openShellInPane(sessionId, leafId, projectId);
    },
    [activeSession, createSession, openShellInPane, projects, t, wsId],
  );

  const handleLauncherAgent = useCallback(
    async (projectId: string | null, agentId: string) => {
      if (activeSession) {
        await startAgentInPane(activeSession.id, activeSession.activeLeafId, projectId, agentId);
        return;
      }
      const agent = agents.find((item) => item.id === agentId);
      const sessionId = createSession(wsId, agent?.name ?? t(($) => $.terminal.agent));
      const created = useTerminalSessionStore.getState().byWorkspace[wsId]?.sessions[0];
      const leafId = created?.activeLeafId;
      if (leafId) await startAgentInPane(sessionId, leafId, projectId, agentId);
    },
    [activeSession, agents, createSession, startAgentInPane, t, wsId],
  );

  const spawnHomeShell = () => {
    createSession(
      wsId,
      t(($) => $.terminal.home),
      defaultDaemonId ? { kind: "shell", daemonId: defaultDaemonId, argv: [] } : undefined,
    );
    if (isCompact) setSidebarCollapsed(wsId, true);
  };

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.repeat || isImeComposing(event)) return;
      const mac = getShortcutPlatform() === "macos";
      const primary = mac ? event.metaKey : event.ctrlKey;
      const target = event.target;
      if (
        target instanceof HTMLElement &&
        isEditableShortcutTarget(target) &&
        !target.closest('[aria-label="Terminal input"]')
      ) {
        return;
      }
      if (event.altKey && activeSession && (event.key === "ArrowLeft" || event.key === "ArrowRight")) {
        event.preventDefault();
        setActiveLeaf(
          wsId,
          activeSession.id,
          adjacentLeaf(
            activeSession.layout,
            activeSession.activeLeafId,
            event.key === "ArrowRight" ? 1 : -1,
          ),
        );
        return;
      }
      if (!primary) return;
      if ((event.key === "n" || event.key === "N") && event.shiftKey) {
        event.preventDefault();
        spawnHomeShell();
        return;
      }
      if (!activeSession) return;
      if (event.key === "\\") {
        event.preventDefault();
        splitPane(
          wsId,
          activeSession.id,
          activeSession.activeLeafId,
          event.shiftKey ? "horizontal" : "vertical",
        );
        return;
      }
      if ((event.key === "w" || event.key === "W") && event.shiftKey) {
        event.preventDefault();
        closePtyProcess(activeSession.activeLeafId);
        closePane(wsId, activeSession.id, activeSession.activeLeafId);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [activeSession, closePane, createSession, defaultDaemonId, isCompact, setActiveLeaf, setSidebarCollapsed, splitPane, t, wsId]);

  const launcherRows = useMemo<TerminalLauncherRow[]>(() => {
    const home: TerminalLauncherRow = {
      id: "home",
      project: null,
      title: t(($) => $.terminal.home),
      path: t(($) => $.terminal.cwd_home),
      agents: machineAgents,
    };
    return [
      home,
      ...projects.map((project) => ({
        id: project.id,
        project,
        title: project.title,
        path: t(($) => $.terminal.cwd_project_unknown),
        agents: machineAgents,
      })),
    ];
  }, [machineAgents, projects, t]);

  const newAction = (
    <TerminalNewMenu
      projects={projects}
      agents={machineAgents}
      onNewShell={spawnHomeShell}
      onOpenProject={(projectId) => {
        const project = projects.find((item) => item.id === projectId);
        const sessionId = createSession(wsId, project?.title ?? t(($) => $.terminal.home));
        const leafId = useTerminalSessionStore.getState().byWorkspace[wsId]?.sessions[0]?.activeLeafId;
        if (leafId) void openShellInPane(sessionId, leafId, projectId);
        if (isCompact) setSidebarCollapsed(wsId, true);
      }}
      onStartAgent={(agentId) => {
        void handleLauncherAgent(null, agentId);
        if (isCompact) setSidebarCollapsed(wsId, true);
      }}
    />
  );

  const rail = (
    <TerminalSessionRail
      sessions={sessions}
      activeSessionId={activeSessionId}
      collapsed={!isCompact && sidebarCollapsed}
      className={isCompact ? "w-full" : undefined}
      showCollapse={!isCompact}
      newAction={newAction}
      onSelect={(sessionId) => {
        setActiveSession(wsId, sessionId);
        if (isCompact) setSidebarCollapsed(wsId, true);
      }}
      onClose={(sessionId) => {
        const session = sessions.find((item) => item.id === sessionId);
        if (session) closePtyProcesses(collectLeafIds(session.layout));
        closeSession(wsId, sessionId);
      }}
      onRename={(sessionId, title) => renameSession(wsId, sessionId, title)}
      onDuplicate={(sessionId) => duplicateSession(wsId, sessionId)}
      onToggleCollapsed={() => setSidebarCollapsed(wsId, !sidebarCollapsed)}
    />
  );

  const workspace = activeSession ? (
    <TerminalSplitLayout
      node={activeSession.layout}
      panes={activeSession.panes}
      activeLeafId={activeSession.activeLeafId}
      expandedLeafId={activeSession.expandedLeafId ?? null}
      slug={slug}
      launcherRows={launcherRows}
      hasMachine={!!defaultDaemonId}
      onFocus={(leafId) => setActiveLeaf(wsId, activeSession.id, leafId)}
      onSplitRight={(leafId) => splitPane(wsId, activeSession.id, leafId, "vertical")}
      onSplitDown={(leafId) => splitPane(wsId, activeSession.id, leafId, "horizontal")}
      onClose={(leafId) => {
        closePtyProcess(leafId);
        closePane(wsId, activeSession.id, leafId);
      }}
      onToggleExpand={(leafId) =>
        setExpandedLeaf(
          wsId,
          activeSession.id,
          activeSession.expandedLeafId === leafId ? null : leafId,
        )
      }
      onEqualize={(leafId) => equalizePane(wsId, activeSession.id, leafId)}
      onSplitRatio={(splitId, ratio) => setSplitRatio(wsId, activeSession.id, splitId, ratio)}
      onRename={(leafId, title) => updatePane(wsId, activeSession.id, leafId, { title })}
      onOpenShell={(leafId, projectId) => void openShellInPane(activeSession.id, leafId, projectId)}
      onStartAgent={(leafId, projectId, agentId) =>
        void startAgentInPane(activeSession.id, leafId, projectId, agentId)
      }
    />
  ) : (
    <TerminalLauncher
      hasMachine={!!defaultDaemonId}
      rows={launcherRows}
      onOpenShell={(projectId) => void handleLauncherShell(projectId)}
      onStartAgent={(projectId, agentId) => void handleLauncherAgent(projectId, agentId)}
    />
  );

  if (isCompact) {
    const showList = !sidebarCollapsed || !activeSession;
    if (showList) {
      return <div className="flex h-full min-h-0">{rail}</div>;
    }
    return (
      <div className="flex h-full min-h-0 flex-col">
        <div className="flex h-10 shrink-0 items-center border-b px-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="gap-1.5 text-muted-foreground"
            onClick={() => setSidebarCollapsed(wsId, false)}
          >
            <ArrowLeft className="size-4" />
            {t(($) => $.terminal.sessions)}
          </Button>
        </div>
        <div className="min-h-0 flex-1">{workspace}</div>
      </div>
    );
  }

  if (sidebarCollapsed) {
    return (
      <div className="flex h-full min-h-0">
        {rail}
        <div className="min-h-0 min-w-0 flex-1">{workspace}</div>
      </div>
    );
  }

  return (
    <ResizablePanelGroup orientation="horizontal" className="h-full min-h-0">
      <ResizablePanel
        id="terminal-sessions"
        defaultSize={224}
        minSize={180}
        maxSize={360}
        groupResizeBehavior="preserve-pixel-size"
      >
        {rail}
      </ResizablePanel>
      <ResizableHandle />
      <ResizablePanel id="terminal-workspace" minSize="40%">
        {workspace}
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}

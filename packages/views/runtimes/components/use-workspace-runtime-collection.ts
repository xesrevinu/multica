"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { runtimeProfileListOptions } from "@multica/core/runtimes";
import { runtimeListOptions, runtimeKeys } from "@multica/core/runtimes/queries";
import { useWSEvent } from "@multica/core/realtime";
import { agentListOptions } from "@multica/core/workspace/queries";
import { buildWorkloadIndex } from "./runtime-list";
import { pendingRuntimeFromProfile } from "./pending-runtime";
import { buildRuntimeMachines } from "./runtime-machines";

function useNowTick(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

export function useWorkspaceRuntimeCollection({
  localDaemonId,
  localMachineName,
  hasLocalMachine,
}: {
  localDaemonId?: string | null;
  localMachineName?: string | null;
  hasLocalMachine?: boolean;
} = {}) {
  const isAuthLoading = useAuthStore((state) => state.isLoading);
  const currentUserId = useAuthStore((state) => state.user?.id);
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(
    runtimeListOptions(wsId),
  );
  const { data: runtimeProfiles = [], isLoading: profilesLoading } = useQuery(
    runtimeProfileListOptions(wsId),
  );
  const { data: agents = [], isLoading: agentsLoading } = useQuery(
    agentListOptions(wsId),
  );
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const { data: chatSessions = [], isLoading: chatSessionsLoading } = useQuery(
    chatSessionsOptions(wsId),
  );

  const handleDaemonEvent = useCallback(() => {
    qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  }, [qc, wsId]);
  useWSEvent("daemon:register", handleDaemonEvent);

  const workloadIndex = useMemo(
    () => buildWorkloadIndex(agents, snapshot),
    [agents, snapshot],
  );
  const now = useNowTick();
  const machines = useMemo(
    () =>
      buildRuntimeMachines(runtimes, {
        now,
        localDaemonId,
        localMachineName,
        currentUserId,
        workloadByRuntimeId: workloadIndex,
        ensureLocalMachine: hasLocalMachine,
      }),
    [
      runtimes,
      now,
      localDaemonId,
      localMachineName,
      currentUserId,
      workloadIndex,
      hasLocalMachine,
    ],
  );
  const orphanProfileRuntimes = useMemo(() => {
    if (machines.some((machine) => machine.mode === "local")) return [];
    return runtimeProfiles.map((profile) => {
      const createdAt = Date.parse(profile.created_at);
      return pendingRuntimeFromProfile({
        profile,
        createdAt: Number.isFinite(createdAt) ? createdAt : 0,
        fallbackMachineName: "Unassigned",
      });
    });
  }, [machines, runtimeProfiles]);

  return {
    loading: isAuthLoading || runtimesLoading || profilesLoading,
    wsId,
    currentUserId,
    runtimes,
    runtimesLoading,
    agents,
    agentsLoading,
    chatSessions,
    chatSessionsLoading,
    machines,
    orphanProfileRuntimes,
    now,
  };
}

"use client";

import { useCallback, useMemo, useRef, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Terminal } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useRequiredWorkspaceSlug } from "@multica/core/paths";
import { projectListOptions, projectResourcesOptions } from "@multica/core/projects";
import { runtimeListOptions, runtimeProfileListOptions } from "@multica/core/runtimes";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { AgentRuntime } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { CollectionPageHeader } from "../layout/collection-page";
import { PAGE_GUTTER } from "../layout/page-header";
import { useT } from "../i18n";
import { buildRuntimeMachines } from "../runtimes/components/runtime-machines";
import { GhosttyHost, type GhosttyHostHandle } from "./ghostty-host";
import {
  cliArgvForRuntime,
  isBoundOnlineAgent,
  localPathForDaemon,
  ptyWebSocketUrl,
} from "./session";

type ConnectionState = "idle" | "connecting" | "connected" | "error";

export function TerminalPage() {
  const { t } = useT("layout");
  const wsId = useWorkspaceId();
  const slug = useRequiredWorkspaceSlug();
  const [projectId, setProjectId] = useState<string | null>(null);
  const [machineId, setMachineId] = useState<string | null>(null);
  const [agentId, setAgentId] = useState<string | null>(null);
  const [status, setStatus] = useState<ConnectionState>("idle");
  const [statusMessage, setStatusMessage] = useState("");

  const termRef = useRef<GhosttyHostHandle | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const pendingOpenRef = useRef<{ cwd: string; argv: string[] } | null>(null);

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: resources } = useQuery({
    ...projectResourcesOptions(wsId, projectId ?? ""),
    enabled: !!projectId,
  });
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
  const onlineAgents = useMemo(
    () => agents.filter((agent) => isBoundOnlineAgent(agent, runtimeById)),
    [agents, runtimeById],
  );

  const selectedMachine = machines.find((machine) => machine.id === machineId) ?? null;
  const selectedAgent = onlineAgents.find((agent) => agent.id === agentId) ?? null;
  const selectedRuntime = selectedAgent ? runtimeById.get(selectedAgent.runtime_id) : undefined;
  const cwd = localPathForDaemon(resources, selectedMachine?.daemonId ?? selectedRuntime?.daemon_id);

  const projectItems = useMemo(
    () => [
      { value: "none", label: t(($) => $.terminal.none) },
      ...projects.map((project) => ({ value: project.id, label: project.title })),
    ],
    [projects, t],
  );
  const machineItems = useMemo(
    () => machines.map((machine) => ({ value: machine.id, label: machine.title })),
    [machines],
  );
  const agentItems = useMemo(
    () => onlineAgents.map((agent) => ({ value: agent.id, label: agent.name })),
    [onlineAgents],
  );

  const disconnect = useCallback(() => {
    pendingOpenRef.current = null;
    const socket = socketRef.current;
    socketRef.current = null;
    if (socket && socket.readyState < WebSocket.CLOSING) {
      try {
        socket.send(JSON.stringify({ type: "pty.close" }));
      } catch {
        // ignore
      }
      socket.close();
    }
    setStatus("idle");
    setStatusMessage("");
  }, []);

  const connect = useCallback(
    (daemonId: string, argv: string[], cwdPath: string) => {
      disconnect();
      pendingOpenRef.current = { cwd: cwdPath, argv };
      setStatus("connecting");
      setStatusMessage(t(($) => $.terminal.connecting));

      const socket = new WebSocket(ptyWebSocketUrl(daemonId, slug));
      socket.binaryType = "arraybuffer";
      socketRef.current = socket;

      const sendOpen = () => {
        const pending = pendingOpenRef.current;
        if (!pending) return;
        const cols = termRef.current?.cols ?? 80;
        const rows = termRef.current?.rows ?? 24;
        socket.send(
          JSON.stringify({
            type: "pty.open",
            cols,
            rows,
            cwd: pending.cwd,
            argv: pending.argv,
          }),
        );
      };

      socket.onopen = () => {
        const token = api.getToken?.() ?? null;
        if (token) {
          socket.send(JSON.stringify({ type: "auth", payload: { token } }));
          return;
        }
        sendOpen();
      };

      socket.onmessage = (event) => {
        if (typeof event.data === "string") {
          let msg: { type?: string; error?: string } = {};
          try {
            msg = JSON.parse(event.data) as { type?: string; error?: string };
          } catch {
            return;
          }
          if (msg.type === "auth_ack") {
            sendOpen();
            return;
          }
          if (msg.type === "pty.opened") {
            setStatus("connected");
            setStatusMessage(t(($) => $.terminal.connected));
            const cols = termRef.current?.cols ?? 80;
            const rows = termRef.current?.rows ?? 24;
            socket.send(JSON.stringify({ type: "pty.resize", cols, rows }));
            return;
          }
          if (msg.type === "pty.exit") {
            setStatus("idle");
            setStatusMessage(t(($) => $.terminal.disconnected));
            return;
          }
          if (msg.type === "pty.error" || msg.type === "error" || msg.error) {
            setStatus("error");
            setStatusMessage(
              t(($) => $.terminal.error, { message: msg.error || t(($) => $.terminal.daemon_offline) }),
            );
          }
          return;
        }
        const bytes =
          event.data instanceof ArrayBuffer
            ? new Uint8Array(event.data)
            : event.data instanceof Blob
              ? null
              : new Uint8Array(event.data as ArrayBuffer);
        if (bytes) termRef.current?.write(bytes);
        else if (event.data instanceof Blob) {
          void event.data.arrayBuffer().then((buf) => termRef.current?.write(new Uint8Array(buf)));
        }
      };

      socket.onerror = () => {
        setStatus("error");
        setStatusMessage(t(($) => $.terminal.daemon_offline));
      };
      socket.onclose = () => {
        if (socketRef.current === socket) {
          socketRef.current = null;
          setStatus((current) => (current === "connecting" ? "error" : "idle"));
          setStatusMessage((current) => current || t(($) => $.terminal.disconnected));
        }
      };
    },
    [disconnect, slug, t],
  );

  const openShell = () => {
    const daemonId = selectedMachine?.daemonId;
    if (!daemonId) {
      setStatus("error");
      setStatusMessage(t(($) => $.terminal.select_machine));
      return;
    }
    connect(daemonId, [], cwd);
  };

  const startAgent = () => {
    if (!selectedAgent || !selectedRuntime?.daemon_id) {
      setStatus("error");
      setStatusMessage(t(($) => $.terminal.select_agent));
      return;
    }
    const argv = cliArgvForRuntime(selectedRuntime, profiles);
    if (argv.length === 0) {
      setStatus("error");
      setStatusMessage(t(($) => $.terminal.select_agent));
      return;
    }
    if (selectedRuntime.daemon_id && selectedMachine?.daemonId !== selectedRuntime.daemon_id) {
      const machine = machines.find((item) => item.daemonId === selectedRuntime.daemon_id);
      if (machine) setMachineId(machine.id);
    }
    connect(selectedRuntime.daemon_id, argv, localPathForDaemon(resources, selectedRuntime.daemon_id));
  };

  const handleTermData = (data: string) => {
    const socket = socketRef.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) return;
    socket.send(new TextEncoder().encode(data));
  };

  const handleTermResize = (size: { cols: number; rows: number }) => {
    const socket = socketRef.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) return;
    socket.send(JSON.stringify({ type: "pty.resize", cols: size.cols, rows: size.rows }));
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <CollectionPageHeader
        icon={Terminal}
        title={t(($) => $.nav.terminal)}
        description={
          cwd
            ? t(($) => $.terminal.cwd_project, { path: cwd })
            : t(($) => $.terminal.cwd_home)
        }
      />
      <div className={`flex flex-wrap items-end gap-2 border-b py-2 ${PAGE_GUTTER}`}>
        <Field label={t(($) => $.terminal.project)}>
          <Select
            items={projectItems}
            value={projectId ?? "none"}
            onValueChange={(next) => setProjectId(!next || next === "none" ? null : next)}
          >
            <SelectTrigger size="sm" className="w-48" aria-label={t(($) => $.terminal.project)}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {projectItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label={t(($) => $.terminal.machine)}>
          <Select
            items={machineItems}
            value={machineId ?? ""}
            onValueChange={(next) => setMachineId(next || null)}
          >
            <SelectTrigger size="sm" className="w-48" aria-label={t(($) => $.terminal.machine)}>
              <SelectValue placeholder={t(($) => $.terminal.no_online_machines)} />
            </SelectTrigger>
            <SelectContent>
              {machineItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label={t(($) => $.terminal.agent)}>
          <Select
            items={agentItems}
            value={agentId ?? ""}
            onValueChange={(next) => setAgentId(next || null)}
          >
            <SelectTrigger size="sm" className="w-48" aria-label={t(($) => $.terminal.agent)}>
              <SelectValue placeholder={t(($) => $.terminal.no_online_agents)} />
            </SelectTrigger>
            <SelectContent>
              {agentItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Button size="sm" onClick={openShell} disabled={!selectedMachine}>
          {t(($) => $.terminal.open_shell)}
        </Button>
        <Button size="sm" variant="outline" onClick={startAgent} disabled={!selectedAgent}>
          {t(($) => $.terminal.start_agent)}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          onClick={disconnect}
          disabled={status === "idle"}
        >
          {t(($) => $.terminal.disconnect)}
        </Button>
        {statusMessage ? (
          <span className="pb-1 text-caption text-muted-foreground">{statusMessage}</span>
        ) : null}
      </div>
      <div className="min-h-0 flex-1">
        <GhosttyHost
          onReady={(handle) => {
            termRef.current = handle;
          }}
          onData={handleTermData}
          onResize={handleTermResize}
        />
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1 text-caption text-muted-foreground">
      {label}
      {children}
    </label>
  );
}

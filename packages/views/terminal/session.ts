import { isAgentRuntimeBound } from "@multica/core/agents";
import { api } from "@multica/core/api";
import { getCurrentSlug } from "@multica/core/platform";
import type {
  Agent,
  AgentRuntime,
  LocalDirectoryResourceRef,
  ProjectResource,
  ProjectResourceRef,
  RuntimeProfile,
} from "@multica/core/types";

export function isLocalDirectoryRef(
  ref: ProjectResourceRef,
): ref is LocalDirectoryResourceRef {
  return (
    typeof ref === "object" &&
    ref !== null &&
    "local_path" in ref &&
    "daemon_id" in ref &&
    typeof (ref as LocalDirectoryResourceRef).local_path === "string" &&
    typeof (ref as LocalDirectoryResourceRef).daemon_id === "string"
  );
}

export function localPathForDaemon(
  resources: ProjectResource[] | undefined,
  daemonId: string | null | undefined,
): string {
  if (!daemonId || !resources) return "";
  for (const resource of resources) {
    if (resource.resource_type !== "local_directory") continue;
    if (!isLocalDirectoryRef(resource.resource_ref)) continue;
    if (resource.resource_ref.daemon_id !== daemonId) continue;
    const path = resource.resource_ref.local_path.trim();
    if (path) return path;
  }
  return "";
}

export function cliArgvForRuntime(
  runtime: AgentRuntime | undefined,
  profiles: RuntimeProfile[] | undefined,
): string[] {
  if (!runtime) return [];
  if (runtime.profile_id) {
    const profile = profiles?.find((item) => item.id === runtime.profile_id);
    if (profile?.command_name) {
      return [profile.command_name, ...(profile.fixed_args ?? [])];
    }
  }
  const provider = runtime.provider?.trim();
  return provider ? [provider] : [];
}

export function isBoundOnlineAgent(
  agent: Agent,
  runtimeById: Map<string, AgentRuntime>,
): boolean {
  if (!isAgentRuntimeBound(agent)) return false;
  return runtimeById.get(agent.runtime_id)?.status === "online";
}

export function machineIdForDaemon(
  machines: Array<{ id: string; daemonId: string | null }>,
  daemonId: string | null | undefined,
): string | null {
  if (!daemonId) return null;
  return machines.find((machine) => machine.daemonId === daemonId)?.id ?? null;
}

export function firstLocalDirectoryDaemonId(
  resources: ProjectResource[] | undefined,
): string | null {
  if (!resources) return null;
  for (const resource of resources) {
    if (resource.resource_type !== "local_directory") continue;
    if (!isLocalDirectoryRef(resource.resource_ref)) continue;
    const daemonId = resource.resource_ref.daemon_id.trim();
    if (daemonId) return daemonId;
  }
  return null;
}

export function onlineAgentsForDaemon(
  agents: Agent[],
  runtimeById: Map<string, AgentRuntime>,
  daemonId: string | null | undefined,
): Agent[] {
  if (!daemonId) return [];
  return agents.filter((agent) => {
    if (!isBoundOnlineAgent(agent, runtimeById)) return false;
    return runtimeById.get(agent.runtime_id)?.daemon_id === daemonId;
  });
}

export function ptyWebSocketUrl(
  daemonId: string,
  workspaceSlug: string,
  ptyId: string,
): string {
  const base = api.getBaseUrl?.() ?? "";
  let url: URL;
  if (base) {
    url = new URL(base);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    const path = url.pathname.replace(/\/+$/, "");
    url.pathname = `${path}/ws/pty`;
  } else if (typeof window !== "undefined") {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    url = new URL(`${proto}//${window.location.host}/ws/pty`);
  } else {
    url = new URL("ws://localhost:8080/ws/pty");
  }
  url.search = "";
  url.hash = "";
  url.searchParams.set("daemon_id", daemonId);
  url.searchParams.set("workspace_slug", workspaceSlug || getCurrentSlug() || "");
  url.searchParams.set("pty_id", ptyId);
  return url.toString();
}

import { describe, expect, it } from "vitest";
import type { Agent, AgentRuntime, ProjectResource, RuntimeProfile } from "@multica/core/types";
import {
  cliArgvForRuntime,
  isBoundOnlineAgent,
  isLocalDirectoryRef,
  localPathForDaemon,
} from "./session";

function runtime(partial: Partial<AgentRuntime> & Pick<AgentRuntime, "id">): AgentRuntime {
  return {
    workspace_id: "ws",
    daemon_id: "daemon-1",
    name: "Claude (mac)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: null,
    created_at: "",
    updated_at: "",
    ...partial,
  };
}

describe("localPathForDaemon", () => {
  const resources: ProjectResource[] = [
    {
      id: "r1",
      project_id: "p1",
      workspace_id: "ws",
      resource_type: "github_repo",
      resource_ref: { url: "https://github.com/acme/app.git" },
      label: null,
      position: 0,
      created_at: "",
      created_by: null,
    },
    {
      id: "r2",
      project_id: "p1",
      workspace_id: "ws",
      resource_type: "local_directory",
      resource_ref: { local_path: "/Users/kee/Code/app", daemon_id: "mac" },
      label: "mac",
      position: 1,
      created_at: "",
      created_by: null,
    },
  ];

  it("returns the matching local_directory path", () => {
    expect(localPathForDaemon(resources, "mac")).toBe("/Users/kee/Code/app");
  });

  it("returns empty when the daemon has no local_directory", () => {
    expect(localPathForDaemon(resources, "vmiss")).toBe("");
  });
});

describe("cliArgvForRuntime", () => {
  it("uses the provider name for built-in runtimes", () => {
    expect(cliArgvForRuntime(runtime({ id: "rt-1", provider: "codex" }), [])).toEqual([
      "codex",
    ]);
  });

  it("uses the custom profile command and fixed args", () => {
    const profiles: RuntimeProfile[] = [
      {
        id: "prof-1",
        workspace_id: "ws",
        display_name: "Company Codex",
        protocol_family: "codex",
        command_name: "company-codex",
        description: null,
        fixed_args: ["--bar"],
        visibility: "workspace",
        created_by: "user-1",
        enabled: true,
        created_at: "",
        updated_at: "",
      },
    ];
    expect(
      cliArgvForRuntime(runtime({ id: "rt-1", provider: "codex", profile_id: "prof-1" }), profiles),
    ).toEqual(["company-codex", "--bar"]);
  });
});

describe("isBoundOnlineAgent", () => {
  const agent: Agent = {
    id: "ag-1",
    workspace_id: "ws",
    runtime_id: "rt-1",
    runtime_bound: true,
    name: "Codex",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    created_at: "",
    updated_at: "",
    archived_at: null,
    owner_id: "user-1",
    visibility: "workspace",
    permission_mode: "public_to",
    invocation_targets: [],
  } as unknown as Agent;

  it("requires a bound runtime that is online", () => {
    const map = new Map<string, AgentRuntime>([
      ["rt-1", runtime({ id: "rt-1", status: "online" })],
    ]);
    expect(isBoundOnlineAgent(agent, map)).toBe(true);
    map.set("rt-1", runtime({ id: "rt-1", status: "offline" }));
    expect(isBoundOnlineAgent(agent, map)).toBe(false);
  });
});

describe("isLocalDirectoryRef", () => {
  it("accepts local_directory refs", () => {
    expect(isLocalDirectoryRef({ local_path: "/tmp", daemon_id: "d1" })).toBe(true);
    expect(isLocalDirectoryRef({ url: "https://github.com/acme/app.git" })).toBe(false);
  });
});

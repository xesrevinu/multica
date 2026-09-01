import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../layout/session-tabs", () => ({
  useSessionTabsEnabled: () => false,
}));

import enAgents from "../../locales/en/agents.json";
import enRuntimes from "../../locales/en/runtimes.json";
import type { RuntimeMachine } from "./runtime-machines";

const navigation = vi.hoisted(() => ({
  push: vi.fn(),
}));

const collection = vi.hoisted(() => ({
  current: {
    loading: false,
    wsId: "ws-1",
    currentUserId: "user-1",
    runtimes: [] as { id: string }[],
    runtimesLoading: false,
    agents: [] as { id: string }[],
    agentsLoading: false,
    chatSessions: [] as { id: string }[],
    chatSessionsLoading: false,
    machines: [] as RuntimeMachine[],
    orphanProfileRuntimes: [] as unknown[],
    now: 0,
  },
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ pathname: "/acme/runtimes", push: navigation.push }),
  AppLink: ({
    href,
    children,
    ...props
  }: {
    href: string;
    children: React.ReactNode;
    "aria-label"?: string;
  }) => (
    <a href={href} aria-label={props["aria-label"]}>
      {children}
    </a>
  ),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    newAgent: () => "/acme/agents/new",
    runtimeDetail: (id: string) => `/acme/runtimes/${id}`,
  }),
}));

vi.mock("../../i18n", () => ({
  useT: (ns: "runtimes" | "agents") => ({
    t: (pick: (bundle: typeof enRuntimes | typeof enAgents) => string) =>
      pick(ns === "agents" ? enAgents : enRuntimes),
    i18n: { language: "en" },
  }),
}));

vi.mock("./use-workspace-runtime-collection", () => ({
  useWorkspaceRuntimeCollection: () => collection.current,
}));

vi.mock("./runtimes-page", () => ({
  OrphanRuntimeProfiles: () => <div>orphan-profiles</div>,
}));

vi.mock("./connect-remote-dialog", () => ({
  ConnectRemoteDialog: () => <div>connect-dialog</div>,
}));

vi.mock("./cloud-runtime-dialog", () => ({
  CloudRuntimeDialog: () => <div>cloud-dialog</div>,
}));

vi.mock("../../agents/components/agents-page", () => ({
  AgentsPage: ({
    hideHeader,
    machineTitle,
    headerActions,
  }: {
    hideHeader?: boolean;
    machineTitle?: string | null;
    headerActions?: React.ReactNode;
  }) => (
    <div>
      agents-panel
      {hideHeader ? " hide-header" : " show-header"}
      {machineTitle ? ` machine:${machineTitle}` : " machine:all"}
      {headerActions}
    </div>
  ),
}));

import { AgentsRuntimesPage } from "./agents-runtimes-page";

function makeMachine(): RuntimeMachine {
  return {
    id: "machine-1",
    daemonId: "daemon-1",
    title: "dev.local",
    subtitle: "arm64 macOS",
    deviceInfo: "dev.local · arm64 macOS",
    cliVersion: "1.0.0",
    launchedBy: null,
    mode: "local",
    section: "local",
    isCurrent: true,
    health: "online",
    runtimes: [{ id: "rt-1" } as RuntimeMachine["runtimes"][number]],
    onlineCount: 1,
    issueCount: 0,
    runningCount: 0,
    queuedCount: 0,
    providerNames: ["claude"],
    lastSeenAt: "2026-05-17T11:59:50Z",
  };
}

describe("AgentsRuntimesPage", () => {
  beforeEach(() => {
    navigation.push.mockReset();
    collection.current = {
      loading: false,
      wsId: "ws-1",
      currentUserId: "user-1",
      runtimes: [],
      runtimesLoading: false,
      agents: [],
      agentsLoading: false,
      chatSessions: [],
      chatSessionsLoading: false,
      machines: [makeMachine()],
      orphanProfileRuntimes: [],
      now: 0,
    };
  });

  it("renders one fused chrome with machines and agents together", () => {
    render(<AgentsRuntimesPage />);
    expect(screen.getByRole("heading", { name: "Runtimes" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /All machines/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /dev.local/ })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Add a computer" })).toHaveLength(1);
    expect(screen.getByText(/agents-panel/)).toHaveTextContent("hide-header");
    expect(screen.queryByText("runtimes-panel")).toBeNull();
  });

  it("filters the agent list from the machine rail", async () => {
    const user = userEvent.setup();
    render(<AgentsRuntimesPage />);
    await user.click(screen.getByRole("button", { name: /dev.local/ }));
    expect(screen.getByText(/agents-panel/)).toHaveTextContent("machine:dev.local");
  });

  it("creates an agent from the shared header", async () => {
    const user = userEvent.setup();
    render(<AgentsRuntimesPage />);
    await user.click(screen.getByRole("button", { name: "New agent" }));
    expect(navigation.push).toHaveBeenCalledWith("/acme/agents/new");
  });
});

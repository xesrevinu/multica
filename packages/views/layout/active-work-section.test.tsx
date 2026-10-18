// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { Agent, AgentTask, Issue } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import {
  ActiveWorkSection,
  deriveActiveWorkContent,
  orderActiveTasks,
  plainFirstLine,
} from "./active-work-section";

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1" }),
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test/issues/${id}`,
    agentDetail: (id: string) => `/test/agents/${id}`,
  }),
}));

vi.mock("../navigation", () => ({
  AppLink: ({
    href,
    children,
    ...rest
  }: {
    href: string;
    children: React.ReactNode;
    [k: string]: unknown;
  }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));

function agent(id: string, name: string): Agent {
  return { id, name, avatar_url: null } as Agent;
}

function task(id: string, overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id,
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    result: null,
    error: "",
    created_at: "2026-10-18T00:00:00Z",
    ...overrides,
  } as AgentTask;
}

function issue(id: string, title: string, description: string | null = null): Issue {
  return {
    id,
    identifier: "FX-12",
    number: 12,
    title,
    description,
  } as Issue;
}

let snapshot: AgentTask[] = [];
let agents: Agent[] = [];
let issues: Record<string, Issue> = {};

function fakeApi() {
  return {
    getBaseUrl: () => "http://api.test",
    getAgentTaskSnapshot: () => Promise.resolve(snapshot),
    listAgents: () => Promise.resolve(agents),
    getIssue: (id: string) => Promise.resolve(issues[id]),
  } as unknown as ApiClient;
}

function renderSection() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <ActiveWorkSection />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  snapshot = [];
  agents = [agent("agent-1", "Codex Engineer")];
  issues = {};
  setApiInstance(fakeApi());
});

afterEach(() => {
  cleanup();
});

describe("ActiveWorkSection", () => {
  it("names the agent, the task, and the comment that started the run", async () => {
    snapshot = [
      task("task-1", {
        issue_id: "issue-1",
        started_at: "2026-10-18T00:00:00Z",
        trigger_summary: "please fix the truncation in the tab strip",
      }),
    ];
    issues = { "issue-1": issue("issue-1", "Sidebar truncates labels") };

    renderSection();

    await waitFor(() => expect(screen.getByText("Codex Engineer")).toBeTruthy());
    // Task name comes from the issue, context from the run's trigger snapshot.
    expect(screen.getByText("Sidebar truncates labels")).toBeTruthy();
    expect(
      screen.getByText("please fix the truncation in the tab strip"),
    ).toBeTruthy();
    expect(screen.getByText("Working")).toBeTruthy();
    expect(
      screen.getByRole("link").getAttribute("href"),
    ).toBe("/test/issues/issue-1");
  });

  it("renders nothing when only finished runs are left", async () => {
    snapshot = [
      task("task-1", { status: "completed", completed_at: "2026-10-18T00:00:00Z" }),
      task("task-2", { status: "cancelled" }),
    ];

    const { container } = renderSection();

    await waitFor(() => expect(container.firstChild).toBeNull());
  });

  it("keeps running ahead of queued and folds the tail behind a toggle", async () => {
    snapshot = [
      task("queued-newest", {
        status: "queued",
        created_at: "2026-10-18T04:00:00Z",
        trigger_summary: "queued newest",
      }),
      task("running", {
        started_at: "2026-10-18T01:00:00Z",
        trigger_summary: "running task",
      }),
      task("dispatched", {
        status: "dispatched",
        dispatched_at: "2026-10-18T03:00:00Z",
        trigger_summary: "dispatched task",
      }),
      task("queued-oldest", {
        status: "queued",
        created_at: "2026-10-18T02:00:00Z",
        trigger_summary: "queued oldest",
      }),
    ];

    renderSection();

    await waitFor(() => expect(screen.getByText("running task")).toBeTruthy());
    // Three rows preview; within queued, the older run is the one left out.
    expect(screen.getByText("queued newest")).toBeTruthy();
    expect(screen.queryByText("queued oldest")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /Show 1 more/ }));

    expect(screen.getByText("queued oldest")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Show fewer" })).toBeTruthy();
  });

  it("falls back to the run kind for an issue-less chat run", async () => {
    snapshot = [
      task("chat-run", {
        agent_id: "agent-2",
        chat_session_id: "session-1",
        trigger_summary: "",
      }),
    ];
    agents = [agent("agent-1", "Codex Engineer"), agent("agent-2", "Grok")];

    renderSection();

    await waitFor(() => expect(screen.getByText("Grok")).toBeTruthy());
    expect(screen.getByText("Chat run")).toBeTruthy();
    expect(screen.getByRole("link").getAttribute("href")).toBe("/test/agents/agent-2");
  });
});

describe("orderActiveTasks", () => {
  it("drops terminal rows and orders running before queued", () => {
    const ordered = orderActiveTasks([
      task("queued", { status: "queued", created_at: "2026-10-18T05:00:00Z" }),
      task("done", { status: "completed" }),
      task("running-old", { started_at: "2026-10-18T01:00:00Z" }),
      task("running-new", { started_at: "2026-10-18T02:00:00Z" }),
    ]);

    expect(ordered.map((t) => t.id)).toEqual(["running-new", "running-old", "queued"]);
  });
});

describe("plainFirstLine", () => {
  it("skips markdown headings and bullets to reach the first sentence", () => {
    expect(plainFirstLine("## Heading\n\n- first real line\nsecond")).toBe(
      "first real line",
    );
  });

  it("clamps long text on a word boundary", () => {
    const text = "a".repeat(200);
    const line = plainFirstLine(text, 20);
    expect(line.endsWith("…")).toBe(true);
    expect(line.length).toBeLessThanOrEqual(20);
  });
});

describe("deriveActiveWorkContent", () => {
  it("uses the issue description when the run has no trigger summary", () => {
    const content = deriveActiveWorkContent(
      task("task-1", { issue_id: "issue-1" }),
      issue("issue-1", "Sidebar truncates labels", "## Why\nlabels clip at 240px"),
    );

    expect(content).toEqual({
      kind: "issue",
      title: "Sidebar truncates labels",
      summary: "labels clip at 240px",
    });
  });

  it("does not repeat the title as its own summary", () => {
    const content = deriveActiveWorkContent(
      task("task-1", { issue_id: "issue-1", trigger_summary: "Sidebar truncates labels" }),
      issue("issue-1", "Sidebar truncates labels"),
    );

    expect(content.summary).toBe("");
  });
});

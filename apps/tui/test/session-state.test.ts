import { describe, expect, test } from "bun:test"
import { mkdtemp, readFile, rm } from "node:fs/promises"
import os from "node:os"
import path from "node:path"
import {
  decodeSession,
  defaultWorkspaceView,
  readSessionStateSync,
  sessionStatePath,
  settleWorkspaceView,
  withWorkspace,
  writeSessionState,
} from "../src/tui/session-state"

describe("sessionStatePath", () => {
  test("keeps view state beside the config file, including profiles", () => {
    expect(sessionStatePath("/home/x/.multica/config.json")).toBe("/home/x/.multica/tui-state.json")
    expect(sessionStatePath("/home/x/.multica/profiles/dev/config.json")).toBe(
      "/home/x/.multica/profiles/dev/tui-state.json",
    )
  })
})

describe("decodeSession", () => {
  test("migrates legacy nested surfaces into the flat IA", () => {
    const state = decodeSession(
      JSON.stringify({
        version: 1,
        workspaces: {
          "ws-1": {
            surface: "chat",
            group: { you: "inbox", work: "nope", ops: "usage" },
            project_id: "prj-1",
            issue_status: "todo",
            issue_mine: "assigned",
            inbox_view: "sideways",
            chat_status: "archived",
            agent_machine: "laptop",
            agent_scope: "mine",
            agents_view: "runtimes",
            project_status: "paused",
            project_priority: "urgent",
            selected: { issues: "iss-1", nope: "x", chat: "c1" },
            detail: { kind: "issue", id: "iss-9" },
          },
          "ws-orch": {
            surface: "orch",
            selected: { orch: "iss-2" },
            detail: { kind: "project", id: "prj-old" },
          },
        },
      }),
    )
    expect(state.workspaces["ws-1"]).toEqual({
      surface: "space",
      space_view: "chats",
      project_id: "prj-1",
      issue_status: "todo",
      issue_mine: "assigned",
      issue_attention: "all",
      inbox_view: "all",
      chat_status: "archived",
      agent_machine: "laptop",
      agent_scope: "mine",
      agents_view: "runtimes",
      project_status: "paused",
      project_priority: "urgent",
      selected: { issues: "iss-1", chats: "c1" },
      detail: { kind: "issue", id: "iss-9" },
    })
    expect(state.workspaces["ws-orch"]).toMatchObject({
      surface: "issues",
      issue_attention: "needs_you",
      selected: { issues: "iss-2" },
      detail: undefined,
    })
  })

  test("ignores a corrupt file", () => {
    expect(decodeSession("{")).toEqual({ version: 1, workspaces: {} })
  })
})

describe("settleWorkspaceView", () => {
  test("clears filters that are not in the loaded workspace", () => {
    const view = { ...defaultWorkspaceView(), project_id: "gone", issue_status: "custom", agent_machine: "old" }
    expect(
      settleWorkspaceView(view, { projectIDs: ["prj-1"], statusKeys: ["todo"], machines: ["laptop"] }),
    ).toMatchObject({ project_id: "all", issue_status: "all", agent_machine: "all" })
    expect(
      settleWorkspaceView(
        { ...view, project_id: "prj-1", issue_status: "todo", agent_machine: "laptop" },
        { projectIDs: ["prj-1"], statusKeys: ["todo"], machines: ["laptop"] },
      ),
    ).toMatchObject({ project_id: "prj-1", issue_status: "todo", agent_machine: "laptop" })
  })
})

describe("writeSessionState", () => {
  test("round-trips one workspace without dropping another", async () => {
    const dir = await mkdtemp(path.join(os.tmpdir(), "multica-tui-state-"))
    try {
      const file = path.join(dir, "tui-state.json")
      const first = withWorkspace({ version: 1, workspaces: {} }, "ws-1", {
        ...defaultWorkspaceView(),
        surface: "inbox",
        inbox_view: "unread",
      })
      await writeSessionState(file, first)
      const second = withWorkspace(readSessionStateSync(file), "ws-2", {
        ...defaultWorkspaceView(),
        surface: "space",
        space_view: "agents",
        agent_scope: "working",
      })
      await writeSessionState(file, second)
      const stored = readSessionStateSync(file)
      expect(stored.workspaces["ws-1"]?.surface).toBe("inbox")
      expect(stored.workspaces["ws-1"]?.inbox_view).toBe("unread")
      expect(stored.workspaces["ws-2"]?.surface).toBe("space")
      expect(stored.workspaces["ws-2"]?.space_view).toBe("agents")
      expect(stored.workspaces["ws-2"]?.agent_scope).toBe("working")
      const text = await readFile(file, "utf8")
      expect(text.endsWith("\n")).toBe(true)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })
})

import { describe, expect, test } from "bun:test"
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises"
import os from "node:os"
import path from "node:path"
import { configPath, resolveConnection, writeWorkspaceID } from "../src/config"

async function withHome(run: (home: string) => Promise<void>) {
  const home = await mkdtemp(path.join(os.tmpdir(), "multica-tui-"))
  try {
    await run(home)
  } finally {
    await rm(home, { recursive: true, force: true })
  }
}

describe("configPath", () => {
  test("matches the multica CLI layout", () => {
    expect(configPath(undefined, "/home/x")).toBe("/home/x/.multica/config.json")
    expect(configPath("dev", "/home/x")).toBe("/home/x/.multica/profiles/dev/config.json")
  })
})

describe("resolveConnection", () => {
  test("reads server, workspace, and token from the config file", () =>
    withHome(async (home) => {
      await mkdir(path.join(home, ".multica"), { recursive: true })
      await writeFile(
        path.join(home, ".multica", "config.json"),
        JSON.stringify({ server_url: "https://api.example.com/", workspace_id: "ws-1", token: "mul_1" }),
      )
      const connection = await resolveConnection({ home, env: {} })
      expect(connection).toEqual({
        serverUrl: "https://api.example.com",
        workspaceID: "ws-1",
        token: "mul_1",
        source: path.join(home, ".multica", "config.json"),
      })
    }))

  test("prefers flags, then env, then the config file", () =>
    withHome(async (home) => {
      await mkdir(path.join(home, ".multica"), { recursive: true })
      await writeFile(
        path.join(home, ".multica", "config.json"),
        JSON.stringify({ server_url: "https://file", workspace_id: "ws-file", token: "mul_file" }),
      )
      const env = { MULTICA_SERVER_URL: "https://env", MULTICA_WORKSPACE_ID: "ws-env", MULTICA_TOKEN: "mul_env" }
      expect((await resolveConnection({ home, env })).serverUrl).toBe("https://env")
      expect(
        (await resolveConnection({ home, env, serverUrl: "https://flag", workspaceID: "ws-flag", token: "mul_flag" }))
          .token,
      ).toBe("mul_flag")
    }))

  test("uses PSTACK_MULTICA_PROFILE when --profile is omitted", () =>
    withHome(async (home) => {
      await mkdir(path.join(home, ".multica", "profiles", "dev-multica-158"), { recursive: true })
      await writeFile(
        path.join(home, ".multica", "config.json"),
        JSON.stringify({ server_url: "https://prod", workspace_id: "ws-prod", token: "mul_prod" }),
      )
      await writeFile(
        path.join(home, ".multica", "profiles", "dev-multica-158", "config.json"),
        JSON.stringify({ server_url: "http://localhost:18238", workspace_id: "ws-dev", token: "mul_dev" }),
      )
      const connection = await resolveConnection({
        home,
        env: { PSTACK_MULTICA_PROFILE: "dev-multica-158" },
      })
      expect(connection.serverUrl).toBe("http://localhost:18238")
      expect(connection.workspaceID).toBe("ws-dev")
      expect(connection.source).toContain("profiles/dev-multica-158")
    }))

  test("reads named profiles", () =>
    withHome(async (home) => {
      await mkdir(path.join(home, ".multica", "profiles", "dev"), { recursive: true })
      await writeFile(
        path.join(home, ".multica", "profiles", "dev", "config.json"),
        JSON.stringify({ server_url: "https://dev", workspace_id: "ws-dev", token: "mul_dev" }),
      )
      const connection = await resolveConnection({ home, profile: "dev", env: {} })
      expect(connection.workspaceID).toBe("ws-dev")
    }))

  test("fails with an actionable message when the token is missing", () =>
    withHome(async (home) => {
      const result = await resolveConnection({ home, env: {} }).then(
        () => "ok",
        (error: unknown) => (error instanceof Error ? error.message : String(error)),
      )
      expect(result).toContain("multica login")
    }))
})

describe("writeWorkspaceID", () => {
  test("updates workspace_id and leaves the rest of the file alone", () =>
    withHome(async (home) => {
      const file = path.join(home, ".multica", "config.json")
      await mkdir(path.dirname(file), { recursive: true })
      await writeFile(
        file,
        '{\n  // shared with the CLI\n  "server_url": "https://api.example.com",\n  "token": "mul_1",\n  "workspace_id": "ws-1"\n}\n',
      )
      await writeWorkspaceID(file, "ws-2")
      const text = await readFile(file, "utf8")
      expect(text).toContain("// shared with the CLI")
      expect(text).toContain('"token": "mul_1"')
      expect(text).toContain('"workspace_id": "ws-2"')
      expect(text).not.toContain('"workspace_id": "ws-1"')
      await writeWorkspaceID(file, "ws-2")
      expect(await readFile(file, "utf8")).toBe(text)
    }))
})


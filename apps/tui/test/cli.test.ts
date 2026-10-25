import { describe, expect, test } from "bun:test"
import { parseArgs } from "../src/cli"

describe("parseArgs", () => {
  test("defaults to the TUI", () => {
    expect(parseArgs([])).toEqual({})
  })

  test("parses --json with and without a resource", () => {
    expect(parseArgs(["--json"]).json).toBe("overview")
    expect(parseArgs(["--json", "issues"]).json).toBe("issues")
    expect(() => parseArgs(["--json", "nope"])).toThrow("Unknown resource")
  })

  test("parses detail and connection flags", () => {
    expect(
      parseArgs([
        "--issue",
        "FX-12",
        "--profile",
        "dev",
        "--server",
        "http://localhost:3000",
        "--workspace",
        "ws-1",
        "--token",
        "mul_x",
        "-q",
        "search",
      ]),
    ).toEqual({
      issue: "FX-12",
      profile: "dev",
      serverUrl: "http://localhost:3000",
      workspaceID: "ws-1",
      token: "mul_x",
      query: "search",
    })
  })

  test("rejects unknown flags and missing values", () => {
    expect(() => parseArgs(["--nope"])).toThrow("Unknown flag")
    expect(() => parseArgs(["--issue"])).toThrow("requires a value")
  })
})

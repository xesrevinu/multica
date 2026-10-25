import { describe, expect, test } from "bun:test"
import { mkdtempSync, readFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { parseArgs } from "../src/cli"
import { parsePickIssueResult, pickIssueResult, serializePickIssueResult, writePickIssueResult } from "../src/pick"

describe("pick issue", () => {
  test("parseArgs accepts --pick-issue and --output", () => {
    expect(parseArgs(["--pick-issue", "--output", "/tmp/pick.json", "--profile", "dev-multica-158"])).toEqual({
      pickIssue: true,
      pick: "issue",
      output: "/tmp/pick.json",
      profile: "dev-multica-158",
    })
  })

  test("serializes identifier for OpenCode follow", () => {
    const result = pickIssueResult({
      id: "01a07135",
      identifier: "DEV-8",
      title: "OpenCode v2 worker e2e",
    })
    expect(parsePickIssueResult(serializePickIssueResult(result))).toEqual(result)
  })

  test("writePickIssueResult uses --output, not the TTY", () => {
    const directory = mkdtempSync(join(tmpdir(), "pick-issue-"))
    const output = join(directory, "result.json")
    writePickIssueResult(pickIssueResult({ id: "abc", identifier: "DEV-1", title: "x" }), output)
    expect(parsePickIssueResult(readFileSync(output, "utf8"))?.identifier).toBe("DEV-1")
  })
})

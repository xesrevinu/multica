import { describe, expect, test } from "bun:test"
import {
  migrateLegacySurface,
  neighborSpaceView,
  neighborSurface,
  spaceViewFromDigit,
  surfaceFromDigit,
} from "../src/tui/nav"

describe("flat nav", () => {
  test("digits pick top-level surfaces", () => {
    expect(surfaceFromDigit("1")).toBe("overview")
    expect(surfaceFromDigit("2")).toBe("inbox")
    expect(surfaceFromDigit("3")).toBe("issues")
    expect(surfaceFromDigit("4")).toBe("space")
    expect(surfaceFromDigit("9")).toBeUndefined()
  })

  test("cycles flat surfaces", () => {
    expect(neighborSurface("overview", 1)).toBe("inbox")
    expect(neighborSurface("inbox", 1)).toBe("issues")
    expect(neighborSurface("issues", 1)).toBe("space")
    expect(neighborSurface("space", 1)).toBe("overview")
    expect(neighborSurface("overview", -1)).toBe("space")
  })

  test("cycles space subviews", () => {
    expect(neighborSpaceView("projects", 1)).toBe("autopilots")
    expect(neighborSpaceView("settings", 1)).toBe("projects")
    expect(spaceViewFromDigit("1")).toBe("projects")
    expect(spaceViewFromDigit("5")).toBe("chats")
  })

  test("migrates legacy nested surfaces", () => {
    expect(migrateLegacySurface("orch")).toEqual({ surface: "issues", attention: "needs_you" })
    expect(migrateLegacySurface("chat")).toEqual({ surface: "space", spaceView: "chats" })
    expect(migrateLegacySurface("agents")).toEqual({ surface: "space", spaceView: "agents" })
    expect(migrateLegacySurface("issues")).toEqual({ surface: "issues" })
    expect(migrateLegacySurface("overview")).toEqual({ surface: "overview" })
  })
})

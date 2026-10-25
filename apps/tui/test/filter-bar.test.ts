import { describe, expect, test } from "bun:test"
import { activeFilterCount, nextFilterValue, type FilterGroup } from "../src/tui/filter-bar"

const group = (value: string): FilterGroup => ({
  id: "attention",
  label: "attention",
  value,
  fallback: "all",
  compact: true,
  options: [
    { id: "all", label: "all" },
    { id: "needs_you", label: "needs_you" },
    { id: "running", label: "running" },
  ],
})

describe("filter chips", () => {
  test("cycles through options and wraps back to the fallback", () => {
    expect(nextFilterValue(group("all"))).toBe("needs_you")
    expect(nextFilterValue(group("needs_you"))).toBe("running")
    expect(nextFilterValue(group("running"))).toBe("all")
  })

  test("counts groups that differ from their fallback", () => {
    expect(activeFilterCount([group("all"), group("all")])).toBe(0)
    expect(activeFilterCount([group("all"), group("running")])).toBe(1)
  })
})

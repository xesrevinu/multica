import { describe, expect, test } from "bun:test"
import { parseMarkdown } from "../src/markdown"

describe("parseMarkdown", () => {
  test("parses headings and mentions", () => {
    const blocks = parseMarkdown("# Title\n\n[@alice](mention://member/id)\n\nHello world")
    expect(blocks[0]).toEqual({ kind: "heading", level: 1, text: "Title" })
    expect(blocks[1]).toEqual({ kind: "paragraph", text: "@alice" })
    expect(blocks[2]).toEqual({ kind: "paragraph", text: "Hello world" })
  })

  test("parses code fences", () => {
    const blocks = parseMarkdown("```ts\nconst x = 1\n```")
    expect(blocks[0]).toEqual({ kind: "code", lang: "ts", lines: ["const x = 1"] })
  })

  test("parses bullet lists", () => {
    const blocks = parseMarkdown("- one\n- two")
    expect(blocks[0]).toEqual({ kind: "list", ordered: false, items: ["one", "two"] })
  })
})

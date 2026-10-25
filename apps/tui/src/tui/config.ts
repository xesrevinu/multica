import { Option, Schema } from "effect"
import { applyEdits, modify, parse, type ParseError } from "jsonc-parser"
import { mkdir } from "node:fs/promises"
import path from "node:path"
import { homedir } from "node:os"
import { type Info, type Interface } from "./opentui-config"

const InfoSchema = Schema.Struct({
  mouse: Schema.optional(Schema.Boolean),
  cursor: Schema.optional(Schema.Literals(["block", "underline", "bar", "line"] as const)),
  terminal: Schema.optional(
    Schema.Struct({
      copy: Schema.optional(Schema.Literals(["select", "manual"] as const)),
    }),
  ),
})

const decode = Schema.decodeUnknownOption(InfoSchema)
const empty: Info = {}

function defaultConfigDir() {
  return process.env.OPENCODE_CONFIG_DIR ?? path.join(homedir(), ".config", "opencode")
}

export function cliConfigPath() {
  return path.join(defaultConfigDir(), "cli.json")
}

export function createCliConfigService(file = cliConfigPath()): Interface {
  const get = async () => {
    const text = await Bun.file(file)
      .text()
      .catch(() => undefined)
    if (text === undefined) return empty
    const errors: ParseError[] = []
    const value = parse(text, errors, { allowTrailingComma: true })
    if (errors.length) return empty
    return Option.getOrElse(decode(value), () => empty)
  }

  return {
    path: file,
    get,
    update: async (update) => {
      const current = await get()
      const next = structuredClone(current)
      update(next)
      const edits = changes(current, next)
      if (!edits.length) return current
      const text = await Bun.file(file)
        .text()
        .catch(() => "{}")
      const updated = edits.reduce(
        (text, edit) =>
          applyEdits(
            text,
            modify(text, edit.path, edit.value, { formattingOptions: { tabSize: 2, insertSpaces: true } }),
          ),
        text,
      )
      const errors: ParseError[] = []
      const config = Option.getOrUndefined(decode(parse(updated, errors, { allowTrailingComma: true })))
      if (errors.length || config === undefined) throw new Error("Invalid CLI config update")
      await mkdir(path.dirname(file), { recursive: true })
      await Bun.write(file, updated.endsWith("\n") ? updated : `${updated}\n`)
      return config
    },
  }
}

function changes(
  before: unknown,
  after: unknown,
  path: (string | number)[] = [],
): { path: (string | number)[]; value: unknown }[] {
  if (Object.is(before, after)) return []
  if (
    before !== null &&
    after !== null &&
    typeof before === "object" &&
    typeof after === "object" &&
    !Array.isArray(before) &&
    !Array.isArray(after)
  ) {
    const keys = new Set([...Object.keys(before), ...Object.keys(after)])
    return [...keys].flatMap((key) => {
      if (!(key in after)) return [{ path: [...path, key], value: undefined }]
      if (!(key in before)) return [{ path: [...path, key], value: (after as Record<string, unknown>)[key] }]
      return changes(
        (before as Record<string, unknown>)[key],
        (after as Record<string, unknown>)[key],
        [...path, key],
      )
    })
  }
  return [{ path, value: after }]
}

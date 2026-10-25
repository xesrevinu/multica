import { renameSync, writeFileSync } from "node:fs"

export type PickIssueResult = {
  readonly id: string
  readonly identifier: string
  readonly title: string
}

export function pickIssueResult(issue: {
  readonly id: string
  readonly identifier: string
  readonly title: string
}): PickIssueResult {
  return {
    id: issue.id,
    identifier: issue.identifier,
    title: issue.title,
  }
}

export function parsePickIssueResult(text: string): PickIssueResult | undefined {
  const trimmed = text.trim()
  if (!trimmed) return undefined
  try {
    const value = JSON.parse(trimmed) as Partial<PickIssueResult>
    if (!value.id?.trim() || !value.identifier?.trim()) return undefined
    return {
      id: value.id.trim(),
      identifier: value.identifier.trim(),
      title: typeof value.title === "string" ? value.title : "",
    }
  } catch {
    return undefined
  }
}

export function serializePickIssueResult(result: PickIssueResult): string {
  return `${JSON.stringify(result)}\n`
}

/** Pane TUI owns stdout; the OpenCode plugin always passes `--output`. */
export function writePickResult(result: unknown, output?: string) {
  const json = `${JSON.stringify(result)}\n`
  if (output) {
    const tmp = `${output}.tmp`
    writeFileSync(tmp, json)
    renameSync(tmp, output)
  } else process.stdout.write(json)
}

export function writePickIssueResult(result: PickIssueResult, output?: string) {
  writePickResult(result, output)
}

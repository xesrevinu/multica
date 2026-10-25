import { Option, Schema } from "effect"
import { applyEdits, modify } from "jsonc-parser"
import { mkdir } from "node:fs/promises"
import os from "node:os"
import path from "node:path"

// Mirrors the multica CLI's stored config (`multica config show`). Only the
// fields needed to reach the API are read; everything else is ignored.
export const StoredConfig = Schema.Struct({
  server_url: Schema.optionalKey(Schema.String),
  workspace_id: Schema.optionalKey(Schema.String),
  token: Schema.optionalKey(Schema.String),
})

const decodeStored = Schema.decodeUnknownOption(Schema.fromJsonString(StoredConfig))

export type Connection = {
  serverUrl: string
  workspaceID: string
  token: string
  source: string
}

export type ConnectionInput = {
  profile?: string
  serverUrl?: string
  workspaceID?: string
  token?: string
  home?: string
  env?: Record<string, string | undefined>
}

export function profileFromEnv(env: Record<string, string | undefined> = process.env): string | undefined {
  const value = env.PSTACK_MULTICA_PROFILE?.trim() || env.MULTICA_PROFILE?.trim()
  return value || undefined
}

// Same layout as the multica CLI: `~/.multica/config.json`, or
// `~/.multica/profiles/<name>/config.json` for a named profile.
export function configPath(profile?: string, home = os.homedir()) {
  if (!profile) return path.join(home, ".multica", "config.json")
  return path.join(home, ".multica", "profiles", profile, "config.json")
}

export async function resolveConnection(input: ConnectionInput = {}): Promise<Connection> {
  const env = input.env ?? process.env
  const profile = input.profile ?? profileFromEnv(env)
  const file = configPath(profile, input.home)
  const stored = Option.getOrElse(
    decodeStored(await Bun.file(file).text().catch(() => "{}")),
    (): typeof StoredConfig.Type => ({}),
  )
  const serverUrl = input.serverUrl ?? env.MULTICA_SERVER_URL ?? stored.server_url
  const workspaceID = input.workspaceID ?? env.MULTICA_WORKSPACE_ID ?? stored.workspace_id
  const token = input.token ?? env.MULTICA_TOKEN ?? stored.token
  if (!serverUrl) throw new Error(`No server URL. Set MULTICA_SERVER_URL or run \`multica login\` (${file})`)
  if (!token) throw new Error(`No auth token. Set MULTICA_TOKEN or run \`multica login\` (${file})`)
  if (!workspaceID) throw new Error(`No workspace. Set MULTICA_WORKSPACE_ID or pass --workspace <id>`)
  return {
    serverUrl: serverUrl.replace(/\/+$/, ""),
    workspaceID,
    token,
    source: file,
  }
}

// Updates only workspace_id. Flags and MULTICA_WORKSPACE_ID still win for the
// current process; the file is the default used on the next launch without them.
export async function writeWorkspaceID(file: string, workspaceID: string) {
  const raw = await Bun.file(file).text().catch(() => "")
  const text = raw.trim() ? raw : "{}\n"
  const edits = modify(text, ["workspace_id"], workspaceID, {
    formattingOptions: { tabSize: 2, insertSpaces: true },
  })
  if (!edits.length) return
  const updated = applyEdits(text, edits)
  await mkdir(path.dirname(file), { recursive: true })
  await Bun.write(file, updated.endsWith("\n") ? updated : `${updated}\n`)
}

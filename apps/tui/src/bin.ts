#!/usr/bin/env bun

import { parseArgs, RESOURCES, runCli } from "./cli"

const HELP = `multica-tui — Multica workspace TUI

Usage:
  multica-tui
  multica-tui --pick-issue --output <file>
  multica-tui --json [resource]
  multica-tui --issue <id-or-key>
  multica-tui --agent <id>

Resources: ${RESOURCES.join(", ")}

Flags:
  --pick-issue        Issue picker TUI; Enter writes JSON and exits 0, Esc exits 1
  --output, -o <file> Where --pick-issue writes {id,identifier,title} (pane-safe)
  --json [resource]   Print data as JSON instead of opening the TUI
  --issue, -i <id>    Print one issue with comments, task runs, and children
  --agent <id>        Print one agent with recent tasks and skills
  --query, -q <text>  Filter issues by text (with --json issues)
  --status <key>      Filter issues by status key (with --json issues)
  --profile <name>    Use ~/.multica/profiles/<name>/config.json
  --server <url>      Override the server URL (env: MULTICA_SERVER_URL)
  --workspace, -w     Override the workspace ID (env: MULTICA_WORKSPACE_ID)
  --token <token>     Override the auth token (env: MULTICA_TOKEN)
  --help, -h          Show this help

Connection details come from the multica CLI config (~/.multica/config.json).
Supports live WebSocket updates, creating issues, commenting, and editing fields.
`

async function main() {
  const options = parseArgs(process.argv.slice(2))
  if (options.help) {
    process.stdout.write(HELP)
    return
  }
  await runCli(options)
}

main().catch((error) => {
  process.stderr.write(`${error instanceof Error ? error.message : error}\n`)
  process.exitCode = 1
})

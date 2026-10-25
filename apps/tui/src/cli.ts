import { resolveConnection, type ConnectionInput } from "./config"
import { makeDataClient } from "./data"

export const RESOURCES = ["overview", "issues", "agents", "runtimes", "projects", "skills", "workspaces", "me"] as const
export type Resource = (typeof RESOURCES)[number]

export type CliOptions = ConnectionInput & {
  help?: boolean
  json?: Resource
  issue?: string
  agent?: string
  query?: string
  status?: string
  pickIssue?: boolean
  pick?: "issue" | "mentions" | "running" | "ask"
  output?: string
}

export function parseArgs(argv: readonly string[]): CliOptions {
  const options: CliOptions = {}
  for (let index = 0; index < argv.length; index++) {
    const arg = argv[index]
    if (arg === "--help" || arg === "-h") {
      options.help = true
      continue
    }
    if (arg === "--pick-issue") {
      options.pickIssue = true
      options.pick = "issue"
      continue
    }
    if (arg === "--pick") {
      const value = requireValue(arg, argv[index + 1])
      if (value !== "issue" && value !== "mentions" && value !== "running" && value !== "ask") {
        throw new Error(`--pick must be issue|mentions|running|ask`)
      }
      options.pick = value
      index++
      continue
    }
    if (arg === "--json") {
      const value = argv[index + 1]
      if (value && !value.startsWith("-")) {
        const resource = RESOURCES.find((name) => name === value)
        if (!resource) throw new Error(`Unknown resource: ${value} (expected one of ${RESOURCES.join(", ")})`)
        options.json = resource
        index++
        continue
      }
      options.json = "overview"
      continue
    }
    const value = argv[index + 1]
    if (arg === "--issue" || arg === "-i") {
      options.issue = requireValue(arg, value)
      index++
      continue
    }
    if (arg === "--agent") {
      options.agent = requireValue(arg, value)
      index++
      continue
    }
    if (arg === "--query" || arg === "-q") {
      options.query = requireValue(arg, value)
      index++
      continue
    }
    if (arg === "--status") {
      options.status = requireValue(arg, value)
      index++
      continue
    }
    if (arg === "--profile") {
      options.profile = requireValue(arg, value)
      index++
      continue
    }
    if (arg === "--server") {
      options.serverUrl = requireValue(arg, value)
      index++
      continue
    }
    if (arg === "--workspace" || arg === "-w") {
      options.workspaceID = requireValue(arg, value)
      index++
      continue
    }
    if (arg === "--token") {
      options.token = requireValue(arg, value)
      index++
      continue
    }
    if (arg === "--output" || arg === "-o") {
      options.output = requireValue(arg, value)
      index++
      continue
    }
    throw new Error(`Unknown flag: ${arg}`)
  }
  return options
}

function requireValue(flag: string, value: string | undefined) {
  if (!value || value.startsWith("-")) throw new Error(`${flag} requires a value`)
  return value
}

const print = (value: unknown) => process.stdout.write(`${JSON.stringify(value, null, 2)}\n`)

export async function runCli(options: CliOptions) {
  const connection = await resolveConnection(options)
  const data = makeDataClient(connection)
  try {
    if (options.issue) {
      const [issue, comments, tasks, children] = await Promise.all([
        data.getIssue(options.issue),
        data.listIssueComments(options.issue),
        data.listIssueTaskRuns(options.issue),
        data.listIssueChildren(options.issue),
      ])
      print({ issue, comments, task_runs: tasks, children: children.issues })
      return
    }
    if (options.agent) {
      const [agent, tasks, skills] = await Promise.all([
        data.getAgent(options.agent),
        data.listAgentTasks(options.agent),
        data.listAgentSkills(options.agent),
      ])
      print({ agent, tasks, skills })
      return
    }
    if (options.json) {
      print(await dumpResource(data, options))
      return
    }
    const { runTui } = await import("./tui/run")
    const pick = options.pick ?? (options.pickIssue ? "issue" : undefined)
    await runTui(connection, pick ? { pick, output: options.output } : undefined)
  } finally {
    if (options.issue || options.agent || options.json) await data.dispose()
  }
}

async function dumpResource(data: ReturnType<typeof makeDataClient>, options: CliOptions) {
  const resource = options.json ?? "overview"
  if (resource === "me") return data.getMe()
  if (resource === "workspaces") return data.listWorkspaces()
  if (resource === "issues") {
    return data.listIssues({
      q: options.query,
      status: options.status,
      sort_by: "updated_at",
      sort_direction: "desc",
      limit: 200,
    })
  }
  if (resource === "agents") return data.listAgents({ include_archived: true })
  if (resource === "runtimes") return data.listRuntimes()
  if (resource === "projects") return data.listProjects()
  if (resource === "skills") return data.listSkills()
  const [me, workspace, members, statuses, issues, agents, runtimes, projects, skills, working] =
    await Promise.all([
      data.getMe(),
      data.getWorkspace(data.connection.workspaceID),
      data.listMembers(data.connection.workspaceID),
      data.listIssueStatuses(),
      data.listIssues({ sort_by: "updated_at", sort_direction: "desc", limit: 200 }),
      data.listAgents({ include_archived: true }),
      data.listRuntimes(),
      data.listProjects(),
      data.listSkills(),
      data.listWorkingAgents(),
    ])
  return { me, workspace, members, statuses, issues, agents, runtimes, projects, skills, working }
}

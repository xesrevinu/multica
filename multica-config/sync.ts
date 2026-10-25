#!/usr/bin/env bun

import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSON_SCHEMA, load as loadYaml } from "js-yaml";

type JsonObject = Record<string, unknown>;

interface AgentManifest {
  name: string;
  runtime: string;
  /** Grok bot name used when runtime is gbot. Becomes MULTICA_GBOT_BOT for that agent. */
  gbot?: string;
  /** Creation-time persona of the persistent Grok Bot named by `gbot`. Sync writes it with `gbot bots update --instructions-file`. */
  gbot_instructions?: string;
  role?: string;
  description?: string;
  instructions?: string;
  model?: string;
  thinking_level?: string;
  service_tier?: string;
  custom_args?: string[];
  access?: "private" | "workspace";
  max_concurrent_tasks?: number;
  avatar?: string;
  skills?: string[];
}

interface LabelManifest {
  name: string;
  color: string;
  description?: string;
}

interface StatusManifest {
  key: string;
  name: string;
  description?: string;
  category: string;
  color: string;
}

interface SkillManifest {
  name: string;
  description?: string;
  path?: string;
  /** `all` = every agent in the merged manifest. `listed` = only agents that name it. */
  bind?: "all" | "listed";
}

interface SquadMemberManifest {
  type: "agent" | "member";
  ref: string;
  role: string;
}

interface SquadManifest {
  name: string;
  leader: string;
  description?: string;
  instructions?: string;
  avatar_url?: string;
  members?: SquadMemberManifest[];
}

interface ProjectResourceManifest {
  type: "github_repo" | "local_directory";
  url?: string;
  local_path?: string;
  runtime?: string;
  execution_mode?: "in_place" | "worktree";
  label?: string;
}

interface ProjectManifest {
  title: string;
  description?: string;
  icon?: string;
  status?: "planned" | "in_progress" | "paused" | "completed" | "cancelled";
  start_date?: string;
  due_date?: string;
  lead?: string;
  repos?: string[];
  resources?: ProjectResourceManifest[];
  /** In-app orchestrator team spec; synced with `multica team diff/apply`. */
  orchestrator?: JsonObject;
}

interface DefaultsManifest {
  agents?: {
    access?: "private" | "workspace";
    max_concurrent_tasks?: number;
  };
  projects?: {
    status?: ProjectManifest["status"];
  };
}

interface MulticaManifest {
  /** Workspace slug or id this manifest syncs into. `--workspace` overrides it. */
  workspace?: string;
  agents: AgentManifest[];
  projects: ProjectManifest[];
  squads: SquadManifest[];
  labels: LabelManifest[];
  statuses: StatusManifest[];
  skills: SkillManifest[];
  /**
   * Directories of `<name>/SKILL.md`. A skill an agent names that no `skills:`
   * entry declares is taken from the first library that has it (bind: listed).
   * The orchestrator preset is exported here with
   * `multica team preset export ~/.cursor/artifacts/multica/preset/<preset>`.
   */
  skill_libraries?: string[];
  defaults?: DefaultsManifest;
}

interface RuntimeRecord {
  id: string;
  name: string;
  custom_name?: string;
  device_info?: string;
  runtime_mode: string;
  provider: string;
  daemon_id?: string;
}

interface AgentSummary {
  id: string;
  name: string;
  archived_at?: string;
}

interface AgentState extends AgentSummary {
  runtime_id: string;
  description: string;
  instructions: string;
  model: string;
  thinking_level: string;
  service_tier: string;
  custom_args: string[];
  permission_mode: string;
  invocation_targets: { target_type: string }[];
  max_concurrent_tasks: number;
  avatar_url: string;
}

interface ProjectState {
  id: string;
  title: string;
  description: string;
  icon: string;
  status: string;
  start_date: string;
  due_date: string;
  lead_type: string;
  lead_id: string;
}

interface ProjectResourceState {
  id: string;
  resource_type: string;
  label: string;
  ref_label: string;
  ref_key: string;
}

interface LabelState {
  id: string;
  name: string;
  color: string;
  description: string;
  resource_type: string;
}

interface StatusState {
  id: string;
  key: string;
  name: string;
  description: string;
  category: string;
  color: string;
  is_system: boolean;
  archived: boolean;
}

interface SkillState {
  id: string;
  name: string;
  description: string;
}

interface SkillFileState {
  id: string;
  path: string;
  content: string;
  /** sha256 hex of the body; multica 42+ lists files with this and no body. */
  content_hash: string;
}

interface AgentSkillState {
  id: string;
  name: string;
}

interface WorkspaceMemberState {
  user_id: string;
  name: string;
  email: string;
  role: string;
}

interface SquadState {
  id: string;
  name: string;
  description: string;
  instructions: string;
  leader_id: string;
  avatar_url: string;
}

interface SquadMemberState {
  member_id: string;
  member_type: string;
  role: string;
}

interface Plan {
  kind: "label" | "status" | "skill" | "agent" | "agent-skill" | "project" | "squad" | "squad-member" | "gbot-bot";
  action: "create" | "update" | "add" | "set-role" | "remove" | "extra" | "delete";
  name: string;
  changes: string[];
  args: string[];
  /** Set when apply must not run the plan. Sync does not delete or edit locked records. */
  blocked?: string;
  exec?: () => void;
  postArgs?: { label: string; args: string[] }[];
  skill?: { name: string; path: string };
  context?: string[];
  avatarPath?: string;
  avatarHash?: string;
  avatarStatePath?: string;
  avatarKey?: string;
  /** Field-level before/after, printed under the plan line. */
  diff?: FieldDiff[];
  /** Delete plans run only with --apply --prune and a second confirmation. */
  prune?: boolean;
  /** Blocked for a reason outside the manifest (e.g. no orchestrator on the server); reported, not fatal. */
  deferred?: boolean;
}

export interface FieldDiff {
  field: string;
  from: string;
  to: string;
}

const AGENT_KEYS = new Set([
  "name",
  "runtime",
  "gbot",
  "gbot_instructions",
  "role",
  "description",
  "instructions",
  "model",
  "thinking_level",
  "service_tier",
  "custom_args",
  "access",
  "max_concurrent_tasks",
  "avatar",
  "skills",
]);

const SKILL_KEYS = new Set(["name", "description", "path", "bind"]);

const PROJECT_KEYS = new Set([
  "title",
  "description",
  "icon",
  "status",
  "start_date",
  "due_date",
  "lead",
  "repos",
  "resources",
  "orchestrator",
]);

const PROJECT_RESOURCE_KEYS = new Set([
  "type",
  "url",
  "local_path",
  "runtime",
  "execution_mode",
  "label",
]);

const LABEL_KEYS = new Set(["name", "color", "description"]);
const STATUS_KEYS = new Set(["key", "name", "description", "category", "color"]);
const SYSTEM_STATUS_KEYS = new Set([
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
]);

const SQUAD_KEYS = new Set([
  "name",
  "leader",
  "description",
  "instructions",
  "avatar_url",
  "members",
]);

const SQUAD_MEMBER_KEYS = new Set(["type", "ref", "role"]);

const DEFAULTS_KEYS = new Set(["agents", "projects"]);
const AGENT_DEFAULT_KEYS = new Set(["access", "max_concurrent_tasks"]);
const PROJECT_DEFAULT_KEYS = new Set(["status"]);
const MANIFEST_KEYS = new Set([
  "workspace",
  "agents",
  "projects",
  "squads",
  "labels",
  "statuses",
  "skills",
  "skill_libraries",
  "defaults",
]);
const BUNDLE_KEYS = new Set([
  "workspaces",
  "agents",
  "defaults",
  "skills",
  "skill_libraries",
  "labels",
  "statuses",
]);

const PROJECT_STATUSES = new Set(["planned", "in_progress", "paused", "completed", "cancelled"]);
const RESOURCE_TYPES = new Set(["github_repo", "local_directory"]);

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Bun sets `import.meta.dir`. Vitest's transform does not, so fall back to the module URL. */
function scriptDir(): string {
  const dir = (import.meta as { dir?: string }).dir;
  return dir && dir.length > 0 ? dir : dirname(fileURLToPath(import.meta.url));
}

function usage(): never {
  console.error(
    [
      "Usage: bun run multica-config/sync.ts [--apply [--yes] [--prune]] [--check] [--workspace <id|slug>] [--profile <name>] [--global-manifest <path>] [--manifest <file-or-dir>]",
      "Default is a dry run: print the plan and field-level diff, change nothing.",
      "  --apply  apply the plan after confirmation (--yes skips the prompt; required without a TTY)",
      "  --prune  also delete server agents/skills the manifest no longer names (second confirmation)",
      "  --check  dry run that exits 1 when anything would change (CI)",
    ].join("\n"),
  );
  process.exit(2);
}

export function parseArgs(argv: string[] = process.argv.slice(2)) {
  const args = [...argv];
  let check = false;
  let apply = false;
  let yes = false;
  let prune = false;
  let workspace: string | undefined;
  let profile: string | undefined;
  let globalManifest = join(scriptDir(), "global.yaml");
  let manifest = join(scriptDir(), "manifest.yaml");
  while (args.length > 0) {
    const argument = args.shift();
    if (argument === "--check") {
      check = true;
      continue;
    }
    if (argument === "--apply") {
      apply = true;
      continue;
    }
    if (argument === "--yes" || argument === "-y") {
      yes = true;
      continue;
    }
    if (argument === "--prune") {
      prune = true;
      continue;
    }
    if (argument === "--workspace") {
      workspace = args.shift();
      if (!workspace) usage();
      continue;
    }
    if (argument === "--profile") {
      profile = args.shift();
      if (!profile) usage();
      continue;
    }
    if (argument === "--global-manifest") {
      globalManifest = args.shift();
      if (!globalManifest) usage();
      continue;
    }
    if (argument === "--manifest") {
      manifest = args.shift();
      if (!manifest) usage();
      continue;
    }
    usage();
  }
  if (check && apply) {
    console.error("--check and --apply are exclusive");
    usage();
  }
  return { check, apply, yes, prune, workspace, profile, globalManifest, manifest };
}

function runMultica(args: string[]) {
  const result = spawnSync("multica", args, {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
  if (result.status !== 0) {
    const detail = (result.stderr || result.stdout || "").trim();
    const command = args
      .filter((argument) => !argument.startsWith("-"))
      .slice(0, 3)
      .join(" ");
    throw new Error(`multica ${command} failed${detail ? `: ${detail}` : ""}`);
  }
  return result.stdout.trim();
}

function parseJSONObject(output: string, label: string): JsonObject {
  let parsed: unknown;
  try {
    parsed = JSON.parse(output);
  } catch (error) {
    throw new Error(
      `Cannot parse ${label} JSON: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  return object(parsed, label);
}

function parseJSONArray<T>(output: string, label: string): T[] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(output);
  } catch (error) {
    throw new Error(
      `Cannot parse ${label} JSON: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  if (!Array.isArray(parsed)) throw new Error(`${label} must be a JSON array`);
  return parsed as T[];
}

function object(value: unknown, label: string): JsonObject {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${label} must be an object`);
  }
  return value as JsonObject;
}

function array(value: unknown, label: string): unknown[] {
  if (!Array.isArray(value)) throw new Error(`${label} must be an array`);
  return value;
}

function requiredString(value: unknown, label: string): string {
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`${label} must be a non-empty string`);
  }
  return value;
}

function optionalString(value: unknown, label: string): string | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value !== "string") throw new Error(`${label} must be a string`);
  return value;
}

function optionalStringArray(value: unknown, label: string): string[] | undefined {
  if (value === undefined || value === null) return undefined;
  return array(value, label).map((entry, index) => requiredString(entry, `${label}[${index}]`));
}

function assertKnownKeys(value: JsonObject, allowed: Set<string>, label: string) {
  const unknown = Object.keys(value).filter((key) => !allowed.has(key));
  if (unknown.length > 0) throw new Error(`${label}: unknown key(s): ${unknown.join(", ")}`);
}

function parseAgentManifest(value: unknown, index: number): AgentManifest {
  const label = `agents[${index}]`;
  const raw = object(value, label);
  assertKnownKeys(raw, AGENT_KEYS, label);
  const access = optionalString(raw.access, `${label}.access`);
  if (access !== undefined && access !== "private" && access !== "workspace") {
    throw new Error(`${label}.access must be private or workspace`);
  }
  const maxConcurrent = raw.max_concurrent_tasks;
  if (maxConcurrent !== undefined && maxConcurrent !== null) {
    if (typeof maxConcurrent !== "number" || !Number.isInteger(maxConcurrent)) {
      throw new Error(`${label}.max_concurrent_tasks must be an integer`);
    }
  }
  return {
    name: requiredString(raw.name, `${label}.name`),
    runtime: requiredString(raw.runtime, `${label}.runtime`),
    gbot: optionalString(raw.gbot, `${label}.gbot`),
    gbot_instructions: optionalString(raw.gbot_instructions, `${label}.gbot_instructions`),
    role: optionalString(raw.role, `${label}.role`),
    description: optionalString(raw.description, `${label}.description`),
    instructions: optionalString(raw.instructions, `${label}.instructions`),
    model: optionalString(raw.model, `${label}.model`),
    thinking_level: optionalString(raw.thinking_level, `${label}.thinking_level`),
    service_tier: optionalString(raw.service_tier, `${label}.service_tier`),
    custom_args: optionalStringArray(raw.custom_args, `${label}.custom_args`),
    access,
    max_concurrent_tasks:
      maxConcurrent === undefined || maxConcurrent === null ? undefined : (maxConcurrent as number),
    avatar: optionalString(raw.avatar, `${label}.avatar`),
    skills: optionalStringArray(raw.skills, `${label}.skills`),
  };
}

function parseProjectResourceManifest(value: unknown, index: number): ProjectResourceManifest {
  const label = `projects.resources[${index}]`;
  const raw = object(value, label);
  assertKnownKeys(raw, PROJECT_RESOURCE_KEYS, label);
  const type = optionalString(raw.type, `${label}.type`) ?? "github_repo";
  if (!RESOURCE_TYPES.has(type)) {
    throw new Error(`${label}.type must be one of ${[...RESOURCE_TYPES].join(", ")}`);
  }
  const executionMode = optionalString(raw.execution_mode, `${label}.execution_mode`);
  if (executionMode !== undefined && executionMode !== "in_place" && executionMode !== "worktree") {
    throw new Error(`${label}.execution_mode must be in_place or worktree`);
  }
  return {
    type: type as ProjectResourceManifest["type"],
    url: optionalString(raw.url, `${label}.url`),
    local_path: optionalString(raw.local_path, `${label}.local_path`),
    runtime: optionalString(raw.runtime, `${label}.runtime`),
    execution_mode: executionMode as ProjectResourceManifest["execution_mode"],
    label: optionalString(raw.label, `${label}.label`),
  };
}

function parseProjectManifest(value: unknown, index: number): ProjectManifest {
  const label = `projects[${index}]`;
  const raw = object(value, label);
  assertKnownKeys(raw, PROJECT_KEYS, label);
  const status = optionalString(raw.status, `${label}.status`);
  if (status !== undefined && !PROJECT_STATUSES.has(status)) {
    throw new Error(`${label}.status must be one of ${[...PROJECT_STATUSES].join(", ")}`);
  }
  for (const key of ["start_date", "due_date"] as const) {
    const value = optionalString(raw[key], `${label}.${key}`);
    if (value !== undefined && !/^\d{4}-\d{2}-\d{2}$/.test(value)) {
      throw new Error(`${label}.${key} must be YYYY-MM-DD`);
    }
  }
  return {
    title: requiredString(raw.title, `${label}.title`),
    description: optionalString(raw.description, `${label}.description`),
    icon: optionalString(raw.icon, `${label}.icon`),
    status: status as ProjectManifest["status"],
    start_date: optionalString(raw.start_date, `${label}.start_date`),
    due_date: optionalString(raw.due_date, `${label}.due_date`),
    lead: optionalString(raw.lead, `${label}.lead`),
    repos: optionalStringArray(raw.repos, `${label}.repos`),
    resources: optionalArray(raw.resources, `${label}.resources`)?.map((entry, resourceIndex) =>
      parseProjectResourceManifest(entry, resourceIndex),
    ),
    orchestrator:
      raw.orchestrator === undefined || raw.orchestrator === null
        ? undefined
        : object(raw.orchestrator, `${label}.orchestrator`),
  };
}

function parseLabelManifest(value: unknown, index: number): LabelManifest {
  const label = `labels[${index}]`;
  const raw = object(value, label);
  assertKnownKeys(raw, LABEL_KEYS, label);
  return {
    name: requiredString(raw.name, `${label}.name`),
    color: requiredString(raw.color, `${label}.color`),
    description: optionalString(raw.description, `${label}.description`),
  };
}

function parseStatusManifest(value: unknown, index: number): StatusManifest {
  const label = `statuses[${index}]`;
  const raw = object(value, label);
  assertKnownKeys(raw, STATUS_KEYS, label);
  return {
    key: requiredString(raw.key, `${label}.key`),
    name: requiredString(raw.name, `${label}.name`),
    description: optionalString(raw.description, `${label}.description`),
    category: requiredString(raw.category, `${label}.category`),
    color: requiredString(raw.color, `${label}.color`),
  };
}

function parseSkillManifest(value: unknown, index: number): SkillManifest {
  const label = `skills[${index}]`;
  const raw = object(value, label);
  assertKnownKeys(raw, SKILL_KEYS, label);
  const bind = optionalString(raw.bind, `${label}.bind`);
  if (bind !== undefined && bind !== "all" && bind !== "listed") {
    throw new Error(`${label}.bind must be all or listed`);
  }
  return {
    name: requiredString(raw.name, `${label}.name`),
    description: optionalString(raw.description, `${label}.description`),
    path: optionalString(raw.path, `${label}.path`),
    bind,
  };
}

function parseSquadMemberManifest(value: unknown, index: number): SquadMemberManifest {
  const label = `squads.members[${index}]`;
  const raw = object(value, label);
  assertKnownKeys(raw, SQUAD_MEMBER_KEYS, label);
  const type = optionalString(raw.type, `${label}.type`);
  if (type !== "agent" && type !== "member") {
    throw new Error(`${label}.type must be agent or member`);
  }
  return {
    type,
    ref: requiredString(raw.ref, `${label}.ref`),
    role: optionalString(raw.role, `${label}.role`) ?? "member",
  };
}

function parseSquadManifest(value: unknown, index: number): SquadManifest {
  const label = `squads[${index}]`;
  const raw = object(value, label);
  assertKnownKeys(raw, SQUAD_KEYS, label);
  return {
    name: requiredString(raw.name, `${label}.name`),
    leader: requiredString(raw.leader, `${label}.leader`),
    description: optionalString(raw.description, `${label}.description`),
    instructions: optionalString(raw.instructions, `${label}.instructions`),
    avatar_url: optionalString(raw.avatar_url, `${label}.avatar_url`),
    members: optionalArray(raw.members, `${label}.members`)?.map((entry, memberIndex) =>
      parseSquadMemberManifest(entry, memberIndex),
    ),
  };
}

function optionalArray(value: unknown, label: string): unknown[] | undefined {
  if (value === undefined || value === null) return undefined;
  return array(value, label);
}

function parseDefaultsManifest(value: unknown): DefaultsManifest | undefined {
  if (value === undefined || value === null) return undefined;
  const label = "defaults";
  const raw = object(value, label);
  assertKnownKeys(raw, DEFAULTS_KEYS, label);
  const agents =
    raw.agents === undefined || raw.agents === null
      ? undefined
      : object(raw.agents, `${label}.agents`);
  if (agents) assertKnownKeys(agents, AGENT_DEFAULT_KEYS, `${label}.agents`);
  const projects =
    raw.projects === undefined || raw.projects === null
      ? undefined
      : object(raw.projects, `${label}.projects`);
  if (projects) assertKnownKeys(projects, PROJECT_DEFAULT_KEYS, `${label}.projects`);
  const access = optionalString(agents?.access, `${label}.agents.access`);
  if (access !== undefined && access !== "private" && access !== "workspace") {
    throw new Error(`${label}.agents.access must be private or workspace`);
  }
  return {
    agents: agents
      ? {
          access: access as "private" | "workspace",
          max_concurrent_tasks:
            agents.max_concurrent_tasks === undefined || agents.max_concurrent_tasks === null
              ? undefined
              : (agents.max_concurrent_tasks as number),
        }
      : undefined,
    projects: projects
      ? {
          status: optionalString(
            projects.status,
            `${label}.projects.status`,
          ) as ProjectManifest["status"],
        }
      : undefined,
  };
}

function parseManifestObject(root: JsonObject, label: string): MulticaManifest {
  assertKnownKeys(root, MANIFEST_KEYS, label);
  return {
    workspace: optionalString(root.workspace, `${label}.workspace`),
    agents: array(root.agents ?? [], `${label}.agents`).map((entry, index) =>
      parseAgentManifest(entry, index),
    ),
    projects: array(root.projects ?? [], `${label}.projects`).map((entry, index) =>
      parseProjectManifest(entry, index),
    ),
    squads: array(root.squads ?? [], `${label}.squads`).map((entry, index) =>
      parseSquadManifest(entry, index),
    ),
    labels: array(root.labels ?? [], `${label}.labels`).map((entry, index) =>
      parseLabelManifest(entry, index),
    ),
    statuses: array(root.statuses ?? [], `${label}.statuses`).map((entry, index) =>
      parseStatusManifest(entry, index),
    ),
    skills: array(root.skills ?? [], `${label}.skills`).map((entry, index) =>
      parseSkillManifest(entry, index),
    ),
    skill_libraries: optionalStringArray(root.skill_libraries, `${label}.skill_libraries`),
    defaults: parseDefaultsManifest(root.defaults),
  };
}

export function parseManifest(text: string): MulticaManifest {
  const root = object(loadYaml(text, { schema: JSON_SCHEMA }), "manifest");
  if (root.workspaces !== undefined) {
    throw new Error("manifest declares workspaces; use parseManifestDocuments");
  }
  return parseManifestObject(root, "manifest");
}

/** One file may declare several workspaces. A file without `workspaces` is a single manifest. */
export function parseManifestDocuments(text: string): MulticaManifest[] {
  const root = object(loadYaml(text, { schema: JSON_SCHEMA }), "manifest");
  if (root.workspaces === undefined) return [parseManifestObject(root, "manifest")];
  assertKnownKeys(root, BUNDLE_KEYS, "manifest");
  const shared = parseManifestObject(
    {
      agents: root.agents,
      defaults: root.defaults,
      skills: root.skills,
      labels: root.labels,
      statuses: root.statuses,
    },
    "manifest",
  );
  const sections = array(root.workspaces, "manifest.workspaces");
  if (sections.length === 0) throw new Error("manifest.workspaces is empty");
  return sections.map((section, index) => {
    const label = `workspaces[${index}]`;
    const raw = object(section, label);
    if (typeof raw.workspace !== "string" || raw.workspace.trim() === "") {
      throw new Error(`${label}.workspace is required`);
    }
    return mergeManifests(shared, parseManifestObject(raw, label));
  });
}

/** Directory a manifest's `.` refers to: the repo that contains `.multica`, otherwise the manifest directory. */
export function manifestProjectRoot(sourceDir: string): string {
  return basename(sourceDir) === ".multica" ? dirname(sourceDir) : sourceDir;
}

/**
 * Paths written in a manifest, turned into the absolute path Multica stores.
 * `.` is the project root. `~/...` is the syncing user's home. An absolute path stays as written.
 */
export function resolveLocalDirectoryPath(localPath: string, sourceDir: string): string {
  const trimmed = localPath.trim();
  const root = manifestProjectRoot(sourceDir);
  if (trimmed === "." || trimmed === "./") return resolve(root);
  if (trimmed === "~") return homedir();
  if (trimmed.startsWith("~/") || trimmed.startsWith("~\\")) {
    return resolve(homedir(), trimmed.slice(2));
  }
  if (trimmed.startsWith("./") || trimmed.startsWith("../")) return resolve(root, trimmed);
  return trimmed;
}

export function resolveManifestSources(
  manifest: MulticaManifest,
  sourceDir: string,
): MulticaManifest {
  return {
    ...manifest,
    agents: manifest.agents.map((agent) => ({
      ...agent,
      avatar: agent.avatar ? resolve(sourceDir, agent.avatar) : undefined,
    })),
    skills: manifest.skills.map((skill) => ({
      ...skill,
      path: skill.path ? resolve(sourceDir, skill.path) : undefined,
    })),
    skill_libraries: manifest.skill_libraries?.map((library) =>
      resolveLocalDirectoryPath(library, sourceDir),
    ),
    projects: manifest.projects.map((project) => ({
      ...project,
      resources: project.resources?.map((resource) =>
        resource.local_path
          ? { ...resource, local_path: resolveLocalDirectoryPath(resource.local_path, sourceDir) }
          : resource,
      ),
    })),
  };
}

function mergeByKey<T>(globalItems: T[], projectItems: T[], key: keyof T): T[] {
  const merged = new Map<string, T>();
  for (const item of globalItems) merged.set(String(item[key]), item);
  for (const item of projectItems) merged.set(String(item[key]), item);
  return [...merged.values()];
}

/** Project wins field-by-field so it can promote `bind` without dropping `path`. */
function mergeSkills(globalItems: SkillManifest[], projectItems: SkillManifest[]): SkillManifest[] {
  const merged = new Map<string, SkillManifest>();
  for (const item of globalItems) merged.set(item.name, item);
  for (const item of projectItems) {
    const previous = merged.get(item.name);
    merged.set(
      item.name,
      previous
        ? {
            ...previous,
            ...item,
            description: item.description ?? previous.description,
            path: item.path ?? previous.path,
            bind: item.bind ?? previous.bind,
          }
        : item,
    );
  }
  return [...merged.values()];
}

export function mergeManifests(global: MulticaManifest, project: MulticaManifest): MulticaManifest {
  return {
    defaults: {
      agents: {
        access: project.defaults?.agents?.access ?? global.defaults?.agents?.access,
        max_concurrent_tasks:
          project.defaults?.agents?.max_concurrent_tasks ??
          global.defaults?.agents?.max_concurrent_tasks,
      },
      projects: {
        status: project.defaults?.projects?.status ?? global.defaults?.projects?.status,
      },
    },
    workspace: project.workspace ?? global.workspace,
    labels: mergeByKey(global.labels, project.labels, "name"),
    statuses: mergeByKey(global.statuses, project.statuses, "key"),
    skills: mergeSkills(global.skills, project.skills),
    skill_libraries: uniqueStrings([...(global.skill_libraries ?? []), ...(project.skill_libraries ?? [])]),
    agents: mergeByKey(global.agents, project.agents, "name"),
    projects: mergeByKey(global.projects, project.projects, "title"),
    squads: mergeByKey(global.squads, project.squads, "name"),
  };
}

export function applyDefaults(manifest: MulticaManifest): MulticaManifest {
  const agentDefaults = manifest.defaults?.agents;
  const projectDefaults = manifest.defaults?.projects;
  return {
    ...manifest,
    agents: manifest.agents.map((agent) => ({
      ...agent,
      access: agent.access ?? agentDefaults?.access,
      max_concurrent_tasks: agent.max_concurrent_tasks ?? agentDefaults?.max_concurrent_tasks,
    })),
    projects: manifest.projects.map((project) => ({
      ...project,
      status: project.status ?? projectDefaults?.status,
    })),
  };
}

function uniqueStrings(values: string[]): string[] {
  return [...new Set(values)];
}

/** Official worker bind = catalog `bind: all` ∪ that agent's listed skills. */
export function applyInheritedSkills(manifest: MulticaManifest): MulticaManifest {
  const shared = manifest.skills.filter((skill) => skill.bind === "all").map((skill) => skill.name);
  return {
    ...manifest,
    agents: manifest.agents.map((agent) => ({
      ...agent,
      skills: uniqueStrings([...shared, ...(agent.skills ?? [])]),
    })),
  };
}

export function officialSkillsForAgent(manifest: MulticaManifest, agentName: string): string[] {
  const agent = manifest.agents.find((candidate) => candidate.name === agentName);
  if (!agent) throw new Error(`agent not found: ${agentName}`);
  return [...(agent.skills ?? [])];
}

/** Declares, from the skill libraries, every agent skill the catalog does not have. */
export function resolveSkillLibraries(manifest: MulticaManifest): MulticaManifest {
  const libraries = manifest.skill_libraries ?? [];
  const known = new Set(manifest.skills.map((skill) => skill.name));
  const added: SkillManifest[] = [];
  const missing: string[] = [];
  for (const agent of manifest.agents) {
    for (const name of agent.skills ?? []) {
      if (known.has(name)) continue;
      const dir = libraries.map((library) => join(library, name)).find((candidate) => existsSync(join(candidate, "SKILL.md")));
      if (!dir) {
        if (!missing.includes(name)) missing.push(name);
        continue;
      }
      known.add(name);
      added.push({ name, path: dir, bind: "listed", description: skillDescription(join(dir, "SKILL.md")) });
    }
  }
  if (missing.length > 0 && libraries.length > 0 && libraries.some((library) => !existsSync(library))) {
    throw new Error(
      `skills ${missing.join(", ")} not found; skill library missing: ${libraries.filter((library) => !existsSync(library)).join(", ")}. Export it with \`multica team preset export <dir>\` (bun run export:multica:preset).`,
    );
  }
  return added.length === 0 ? manifest : { ...manifest, skills: [...manifest.skills, ...added] };
}

function skillDescription(file: string): string | undefined {
  const text = readFileSync(file, "utf8");
  const front = /^---\n([\s\S]*?)\n---/.exec(text);
  if (!front) return undefined;
  try {
    const parsed = loadYaml(front[1], { schema: JSON_SCHEMA }) as JsonObject;
    return typeof parsed?.description === "string" ? parsed.description : undefined;
  } catch {
    return undefined;
  }
}

export function validateManifest(manifest: MulticaManifest) {
  const agentNames = new Set<string>();
  for (const agent of manifest.agents) {
    if (agentNames.has(agent.name)) throw new Error(`duplicate agent name: ${agent.name}`);
    agentNames.add(agent.name);
    if (agent.max_concurrent_tasks !== undefined) {
      if (agent.max_concurrent_tasks < 1 || agent.max_concurrent_tasks > 50) {
        throw new Error(`agents.${agent.name}.max_concurrent_tasks must be between 1 and 50`);
      }
    }
  }
  const projectTitles = new Set<string>();
  for (const project of manifest.projects) {
    if (projectTitles.has(project.title))
      throw new Error(`duplicate project title: ${project.title}`);
    projectTitles.add(project.title);
    for (const repo of project.repos ?? []) {
      if (!/^https?:\/\/\S+$/.test(repo)) {
        throw new Error(`projects.${project.title}.repos contains invalid URL: ${repo}`);
      }
    }
    for (const resource of project.resources ?? []) {
      if (resource.type === "github_repo" && !resource.url) {
        throw new Error(`projects.${project.title}.resources: github_repo requires url`);
      }
      if (resource.type === "local_directory") {
        if (!resource.local_path) {
          throw new Error(
            `projects.${project.title}.resources: local_directory requires local_path`,
          );
        }
        if (!resource.runtime) {
          throw new Error(`projects.${project.title}.resources: local_directory requires runtime`);
        }
      }
    }
    if (project.lead !== undefined && !agentNames.has(project.lead)) {
      throw new Error(`projects.${project.title}.lead agent not found in agents: ${project.lead}`);
    }
  }
  const labelNames = new Set<string>();
  for (const label of manifest.labels) {
    if (labelNames.has(label.name)) throw new Error(`duplicate label name: ${label.name}`);
    labelNames.add(label.name);
    if (!/^#[0-9a-fA-F]{6}$/.test(label.color)) {
      throw new Error(`labels.${label.name}.color must be a hex color like #3b82f6`);
    }
  }
  const statusKeys = new Set<string>();
  for (const status of manifest.statuses) {
    if (statusKeys.has(status.key)) throw new Error(`duplicate status key: ${status.key}`);
    statusKeys.add(status.key);
    if (!/^[a-z0-9][a-z0-9_-]*$/.test(status.key)) {
      throw new Error(`statuses.${status.key}.key must be a lowercase slug`);
    }
    if (!/^#[0-9a-fA-F]{6}$/.test(status.color)) {
      throw new Error(`statuses.${status.key}.color must be a hex color like #3b82f6`);
    }
  }
  const skillNames = new Set<string>();
  for (const skill of manifest.skills) {
    if (skillNames.has(skill.name)) throw new Error(`duplicate skill name: ${skill.name}`);
    skillNames.add(skill.name);
    if (skill.path && !existsSync(join(skill.path, "SKILL.md"))) {
      throw new Error(`skills.${skill.name}.path does not contain SKILL.md: ${skill.path}`);
    }
  }
  for (const agent of manifest.agents) {
    for (const skill of agent.skills ?? []) {
      if (!skillNames.has(skill)) {
        throw new Error(`agents.${agent.name}.skills references unknown skill: ${skill}`);
      }
    }
  }
  const squadNames = new Set<string>();
  for (const squad of manifest.squads) {
    if (squadNames.has(squad.name)) throw new Error(`duplicate squad name: ${squad.name}`);
    squadNames.add(squad.name);
    if (!agentNames.has(squad.leader)) {
      throw new Error(`squads.${squad.name}.leader agent not found in agents: ${squad.leader}`);
    }
    for (const member of squad.members ?? []) {
      if (member.type === "agent" && !agentNames.has(member.ref)) {
        throw new Error(`squads.${squad.name}.members agent not found in agents: ${member.ref}`);
      }
    }
  }
}

function contextArgs(workspaceId: string | undefined, profile: string | undefined) {
  const args: string[] = [];
  if (profile) args.push("--profile", profile);
  if (workspaceId) args.push("--workspace-id", workspaceId);
  return args;
}

function isUUID(value: string) {
  return UUID_PATTERN.test(value);
}

function resolveWorkspaceId(workspace: string | undefined, profile: string | undefined) {
  if (!workspace) return undefined;
  if (isUUID(workspace)) return workspace;
  const args = [
    "workspace",
    "list",
    "--output",
    "json",
    ...(profile ? ["--profile", profile] : []),
  ];
  const workspaces = parseJSONArray<JsonObject>(runMultica(args), "workspaces");
  const match = workspaces.find((entry) => object(entry, "workspace").slug === workspace);
  if (!match) throw new Error(`workspace not found: ${workspace}`);
  return requiredString(object(match, "workspace").id, "workspace.id");
}

function toRuntime(value: unknown): RuntimeRecord {
  const raw = object(value, "runtime");
  return {
    id: requiredString(raw.id, "runtime.id"),
    name: optionalString(raw.name, "runtime.name") ?? "",
    custom_name: optionalString(raw.custom_name, "runtime.custom_name"),
    device_info: optionalString(raw.device_info, "runtime.device_info"),
    runtime_mode: optionalString(raw.runtime_mode, "runtime.runtime_mode") ?? "",
    provider: optionalString(raw.provider, "runtime.provider") ?? "",
    daemon_id: optionalString(raw.daemon_id, "runtime.daemon_id"),
  };
}

export function resolveRuntimeId(runtimes: RuntimeRecord[], selector: string): string {
  const normalized = selector.trim().toLowerCase();
  const byId = runtimes.find(
    (runtime) => runtime.id === selector || runtime.id.toLowerCase() === normalized,
  );
  if (byId) return byId.id;
  const matches = runtimes.filter((runtime) => {
    const candidates = [
      runtime.name,
      runtime.custom_name ?? "",
      runtime.device_info ?? "",
      runtime.runtime_mode,
      runtime.provider,
    ].map((value) => value.toLowerCase());
    if (candidates.includes(normalized)) return true;
    const parts = normalized.split(/[:/]/, 2);
    if (parts.length === 2) {
      const [device, provider] = parts;
      const deviceNames = [runtime.name, runtime.custom_name ?? "", runtime.device_info ?? ""]
        .join(" ")
        .toLowerCase();
      return (
        deviceNames.includes(device!) &&
        [runtime.provider, runtime.runtime_mode].some((value) =>
          value.toLowerCase().includes(provider!),
        )
      );
    }
    return false;
  });
  if (matches.length === 1) return matches[0]!.id;
  if (matches.length > 1) {
    throw new Error(
      `runtime selector ${selector} is ambiguous: ${matches
        .map((runtime) => runtime.custom_name ?? (runtime.name || runtime.provider))
        .join(", ")}`,
    );
  }
  throw new Error(
    `runtime not found: ${selector} (available: ${
      runtimes
        .map((runtime) => runtime.custom_name ?? (runtime.name || runtime.provider))
        .join(", ") || "none"
    })`,
  );
}

function toAgentSummary(value: unknown): AgentSummary {
  const raw = object(value, "agent");
  return {
    id: requiredString(raw.id, "agent.id"),
    name: requiredString(raw.name, "agent.name"),
    archived_at: optionalString(raw.archived_at, "agent.archived_at"),
  };
}

function toAgentState(value: unknown): AgentState {
  const raw = object(value, "agent");
  const invocationTargets = array(raw.invocation_targets ?? [], "agent.invocation_targets").map(
    (target) => {
      const targetObject = object(target, "agent.invocation_targets[]");
      return {
        target_type:
          optionalString(targetObject.target_type, "invocation_target.target_type") ?? "",
      };
    },
  );
  return {
    id: requiredString(raw.id, "agent.id"),
    name: requiredString(raw.name, "agent.name"),
    runtime_id: optionalString(raw.runtime_id, "agent.runtime_id") ?? "",
    description: optionalString(raw.description, "agent.description") ?? "",
    instructions: optionalString(raw.instructions, "agent.instructions") ?? "",
    model: optionalString(raw.model, "agent.model") ?? "",
    thinking_level: optionalString(raw.thinking_level, "agent.thinking_level") ?? "",
    service_tier: optionalString(raw.service_tier, "agent.service_tier") ?? "",
    custom_args: optionalStringArray(raw.custom_args, "agent.custom_args") ?? [],
    permission_mode: optionalString(raw.permission_mode, "agent.permission_mode") ?? "private",
    invocation_targets: invocationTargets,
    max_concurrent_tasks:
      typeof raw.max_concurrent_tasks === "number" ? raw.max_concurrent_tasks : 0,
    avatar_url: optionalString(raw.avatar_url, "agent.avatar_url") ?? "",
    archived_at: optionalString(raw.archived_at, "agent.archived_at"),
  };
}

function toProjectState(value: unknown): ProjectState {
  const raw = object(value, "project");
  return {
    id: requiredString(raw.id, "project.id"),
    title: requiredString(raw.title, "project.title"),
    description: optionalString(raw.description, "project.description") ?? "",
    icon: optionalString(raw.icon, "project.icon") ?? "",
    status: optionalString(raw.status, "project.status") ?? "",
    start_date: optionalString(raw.start_date, "project.start_date") ?? "",
    due_date: optionalString(raw.due_date, "project.due_date") ?? "",
    lead_type: optionalString(raw.lead_type, "project.lead_type") ?? "",
    lead_id: optionalString(raw.lead_id, "project.lead_id") ?? "",
  };
}

function toLabelState(value: unknown): LabelState {
  const raw = object(value, "label");
  return {
    id: requiredString(raw.id, "label.id"),
    name: requiredString(raw.name, "label.name"),
    color: optionalString(raw.color, "label.color") ?? "",
    description: optionalString(raw.description, "label.description") ?? "",
    resource_type: optionalString(raw.resource_type, "label.resource_type") ?? "issue",
  };
}

function toStatusState(value: unknown): StatusState {
  const raw = object(value, "status");
  return {
    id: requiredString(raw.id, "status.id"),
    key: requiredString(raw.key, "status.key"),
    name: requiredString(raw.name, "status.name"),
    description: optionalString(raw.description, "status.description") ?? "",
    category: requiredString(raw.category, "status.category"),
    color: optionalString(raw.color, "status.color") ?? "",
    is_system: raw.is_system === true,
    archived: raw.archived_at !== undefined && raw.archived_at !== null,
  };
}

function toSkillState(value: unknown): SkillState {
  const raw = object(value, "skill");
  return {
    id: requiredString(raw.id, "skill.id"),
    name: requiredString(raw.name, "skill.name"),
    description: optionalString(raw.description, "skill.description") ?? "",
  };
}

function toSkillFileState(value: unknown): SkillFileState {
  const raw = object(value, "skill file");
  return {
    id: requiredString(raw.id, "skill file.id"),
    path: requiredString(raw.path, "skill file.path"),
    content: optionalString(raw.content, "skill file.content") ?? "",
    content_hash: optionalString(raw.content_hash, "skill file.content_hash") ?? "",
  };
}

function toAgentSkillState(value: unknown): AgentSkillState {
  const raw = object(value, "agent skill");
  return {
    id: requiredString(raw.id, "agent skill.id"),
    name: optionalString(raw.name, "agent skill.name") ?? "",
  };
}

function toWorkspaceMemberState(value: unknown): WorkspaceMemberState {
  const raw = object(value, "member");
  return {
    user_id: requiredString(raw.user_id, "member.user_id"),
    name: optionalString(raw.name, "member.name") ?? "",
    email: optionalString(raw.email, "member.email") ?? "",
    role: optionalString(raw.role, "member.role") ?? "",
  };
}

function toSquadState(value: unknown): SquadState {
  const raw = object(value, "squad");
  return {
    id: requiredString(raw.id, "squad.id"),
    name: requiredString(raw.name, "squad.name"),
    description: optionalString(raw.description, "squad.description") ?? "",
    instructions: optionalString(raw.instructions, "squad.instructions") ?? "",
    leader_id: optionalString(raw.leader_id, "squad.leader_id") ?? "",
    avatar_url: optionalString(raw.avatar_url, "squad.avatar_url") ?? "",
  };
}

function toSquadMemberState(value: unknown): SquadMemberState {
  const raw = object(value, "squad member");
  return {
    member_id: requiredString(raw.member_id, "squad member.member_id"),
    member_type: optionalString(raw.member_type, "squad member.member_type") ?? "agent",
    role: optionalString(raw.role, "squad member.role") ?? "member",
  };
}

function toProjectResourceState(value: unknown): ProjectResourceState {
  const raw = object(value, "project resource");
  const ref =
    raw.resource_ref === undefined || raw.resource_ref === null
      ? {}
      : object(raw.resource_ref, "project resource.ref");
  const refKey =
    typeof ref.url === "string"
      ? ref.url
      : typeof ref.local_path === "string" && typeof ref.daemon_id === "string"
        ? `${ref.daemon_id}:${ref.local_path}`
        : JSON.stringify(ref);
  return {
    id: requiredString(raw.id, "project resource.id"),
    resource_type: optionalString(raw.resource_type, "project resource.resource_type") ?? "",
    label: optionalString(raw.label, "project resource.label") ?? "",
    ref_label: typeof ref.label === "string" ? ref.label : "",
    ref_key: refKey,
  };
}

function sameAccess(existing: AgentState, access: "private" | "workspace") {
  if (access === "private") return existing.permission_mode === "private";
  return (
    existing.permission_mode === "public_to" &&
    existing.invocation_targets.some((target) => target.target_type === "workspace")
  );
}

export function agentDiff(
  existing: AgentState,
  desired: AgentManifest,
  runtimeId: string,
): string[] {
  const changes = new Set<string>();
  if (existing.runtime_id !== runtimeId) changes.add("runtime");
  if (desired.description !== undefined && existing.description !== desired.description) {
    changes.add("description");
  }
  if (desired.instructions !== undefined && existing.instructions !== desired.instructions) {
    changes.add("instructions");
  }
  if (desired.model !== undefined && existing.model !== desired.model) changes.add("model");
  if (desired.thinking_level !== undefined && existing.thinking_level !== desired.thinking_level) {
    changes.add("thinking-level");
  }
  if (desired.service_tier !== undefined && existing.service_tier !== desired.service_tier) {
    changes.add("service-tier");
  }
  if (
    (desired.custom_args !== undefined || desired.gbot !== undefined) &&
    JSON.stringify(existing.custom_args) !== JSON.stringify(gbotCustomArgs(desired))
  ) {
    changes.add(desired.gbot !== undefined ? "gbot" : "custom-args");
  }
  if (desired.access !== undefined && !sameAccess(existing, desired.access)) {
    changes.add("access");
  }
  if (
    desired.max_concurrent_tasks !== undefined &&
    existing.max_concurrent_tasks !== desired.max_concurrent_tasks
  ) {
    changes.add("max-concurrent-tasks");
  }
  return [...changes];
}

/** Before/after for every field agentDiff reports. */
export function agentFieldDiff(
  existing: AgentState,
  desired: AgentManifest,
  runtimeId: string,
  changes: string[],
): FieldDiff[] {
  const values: Record<string, [unknown, unknown]> = {
    runtime: [existing.runtime_id, runtimeId],
    description: [existing.description, desired.description],
    instructions: [existing.instructions, desired.instructions],
    model: [existing.model, desired.model],
    "thinking-level": [existing.thinking_level, desired.thinking_level],
    "service-tier": [existing.service_tier, desired.service_tier],
    gbot: [existing.custom_args.join(" "), gbotCustomArgs(desired).join(" ")],
    "custom-args": [existing.custom_args.join(" "), gbotCustomArgs(desired).join(" ")],
    access: [existing.permission_mode, desired.access],
    "max-concurrent-tasks": [existing.max_concurrent_tasks, desired.max_concurrent_tasks],
  };
  return changes
    .filter((change) => values[change] !== undefined)
    .map((change) => ({
      field: change,
      from: String(values[change][0] ?? ""),
      to: String(values[change][1] ?? ""),
    }));
}

export function gbotCustomArgs(desired: AgentManifest): string[] {
  const args = [...(desired.custom_args ?? [])].filter(
    (arg) => !arg.startsWith("MULTICA_GBOT_BOT="),
  );
  if (desired.gbot) args.push(`MULTICA_GBOT_BOT=${desired.gbot}`);
  return args;
}

function appendOptional(args: string[], flag: string, value: string | undefined) {
  if (value !== undefined && value !== "") args.push(flag, value);
}

function appendAccess(args: string[], access: "private" | "workspace") {
  if (access === "private") {
    args.push("--permission-mode", "private");
  } else {
    args.push("--permission-mode", "public_to", "--public-to-workspace");
  }
}

export function agentCreateArgs(
  desired: AgentManifest,
  runtimeId: string,
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  const args = [
    "agent",
    "create",
    "--name",
    desired.name,
    "--runtime-id",
    runtimeId,
    "--output",
    "json",
  ];
  appendOptional(args, "--description", desired.description);
  appendOptional(args, "--instructions", desired.instructions);
  appendOptional(args, "--model", desired.model);
  appendOptional(args, "--thinking-level", desired.thinking_level);
  appendOptional(args, "--service-tier", desired.service_tier);
  const customArgs = gbotCustomArgs(desired);
  if (customArgs.length > 0) {
    args.push("--custom-args", JSON.stringify(customArgs));
  }
  if (desired.access) appendAccess(args, desired.access);
  if (desired.max_concurrent_tasks !== undefined) {
    args.push("--max-concurrent-tasks", String(desired.max_concurrent_tasks));
  }
  args.push(...contextArgs(workspaceId, profile));
  return args;
}

export function agentUpdateArgs(
  agentId: string,
  desired: AgentManifest,
  runtimeId: string,
  changes: string[],
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  const args = ["agent", "update", agentId, "--output", "json"];
  const changed = new Set(changes);
  if (changed.has("runtime")) args.push("--runtime-id", runtimeId);
  if (changed.has("description") && desired.description !== undefined) {
    args.push("--description", desired.description);
  }
  if (changed.has("instructions") && desired.instructions !== undefined) {
    args.push("--instructions", desired.instructions);
  }
  if (changed.has("model") && desired.model !== undefined) args.push("--model", desired.model);
  if (changed.has("thinking-level") && desired.thinking_level !== undefined) {
    args.push("--thinking-level", desired.thinking_level);
  }
  if (changed.has("service-tier") && desired.service_tier !== undefined) {
    args.push("--service-tier", desired.service_tier);
  }
  if (changed.has("custom-args") || changed.has("gbot")) {
    args.push("--custom-args", JSON.stringify(gbotCustomArgs(desired)));
  }
  if (changed.has("access") && desired.access) appendAccess(args, desired.access);
  if (changed.has("max-concurrent-tasks") && desired.max_concurrent_tasks !== undefined) {
    args.push("--max-concurrent-tasks", String(desired.max_concurrent_tasks));
  }
  args.push(...contextArgs(workspaceId, profile));
  return args;
}

export function projectDiff(
  existing: ProjectState,
  desired: ProjectManifest,
  leadId: string | undefined,
): string[] {
  const changes = new Set<string>();
  if (desired.description !== undefined && existing.description !== desired.description) {
    changes.add("description");
  }
  if (desired.icon !== undefined && existing.icon !== desired.icon) changes.add("icon");
  if (desired.status !== undefined && existing.status !== desired.status) changes.add("status");
  if (desired.start_date !== undefined && existing.start_date !== desired.start_date) {
    changes.add("start-date");
  }
  if (desired.due_date !== undefined && existing.due_date !== desired.due_date) {
    changes.add("due-date");
  }
  if (desired.lead !== undefined && leadId !== undefined) {
    if (existing.lead_type !== "agent" || existing.lead_id !== leadId) changes.add("lead");
  }
  return [...changes];
}

export function projectCreateArgs(
  desired: ProjectManifest,
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  const args = ["project", "create", "--title", desired.title, "--output", "json"];
  appendOptional(args, "--description", desired.description);
  appendOptional(args, "--icon", desired.icon);
  appendOptional(args, "--status", desired.status);
  appendOptional(args, "--start-date", desired.start_date);
  appendOptional(args, "--due-date", desired.due_date);
  appendOptional(args, "--lead", desired.lead);
  for (const repo of desired.repos ?? []) args.push("--repo", repo);
  args.push(...contextArgs(workspaceId, profile));
  return args;
}

export function projectUpdateArgs(
  projectId: string,
  desired: ProjectManifest,
  changes: string[],
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  const args = ["project", "update", projectId, "--output", "json"];
  const changed = new Set(changes);
  if (changed.has("description") && desired.description !== undefined) {
    args.push("--description", desired.description);
  }
  if (changed.has("icon") && desired.icon !== undefined) args.push("--icon", desired.icon);
  if (changed.has("status") && desired.status !== undefined) {
    args.push("--status", desired.status);
  }
  if (changed.has("start-date") && desired.start_date !== undefined) {
    args.push("--start-date", desired.start_date);
  }
  if (changed.has("due-date") && desired.due_date !== undefined) {
    args.push("--due-date", desired.due_date);
  }
  if (changed.has("lead") && desired.lead !== undefined) args.push("--lead", desired.lead);
  args.push(...contextArgs(workspaceId, profile));
  return args;
}

export function labelDiff(existing: LabelState, desired: LabelManifest): string[] {
  const changes = new Set<string>();
  if (existing.name !== desired.name) changes.add("name");
  if (existing.color.toLowerCase() !== desired.color.toLowerCase()) changes.add("color");
  if ((existing.description ?? "") !== (desired.description ?? "")) changes.add("description");
  return [...changes];
}

export function statusDiff(existing: StatusState, desired: StatusManifest): string[] {
  const changes = new Set<string>();
  if (existing.name !== desired.name) changes.add("name");
  if (existing.description !== (desired.description ?? "")) changes.add("description");
  if (existing.category !== desired.category) changes.add("category");
  if (existing.color.toLowerCase() !== desired.color.toLowerCase()) changes.add("color");
  if (existing.archived) changes.add("archived");
  return [...changes];
}

/** Names present on the server and absent from the manifest. Sync reports these and does not delete them. */
export function catalogExtras(existing: string[], desired: string[]): string[] {
  const wanted = new Set(desired);
  return existing.filter((name) => !wanted.has(name)).sort();
}

export function labelCreateArgs(
  desired: LabelManifest,
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  const args = [
    "label",
    "create",
    "--name",
    desired.name,
    "--color",
    desired.color,
    "--output",
    "json",
  ];
  if (desired.description !== undefined) args.push("--description", desired.description);
  args.push(...contextArgs(workspaceId, profile));
  return args;
}

export function labelUpdateArgs(
  labelId: string,
  desired: LabelManifest,
  changes: string[],
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  const args = ["label", "update", labelId, "--output", "json"];
  const changed = new Set(changes);
  if (changed.has("name")) args.push("--name", desired.name);
  if (changed.has("color")) args.push("--color", desired.color);
  if (changed.has("description")) args.push("--description", desired.description ?? "");
  args.push(...contextArgs(workspaceId, profile));
  return args;
}

export function squadDiff(
  existing: SquadState,
  desired: SquadManifest,
  leaderId: string,
): string[] {
  const changes = new Set<string>();
  if (existing.description !== (desired.description ?? "")) changes.add("description");
  if (existing.instructions !== (desired.instructions ?? "")) changes.add("instructions");
  if (existing.leader_id !== leaderId) changes.add("leader");
  if (existing.avatar_url !== (desired.avatar_url ?? "")) changes.add("avatar-url");
  return [...changes];
}

export function squadCreateArgs(
  desired: SquadManifest,
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  const args = [
    "squad",
    "create",
    "--name",
    desired.name,
    "--leader",
    desired.leader,
    "--output",
    "json",
  ];
  appendOptional(args, "--description", desired.description);
  args.push(...contextArgs(workspaceId, profile));
  return args;
}

export function squadUpdateArgs(
  squadId: string,
  desired: SquadManifest,
  changes: string[],
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  const args = ["squad", "update", squadId, "--output", "json"];
  const changed = new Set(changes);
  if (changed.has("description")) args.push("--description", desired.description ?? "");
  if (changed.has("instructions")) args.push("--instructions", desired.instructions ?? "");
  if (changed.has("leader")) args.push("--leader", desired.leader);
  if (changed.has("avatar-url") && desired.avatar_url !== undefined) {
    args.push("--avatar-url", desired.avatar_url);
  }
  args.push(...contextArgs(workspaceId, profile));
  return args;
}

export function squadMemberAddArgs(
  squadId: string,
  memberId: string,
  memberType: "agent" | "member",
  role: string,
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  return [
    "squad",
    "member",
    "add",
    squadId,
    "--member-id",
    memberId,
    "--type",
    memberType,
    "--role",
    role,
    "--output",
    "json",
    ...contextArgs(workspaceId, profile),
  ];
}

export function squadMemberSetRoleArgs(
  squadId: string,
  memberId: string,
  memberType: "agent" | "member",
  role: string,
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  return [
    "squad",
    "member",
    "set-role",
    squadId,
    "--member-id",
    memberId,
    "--member-type",
    memberType,
    "--role",
    role,
    "--output",
    "json",
    ...contextArgs(workspaceId, profile),
  ];
}

export function squadMemberRemoveArgs(
  squadId: string,
  memberId: string,
  memberType: "agent" | "member",
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  return [
    "squad",
    "member",
    "remove",
    squadId,
    "--member-id",
    memberId,
    "--type",
    memberType,
    "--output",
    "json",
    ...contextArgs(workspaceId, profile),
  ];
}

/** Squad membership follows the manifest. The leader row is not a listed member. */
export function unmatchedSquadMembers(
  existing: SquadMemberState[],
  kept: { id: string; type: string }[],
  leaderId: string,
): SquadMemberState[] {
  const wanted = new Set(kept.map((member) => `${member.type}:${member.id}`));
  return existing.filter(
    (member) =>
      member.member_id !== leaderId && !wanted.has(`${member.member_type}:${member.member_id}`),
  );
}

export function projectResourceAddArgs(
  projectId: string,
  resource: ProjectResourceManifest,
  daemonId: string | undefined,
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  const args = [
    "project",
    "resource",
    "add",
    projectId,
    "--type",
    resource.type,
    "--output",
    "json",
  ];
  if (resource.type === "github_repo") {
    args.push("--url", requiredString(resource.url, "resource.url"));
    appendOptional(args, "--label", resource.label);
  } else {
    const ref: Record<string, string> = {
      local_path: requiredString(resource.local_path, "resource.local_path"),
      daemon_id: requiredString(daemonId, "resource daemon id"),
    };
    if (resource.execution_mode) ref.execution_mode = resource.execution_mode;
    if (resource.label) ref.label = resource.label;
    args.push("--ref", JSON.stringify(ref));
  }
  args.push(...contextArgs(workspaceId, profile));
  return args;
}

export function projectResourceUpdateArgs(
  projectId: string,
  resourceId: string,
  resource: ProjectResourceManifest,
  daemonId: string | undefined,
  changes: string[],
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  const args = ["project", "resource", "update", projectId, resourceId, "--output", "json"];
  const changed = new Set(changes);
  if (resource.type === "github_repo") {
    if (changed.has("ref")) {
      args.push("--url", requiredString(resource.url, "resource.url"));
    }
    if (changed.has("label")) args.push("--label", resource.label ?? "");
  } else {
    if (changed.has("ref") || changed.has("label")) {
      const ref: Record<string, string> = {
        local_path: requiredString(resource.local_path, "resource.local_path"),
        daemon_id: requiredString(daemonId, "resource daemon id"),
      };
      if (resource.execution_mode) ref.execution_mode = resource.execution_mode;
      if (resource.label) ref.label = resource.label;
      args.push("--ref", JSON.stringify(ref));
    }
  }
  args.push(...contextArgs(workspaceId, profile));
  return args;
}

export function projectResourceRemoveArgs(
  projectId: string,
  resourceId: string,
  workspaceId: string | undefined,
  profile: string | undefined,
): string[] {
  return [
    "project",
    "resource",
    "remove",
    projectId,
    resourceId,
    "--output",
    "json",
    ...contextArgs(workspaceId, profile),
  ];
}

function resolveMemberId(members: WorkspaceMemberState[], ref: string): string {
  const normalized = ref.trim().toLowerCase();
  const match = findUnique(
    members,
    (member) =>
      member.name.toLowerCase() === normalized || member.email.toLowerCase() === normalized,
    `member ${ref}`,
  );
  if (!match) throw new Error(`workspace member not found: ${ref}`);
  return match.user_id;
}

function findUnique<T>(items: T[], predicate: (item: T) => boolean, label: string): T | undefined {
  const matches = items.filter(predicate);
  if (matches.length > 1) throw new Error(`${label} is ambiguous`);
  return matches[0];
}

function printPlan(plan: Plan) {
  const pending =
    plan.args.length === 0 && !plan.exec && !plan.blocked
      ? " (pending: applies after dependent resources exist)"
      : "";
  const detail = plan.changes.length > 0 ? ` (${plan.changes.join(", ")})` : "";
  const blocked = plan.blocked ? ` — ${plan.blocked}` : "";
  const prune = plan.prune ? " [--prune]" : "";
  console.log(`${plan.kind} ${plan.action}: ${plan.name}${detail}${pending}${blocked}${prune}`);
  for (const line of formatDiff(plan.diff ?? [])) console.log(line);
}

/** Field-level diff lines. Multi-line values get a line diff; long single lines are clipped. */
export function formatDiff(diff: FieldDiff[], width = 160): string[] {
  const out: string[] = [];
  const clip = (value: string) => (value.length > width ? `${value.slice(0, width - 1)}…` : value);
  for (const entry of diff) {
    if (!entry.from.includes("\n") && !entry.to.includes("\n")) {
      out.push(`    ${entry.field}: ${JSON.stringify(clip(entry.from))} -> ${JSON.stringify(clip(entry.to))}`);
      continue;
    }
    out.push(`    ${entry.field}:`);
    for (const line of lineDiff(entry.from.split("\n"), entry.to.split("\n"))) out.push(`      ${clip(line)}`);
  }
  return out;
}

/** Minimal LCS line diff: "  " same, "- " removed, "+ " added. */
export function lineDiff(a: string[], b: string[]): string[] {
  const n = a.length;
  const m = b.length;
  const lcs: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i -= 1) {
    for (let j = m - 1; j >= 0; j -= 1) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }
  const out: string[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push(`  ${a[i]}`);
      i += 1;
      j += 1;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      out.push(`- ${a[i]}`);
      i += 1;
    } else {
      out.push(`+ ${b[j]}`);
      j += 1;
    }
  }
  while (i < n) out.push(`- ${a[i++]}`);
  while (j < m) out.push(`+ ${b[j++]}`);
  return out;
}

interface LoadedState {
  runtimes: RuntimeRecord[];
  agents: AgentSummary[];
  projects: ProjectState[];
  labels: LabelState[];
  statuses: StatusState[];
  skills: SkillState[];
  members: WorkspaceMemberState[];
  squads: SquadState[];
}

function cliConfigPath(profile: string | undefined): string {
  return profile
    ? join(homedir(), ".multica", "profiles", profile, "config.json")
    : join(homedir(), ".multica", "config.json");
}

function multicaApi(
  profile: string | undefined,
  workspaceId: string,
  method: string,
  path: string,
  body?: JsonObject,
): JsonObject {
  const cfg = object(JSON.parse(readFileSync(cliConfigPath(profile), "utf8")), "config");
  const server = requiredString(cfg.server_url, "config.server_url").replace(/\/$/, "");
  const token = requiredString(cfg.token, "config.token");
  const args = [
    "-fsS",
    "-X",
    method,
    "-H",
    `Authorization: Bearer ${token}`,
    "-H",
    `X-Workspace-Id: ${workspaceId}`,
    "-H",
    "Accept: application/json",
  ];
  if (body) {
    args.push("-H", "Content-Type: application/json", "--data-binary", JSON.stringify(body));
  }
  args.push(`${server}${path}`);
  const result = spawnSync("curl", args, { encoding: "utf8" });
  if (result.status !== 0) {
    const detail = (result.stderr || "").trim().split("\n").slice(-3).join(" ");
    throw new Error(`issue-status request failed (${method} ${path}): ${detail || "curl failed"}`);
  }
  if (!result.stdout.trim()) return {};
  return parseJSONObject(result.stdout, path);
}

function loadStatuses(context: string[]): StatusState[] {
  const workspaceId = contextWorkspaceId(context);
  if (!workspaceId) throw new Error("workspace id is required to read issue statuses");
  const payload = multicaApi(
    contextProfile(context),
    workspaceId,
    "GET",
    "/api/issue-statuses?include_archived=true",
  );
  return array(payload.statuses, "statuses").map(toStatusState);
}

function loadLabels(context: string[]): LabelState[] {
  return parseJSONArray<unknown>(
    runMultica([...context, "label", "list", "--output", "json"]),
    "labels",
  ).map(toLabelState);
}

function loadMembers(context: string[]): WorkspaceMemberState[] {
  return parseJSONArray<unknown>(
    runMultica([...context, "workspace", "member", "list", "--output", "json"]),
    "members",
  ).map(toWorkspaceMemberState);
}

function loadSquads(context: string[]): SquadState[] {
  let output: string;
  try {
    output = runMultica([...context, "squad", "list", "--output", "json"]);
  } catch (error) {
    // The orchestrator build dropped squads: no `multica squad`, so none exist.
    if (/unknown command "squad"/.test(error instanceof Error ? error.message : String(error))) return [];
    throw error;
  }
  return parseJSONArray<unknown>(output, "squads").map(toSquadState);
}

function loadSquadMembers(squadId: string, context: string[]): SquadMemberState[] {
  return parseJSONArray<unknown>(
    runMultica([...context, "squad", "member", "list", squadId, "--output", "json"]),
    "squad members",
  ).map(toSquadMemberState);
}

function loadProjectResources(projectId: string, context: string[]): ProjectResourceState[] {
  return parseJSONArray<unknown>(
    runMultica([...context, "project", "resource", "list", projectId, "--output", "json"]),
    "project resources",
  ).map(toProjectResourceState);
}

function loadSkills(context: string[]): SkillState[] {
  return parseJSONArray<unknown>(
    runMultica([...context, "skill", "list", "--output", "json"]),
    "skills",
  ).map(toSkillState);
}

// multica 42 includes file and skill bodies by default and rejects --with-content.
export function skillFilesListArgs(skillId: string, context: string[]): string[] {
  return [...context, "skill", "files", "list", skillId, "--output", "json"];
}

export function skillGetArgs(skillId: string, context: string[]): string[] {
  return [...context, "skill", "get", skillId, "--output", "json"];
}

function loadSkillFiles(skillId: string, context: string[]): SkillFileState[] {
  return parseJSONArray<unknown>(
    runMultica(skillFilesListArgs(skillId, context)),
    "skill files",
  ).map(toSkillFileState);
}

function loadSkillContent(skillId: string, context: string[]): string {
  const skill = parseJSONObject(
    runMultica(skillGetArgs(skillId, context)),
    "skill",
  );
  return optionalString(skill.content, "skill.content") ?? "";
}

/** sha256 of the server's SKILL.md body: `content_hash` when the server sends it, else hashed from `content`. */
function loadSkillContentHash(skillId: string, context: string[]): string {
  const skill = parseJSONObject(runMultica(skillGetArgs(skillId, context)), "skill");
  const hash = optionalString(skill.content_hash, "skill.content_hash");
  if (hash) return hash.toLowerCase();
  return sha256Text(optionalString(skill.content, "skill.content") ?? "");
}

export function sha256Text(text: string): string {
  return createHash("sha256").update(text, "utf8").digest("hex");
}

/** A server file matches when its body or, lacking one, its content_hash equals the local text. */
export function skillFileMatches(existing: { content: string; content_hash: string } | undefined, local: string): boolean {
  if (!existing) return false;
  if (existing.content_hash) return existing.content_hash.toLowerCase() === sha256Text(local);
  return existing.content === local;
}

function loadAgentSkills(agentId: string, context: string[]): AgentSkillState[] {
  return parseJSONArray<unknown>(
    runMultica([...context, "agent", "skills", "list", agentId, "--output", "json"]),
    "agent skills",
  ).map(toAgentSkillState);
}

function loadState(context: string[]): LoadedState {
  return {
    runtimes: parseJSONArray<unknown>(
      runMultica([...context, "runtime", "list", "--output", "json"]),
      "runtimes",
    ).map(toRuntime),
    agents: parseJSONArray<unknown>(
      runMultica([...context, "agent", "list", "--output", "json", "--include-archived"]),
      "agents",
    ).map(toAgentSummary),
    projects: parseJSONArray<unknown>(
      runMultica([...context, "project", "list", "--output", "json"]),
      "projects",
    ).map(toProjectState),
    labels: loadLabels(context),
    statuses: loadStatuses(context),
    skills: loadSkills(context),
    members: loadMembers(context),
    squads: loadSquads(context),
  };
}

function buildLabelPlans(
  manifest: MulticaManifest,
  labels: LabelState[],
  context: string[],
): Plan[] {
  const plans: Plan[] = [];
  const issueLabels = labels.filter(
    (label) => label.resource_type === "issue" || label.resource_type === "",
  );
  for (const desired of manifest.labels) {
    const existing = findUnique(
      issueLabels,
      (label) => label.name === desired.name,
      `label ${desired.name}`,
    );
    if (!existing) {
      plans.push({
        kind: "label",
        action: "create",
        name: desired.name,
        changes: [],
        args: labelCreateArgs(desired, contextWorkspaceId(context), contextProfile(context)),
      });
      continue;
    }
    const changes = labelDiff(existing, desired);
    if (changes.length > 0) {
      plans.push({
        kind: "label",
        action: "update",
        name: desired.name,
        changes,
        args: labelUpdateArgs(
          existing.id,
          desired,
          changes,
          contextWorkspaceId(context),
          contextProfile(context),
        ),
      });
    }
  }
  for (const name of catalogExtras(
    issueLabels.map((label) => label.name),
    manifest.labels.map((label) => label.name),
  )) {
    plans.push({
      kind: "label",
      action: "extra",
      name,
      changes: ["not-in-manifest"],
      args: [],
      blocked: "on the server but not in the manifest; sync does not delete it",
    });
  }
  return plans;
}

function statusPlanBlocked(
  existing: StatusState | undefined,
  desired: StatusManifest,
  changes: string[],
): string | undefined {
  if (!existing && SYSTEM_STATUS_KEYS.has(desired.key)) {
    return "built-in status is missing; sync does not recreate it";
  }
  if ((existing?.is_system || SYSTEM_STATUS_KEYS.has(desired.key)) && changes.length > 0) {
    return "built-in status is locked; the manifest must match the server";
  }
  if (changes.includes("category")) return "category is immutable";
  if (changes.includes("archived")) return "archived on the server; sync does not restore it";
  return undefined;
}

function buildStatusPlans(
  manifest: MulticaManifest,
  statuses: StatusState[],
  context: string[],
): Plan[] {
  const workspaceId = contextWorkspaceId(context);
  const profile = contextProfile(context);
  if (!workspaceId) throw new Error("workspace id is required to sync issue statuses");
  const plans: Plan[] = [];
  for (const desired of manifest.statuses) {
    const existing = findUnique(
      statuses,
      (status) => status.key === desired.key,
      `status ${desired.key}`,
    );
    if (!existing) {
      const blocked = statusPlanBlocked(undefined, desired, []);
      plans.push({
        kind: "status",
        action: "create",
        name: desired.key,
        changes: [],
        args: [],
        blocked,
        exec: blocked
          ? undefined
          : () => {
              multicaApi(profile, workspaceId, "POST", "/api/issue-statuses", {
                key: desired.key,
                name: desired.name,
                description: desired.description ?? "",
                category: desired.category,
                color: desired.color,
              });
            },
      });
      continue;
    }
    const changes = statusDiff(existing, desired);
    if (changes.length === 0) continue;
    const blocked = statusPlanBlocked(existing, desired, changes);
    plans.push({
      kind: "status",
      action: "update",
      name: desired.key,
      changes,
      args: [],
      blocked,
      exec: blocked
        ? undefined
        : () => {
            const body: JsonObject = {};
            if (changes.includes("name")) body.name = desired.name;
            if (changes.includes("description")) body.description = desired.description ?? "";
            if (changes.includes("color")) body.color = desired.color;
            multicaApi(profile, workspaceId, "PATCH", `/api/issue-statuses/${existing.id}`, body);
          },
    });
  }
  for (const key of catalogExtras(
    statuses.map((status) => status.key),
    manifest.statuses.map((status) => status.key),
  )) {
    plans.push({
      kind: "status",
      action: "extra",
      name: key,
      changes: ["not-in-manifest"],
      args: [],
      blocked: "on the server but not in the manifest; sync does not delete it",
    });
  }
  return plans;
}

function skillFilesOnDisk(directory: string): Map<string, string> {
  const files = new Map<string, string>();
  const walk = (current: string, prefix: string) => {
    for (const entry of readdirSync(current, { withFileTypes: true })) {
      const absolute = join(current, entry.name);
      const relativePath = prefix ? `${prefix}/${entry.name}` : entry.name;
      if (entry.isDirectory()) {
        walk(absolute, relativePath);
      } else if (entry.isFile()) {
        files.set(relativePath, absolute);
      }
    }
  };
  walk(directory, "");
  return files;
}

function skillCreateArgs(desired: SkillManifest, context: string[]): string[] {
  const args = ["skill", "create", "--name", desired.name, "--output", "json"];
  if (desired.description) args.push("--description", desired.description);
  args.push(
    "--content-file",
    join(requiredString(desired.path, "skill.path"), "SKILL.md"),
    ...context,
  );
  return args;
}

function skillUpdateArgs(
  skillId: string,
  desired: SkillManifest,
  changes: string[],
  context: string[],
): string[] {
  const args = ["skill", "update", skillId, "--output", "json"];
  if (changes.includes("description") && desired.description !== undefined) {
    args.push("--description", desired.description);
  }
  if (changes.includes("content")) {
    args.push("--content-file", join(requiredString(desired.path, "skill.path"), "SKILL.md"));
  }
  args.push(...context);
  return args;
}

function skillFileUpsertArgs(
  skillId: string,
  relativePath: string,
  absolutePath: string,
  context: string[],
): string[] {
  return [
    "skill",
    "files",
    "upsert",
    skillId,
    "--path",
    relativePath,
    "--content-file",
    absolutePath,
    "--output",
    "json",
    ...context,
  ];
}

function skillFileDeleteArgs(skillId: string, fileId: string, context: string[]): string[] {
  return ["skill", "files", "delete", skillId, fileId, "--output", "json", ...context];
}

function agentSkillsSetArgs(agentId: string, skillIds: string[], context: string[]): string[] {
  return [
    "agent",
    "skills",
    "set",
    agentId,
    "--skill-ids",
    skillIds.join(","),
    "--output",
    "json",
    ...context,
  ];
}

function buildSkillPlans(manifest: MulticaManifest, state: LoadedState, context: string[]): Plan[] {
  const plans: Plan[] = [];
  for (const desired of manifest.skills) {
    const path = desired.path;
    if (!path) throw new Error(`skills.${desired.name}.path is required`);
    const diskFiles = skillFilesOnDisk(path);
    const existing = findUnique(
      state.skills,
      (skill) => skill.name === desired.name,
      `skill ${desired.name}`,
    );
    if (!existing) {
      plans.push({
        kind: "skill",
        action: "create",
        name: desired.name,
        changes: [],
        args: skillCreateArgs(desired, context),
        skill: { name: desired.name, path },
        context,
      });
      continue;
    }
    const existingFiles = loadSkillFiles(existing.id, context);
    const existingByPath = new Map(existingFiles.map((file) => [file.path, file]));
    const postArgs: { label: string; args: string[] }[] = [];
    const changes: string[] = [];
    const primaryPath = join(path, "SKILL.md");
    const primaryContent = readFileSync(primaryPath, "utf8");
    if (loadSkillContentHash(existing.id, context) !== sha256Text(primaryContent)) {
      changes.push("content");
    }
    for (const [relativePath, absolutePath] of diskFiles) {
      if (relativePath === "SKILL.md") continue;
      const content = readFileSync(absolutePath, "utf8");
      if (!skillFileMatches(existingByPath.get(relativePath), content)) {
        postArgs.push({
          label: `skill file upsert: ${relativePath}`,
          args: skillFileUpsertArgs(existing.id, relativePath, absolutePath, context),
        });
      }
    }
    for (const file of existingFiles) {
      if (file.path === "SKILL.md") continue;
      if (!diskFiles.has(file.path)) {
        postArgs.push({
          label: `skill file delete: ${file.path}`,
          args: skillFileDeleteArgs(existing.id, file.id, context),
        });
      }
    }
    if (desired.description !== undefined && existing.description !== desired.description) {
      changes.push("description");
    }
    if (changes.length > 0 || postArgs.length > 0) {
      plans.push({
        kind: "skill",
        action: "update",
        name: desired.name,
        changes: [...changes, ...(postArgs.length > 0 ? ["files"] : [])],
        args: changes.length > 0 ? skillUpdateArgs(existing.id, desired, changes, context) : [],
        postArgs,
      });
    }
  }
  return plans;
}

function buildAgentSkillPlans(
  manifest: MulticaManifest,
  state: LoadedState,
  context: string[],
  allowUnresolved: boolean,
): Plan[] {
  const plans: Plan[] = [];
  for (const desired of manifest.agents) {
    if (!desired.skills?.length) continue;
    const agent = state.agents.find((candidate) => candidate.name === desired.name);
    if (!agent) {
      if (allowUnresolved) {
        plans.push({
          kind: "agent-skill",
          action: "set",
          name: `${desired.name}: skills`,
          changes: ["pending"],
          args: [],
        });
      } else {
        throw new Error(`agent not found after apply: ${desired.name}`);
      }
      continue;
    }
    const desiredIds: string[] = [];
    let missing = false;
    for (const skillName of desired.skills) {
      const skill = state.skills.find((candidate) => candidate.name === skillName);
      if (!skill) {
        if (allowUnresolved) {
          missing = true;
          break;
        }
        throw new Error(`skill not found for agent ${desired.name}: ${skillName}`);
      }
      desiredIds.push(skill.id);
    }
    if (missing) {
      plans.push({
        kind: "agent-skill",
        action: "set",
        name: `${desired.name}: skills`,
        changes: ["pending"],
        args: [],
      });
      continue;
    }
    const existing = loadAgentSkills(agent.id, context);
    const existingIds = new Set(existing.map((skill) => skill.id));
    const same =
      desiredIds.length === existing.length && desiredIds.every((id) => existingIds.has(id));
    if (!same) {
      plans.push({
        kind: "agent-skill",
        action: "set",
        name: `${desired.name}: skills`,
        changes: desired.skills,
        args: agentSkillsSetArgs(agent.id, desiredIds, context),
      });
    }
  }
  return plans;
}

function buildAgentPlans(manifest: MulticaManifest, state: LoadedState, context: string[]): Plan[] {
  const plans: Plan[] = [];
  for (const desired of manifest.agents) {
    const runtimeId = resolveRuntimeId(state.runtimes, desired.runtime);
    const existingSummary = findUnique(
      state.agents,
      (agent) => agent.name === desired.name,
      `agent ${desired.name}`,
    );
    if (!existingSummary) {
      plans.push({
        kind: "agent",
        action: "create",
        name: desired.name,
        changes: [],
        args: agentCreateArgs(
          desired,
          runtimeId,
          contextWorkspaceId(context),
          contextProfile(context),
        ),
      });
      continue;
    }
    if (existingSummary.archived_at) {
      throw new Error(`agent ${desired.name} is archived; restore it or choose a different name`);
    }
    const existing = toAgentState(
      parseJSONObject(
        runMultica([...context, "agent", "get", existingSummary.id, "--output", "json"]),
        "agent",
      ),
    );
    const changes = agentDiff(existing, desired, runtimeId);
    if (changes.length > 0) {
      plans.push({
        kind: "agent",
        action: "update",
        name: desired.name,
        changes,
        diff: agentFieldDiff(existing, desired, runtimeId, changes),
        args: agentUpdateArgs(
          existing.id,
          desired,
          runtimeId,
          changes,
          contextWorkspaceId(context),
          contextProfile(context),
        ),
      });
    }
  }
  return plans;
}

function buildProjectPlans(
  manifest: MulticaManifest,
  state: LoadedState,
  context: string[],
): Plan[] {
  const plans: Plan[] = [];
  for (const desired of manifest.projects) {
    const existing = findUnique(
      state.projects,
      (project) => project.title === desired.title,
      `project ${desired.title}`,
    );
    if (!existing) {
      plans.push({
        kind: "project",
        action: "create",
        name: desired.title,
        changes: [],
        args: projectCreateArgs(desired, contextWorkspaceId(context), contextProfile(context)),
      });
      continue;
    }
    const leadAgent =
      desired.lead === undefined
        ? undefined
        : state.agents.find((agent) => agent.name === desired.lead);
    const leadId = leadAgent?.id;
    const changes = projectDiff(existing, desired, leadId);
    if (changes.length > 0) {
      plans.push({
        kind: "project",
        action: "update",
        name: desired.title,
        changes,
        args: projectUpdateArgs(
          existing.id,
          desired,
          changes,
          contextWorkspaceId(context),
          contextProfile(context),
        ),
      });
    }
  }
  return plans;
}

function buildProjectPostPlans(
  manifest: MulticaManifest,
  state: LoadedState,
  context: string[],
): Plan[] {
  return buildProjectPlans(manifest, state, context).filter((plan) => plan.action === "update");
}

function buildSquadPlans(manifest: MulticaManifest, state: LoadedState, context: string[]): Plan[] {
  const plans: Plan[] = [];
  for (const desired of manifest.squads) {
    const existing = findUnique(
      state.squads,
      (squad) => squad.name === desired.name,
      `squad ${desired.name}`,
    );
    if (!existing) {
      plans.push({
        kind: "squad",
        action: "create",
        name: desired.name,
        changes: [],
        args: squadCreateArgs(desired, contextWorkspaceId(context), contextProfile(context)),
      });
      continue;
    }
    const leaderAgent = state.agents.find((agent) => agent.name === desired.leader);
    const leaderId = leaderAgent?.id;
    if (leaderId === undefined) continue;
    const changes = squadDiff(existing, desired, leaderId);
    if (changes.length > 0) {
      plans.push({
        kind: "squad",
        action: "update",
        name: desired.name,
        changes,
        args: squadUpdateArgs(
          existing.id,
          desired,
          changes,
          contextWorkspaceId(context),
          contextProfile(context),
        ),
      });
    }
  }
  return plans;
}

function buildSquadPostPlans(
  manifest: MulticaManifest,
  state: LoadedState,
  context: string[],
): Plan[] {
  return buildSquadPlans(manifest, state, context).filter((plan) => plan.action === "update");
}

function resolveSquadMemberId(
  member: SquadMemberManifest,
  state: LoadedState,
): { id: string; type: "agent" | "member" } {
  if (member.type === "agent") {
    const agent = state.agents.find((candidate) => candidate.name === member.ref);
    if (!agent) throw new Error(`squad member agent not found: ${member.ref}`);
    return { id: agent.id, type: "agent" };
  }
  return { id: resolveMemberId(state.members, member.ref), type: "member" };
}

function buildSquadMemberPlans(
  manifest: MulticaManifest,
  state: LoadedState,
  context: string[],
  allowUnresolved: boolean,
): Plan[] {
  const plans: Plan[] = [];
  for (const desired of manifest.squads) {
    const existing = findUnique(
      state.squads,
      (squad) => squad.name === desired.name,
      `squad ${desired.name}`,
    );
    if (!existing) {
      if (!allowUnresolved) throw new Error(`squad not found after apply: ${desired.name}`);
      for (const member of desired.members ?? []) {
        plans.push({
          kind: "squad-member",
          action: "add",
          name: `${desired.name}: ${member.ref}`,
          changes: ["pending"],
          args: [],
        });
      }
      continue;
    }
    const existingMembers = loadSquadMembers(existing.id, context);
    for (const member of desired.members ?? []) {
      let resolved: { id: string; type: "agent" | "member" };
      try {
        resolved = resolveSquadMemberId(member, state);
      } catch (error) {
        if (allowUnresolved) {
          plans.push({
            kind: "squad-member",
            action: "add",
            name: `${desired.name}: ${member.ref}`,
            changes: ["pending"],
            args: [],
          });
          continue;
        }
        throw error;
      }
      const current = existingMembers.find(
        (entry) => entry.member_id === resolved.id && entry.member_type === resolved.type,
      );
      if (!current) {
        plans.push({
          kind: "squad-member",
          action: "add",
          name: `${desired.name}: ${member.ref}`,
          changes: [`role=${member.role}`],
          args: squadMemberAddArgs(
            existing.id,
            resolved.id,
            resolved.type,
            member.role,
            contextWorkspaceId(context),
            contextProfile(context),
          ),
        });
      } else if (current.role !== member.role) {
        plans.push({
          kind: "squad-member",
          action: "set-role",
          name: `${desired.name}: ${member.ref}`,
          changes: [`role=${member.role}`],
          args: squadMemberSetRoleArgs(
            existing.id,
            resolved.id,
            resolved.type,
            member.role,
            contextWorkspaceId(context),
            contextProfile(context),
          ),
        });
      }
    }
    if (desired.members) {
      const kept: { id: string; type: "agent" | "member" }[] = [];
      for (const member of desired.members) {
        try {
          kept.push(resolveSquadMemberId(member, state));
        } catch {
          // Phase 1 creates this agent; --check cannot resolve it yet.
          // It is not an existing row, so omitting it does not remove anyone.
        }
      }
      for (const stale of unmatchedSquadMembers(existingMembers, kept, existing.leader_id)) {
        const memberType = stale.member_type === "member" ? "member" : "agent";
        plans.push({
          kind: "squad-member",
          action: "remove",
          name: `${desired.name}: ${stale.member_id}`,
          changes: ["remove"],
          args: squadMemberRemoveArgs(
            existing.id,
            stale.member_id,
            memberType,
            contextWorkspaceId(context),
            contextProfile(context),
          ),
        });
      }
    }
  }
  return plans;
}

function desiredResourceRefKey(
  resource: ProjectResourceManifest,
  daemonId: string | undefined,
): string {
  if (resource.type === "github_repo") return resource.url ?? "";
  return `${daemonId ?? ""}:${resource.local_path ?? ""}`;
}

function runtimeDaemonId(runtimes: RuntimeRecord[], runtimeId: string): string {
  const runtime = runtimes.find((candidate) => candidate.id === runtimeId);
  if (!runtime?.daemon_id) throw new Error(`runtime ${runtimeId} has no daemon id`);
  return runtime.daemon_id;
}

function buildProjectResourcePlans(
  manifest: MulticaManifest,
  state: LoadedState,
  context: string[],
  allowUnresolved: boolean,
): Plan[] {
  const plans: Plan[] = [];
  for (const desired of manifest.projects) {
    const existing = findUnique(
      state.projects,
      (project) => project.title === desired.title,
      `project ${desired.title}`,
    );
    if (!existing) {
      if (!allowUnresolved) throw new Error(`project not found after apply: ${desired.title}`);
      for (const resource of desired.resources ?? []) {
        plans.push({
          kind: "project",
          action: "add",
          name: `${desired.title}: ${resource.type}`,
          changes: ["pending"],
          args: [],
        });
      }
      continue;
    }
    const existingResources = loadProjectResources(existing.id, context);
    const matched = new Set<string>();
    for (const resource of desired.resources ?? []) {
      const daemonId =
        resource.type === "local_directory"
          ? runtimeDaemonId(
              state.runtimes,
              resolveRuntimeId(state.runtimes, resource.runtime ?? ""),
            )
          : undefined;
      const refKey = desiredResourceRefKey(resource, daemonId);
      const candidates = existingResources.filter((entry) => entry.resource_type === resource.type);
      if (candidates.length === 0) {
        plans.push({
          kind: "project",
          action: "add",
          name: `${desired.title}: ${resource.type} ${refKey}`,
          changes: [`${resource.type} ${refKey}`],
          args: projectResourceAddArgs(
            existing.id,
            resource,
            daemonId,
            contextWorkspaceId(context),
            contextProfile(context),
          ),
        });
        continue;
      }
      const current =
        candidates.find((entry) => entry.ref_key === refKey) ??
        (candidates.length === 1 ? candidates[0] : undefined);
      if (!current) {
        throw new Error(
          `project ${desired.title}: multiple ${resource.type} resources exist; add a stable ref key`,
        );
      }
      matched.add(current.id);
      const changes = new Set<string>();
      if (current.ref_key !== refKey) changes.add("ref");
      const desiredLabel = resource.label ?? "";
      const currentLabel = current.label || current.ref_label;
      if (currentLabel !== desiredLabel) changes.add("label");
      if (changes.size > 0) {
        plans.push({
          kind: "project",
          action: "update",
          name: `${desired.title}: ${resource.type} ${refKey}`,
          changes: [...changes],
          args: projectResourceUpdateArgs(
            existing.id,
            current.id,
            resource,
            daemonId,
            [...changes],
            contextWorkspaceId(context),
            contextProfile(context),
          ),
        });
      }
    }
    for (const stale of existingResources) {
      if (matched.has(stale.id)) continue;
      plans.push({
        kind: "project",
        action: "remove",
        name: `${desired.title}: ${stale.resource_type} ${stale.ref_key}`,
        changes: ["remove"],
        args: projectResourceRemoveArgs(
          existing.id,
          stale.id,
          contextWorkspaceId(context),
          contextProfile(context),
        ),
      });
    }
  }
  return plans;
}

function contextWorkspaceId(context: string[]): string | undefined {
  const index = context.indexOf("--workspace-id");
  return index === -1 ? undefined : context[index + 1];
}

function contextProfile(context: string[]): string | undefined {
  const index = context.indexOf("--profile");
  return index === -1 ? undefined : context[index + 1];
}

function dedupePlans(phase1: Plan[], phase2: Plan[]): Plan[] {
  return phase2.filter(
    (plan) =>
      !phase1.some(
        (earlier) =>
          earlier.kind === plan.kind &&
          earlier.name === plan.name &&
          plan.changes.every((change) => earlier.changes.includes(change)),
      ),
  );
}

/**
 * Orchestrator team per project: the server owns validation, so the plan asks
 * it (`multica team diff`) and applies only when it reports changes.
 */
function buildOrchestratorPlans(
  manifest: MulticaManifest,
  state: LoadedState,
  context: string[],
  allowUnresolved: boolean,
): Plan[] {
  const plans: Plan[] = [];
  for (const desired of manifest.projects) {
    if (!desired.orchestrator) continue;
    const existing = state.projects.find((project) => project.title === desired.title);
    if (!existing) {
      if (!allowUnresolved) throw new Error(`project not found after apply: ${desired.title}`);
      plans.push({ kind: "project", action: "update", name: `${desired.title}: orchestrator`, changes: ["pending"], args: [] });
      continue;
    }
    const dir = join(homedir(), ".cursor", "artifacts", "multica", "tmp", "orchestrator");
    mkdirSync(dir, { recursive: true });
    const file = join(dir, `${existing.id}.json`);
    writeFileSync(file, JSON.stringify({ orchestrator: desired.orchestrator }, null, 2));
    let raw: string;
    try {
      raw = runMultica(["team", "diff", existing.id, "-f", file, "--output", "json", ...context]);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (!/unknown command "team"/.test(message)) throw error;
      plans.push({
        kind: "project",
        action: "update",
        name: `${desired.title}: orchestrator`,
        changes: ["team"],
        args: [],
        blocked: "this multica CLI/server has no orchestrator (`multica team`); deploy feat/orchestrator first",
        deferred: true,
      });
      continue;
    }
    const diff = parseJSONObject(raw, `team diff ${desired.title}`);
    const changes = Array.isArray(diff.changes) ? diff.changes.map(String) : [];
    if (changes.length === 0) continue;
    plans.push({
      kind: "project",
      action: "update",
      name: `${desired.title}: orchestrator`,
      changes,
      args: ["team", "apply", existing.id, "-f", file, "--output", "json", ...context],
    });
  }
  return plans;
}

function applyPlans(plans: Plan[]) {
  const blocked = plans.filter((plan) => plan.blocked && !plan.deferred);
  for (const plan of plans) {
    if (plan.blocked) continue;
    let output = "";
    if (plan.exec) plan.exec();
    else if (plan.args.length > 0) output = runMultica(plan.args);
    for (const post of plan.postArgs ?? []) runMultica(post.args);
    if (plan.kind === "skill" && plan.action === "create" && plan.skill) {
      const created = parseJSONObject(output, "skill");
      const skillId = requiredString(created.id, "skill.id");
      const context = plan.context ?? [];
      for (const [relativePath, absolutePath] of skillFilesOnDisk(plan.skill.path)) {
        if (relativePath === "SKILL.md") continue;
        runMultica(skillFileUpsertArgs(skillId, relativePath, absolutePath, context));
      }
    }
    if (plan.avatarPath && plan.avatarHash && plan.avatarStatePath && plan.avatarKey) {
      const hashes = readAvatarHashes(plan.avatarStatePath);
      hashes[plan.avatarKey] = plan.avatarHash;
      writeAvatarHashes(plan.avatarStatePath, hashes);
    }
    printPlan(plan);
  }
  for (const plan of plans.filter((candidate) => candidate.blocked)) printPlan(plan);
  if (blocked.length > 0) {
    throw new Error(
      `${blocked.length} record(s) were not applied. Add a server-only label or status to the manifest, or make a locked status match the server. Sync does not delete them.`,
    );
  }
}

/**
 * Per-project state outside every repo, the sibling of the worktrunk root
 * (~/.cursor/worktrees/<repo>): ~/.cursor/artifacts/<repo>. <repo> is the
 * directory name of the checkout that holds `.multica`, as worktrunk's
 * {{ repo }}. MULTICA_ARTIFACT_ROOT overrides the base.
 */
export function artifactRoot(projectRoot: string, env: NodeJS.ProcessEnv = process.env): string {
  const base = env.MULTICA_ARTIFACT_ROOT?.trim() || join(homedir(), ".cursor", "artifacts");
  return join(base, basename(resolve(projectRoot)));
}

export function avatarStatePath(projectRoot: string, env: NodeJS.ProcessEnv = process.env): string {
  return join(artifactRoot(projectRoot, env), "avatars", "state.json");
}

function readAvatarHashes(path: string): Record<string, string> {
  try {
    return JSON.parse(readFileSync(path, "utf8")) as Record<string, string>;
  } catch {
    return {};
  }
}

function writeAvatarHashes(path: string, hashes: Record<string, string>) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, `${JSON.stringify(hashes, null, 2)}\n`, { mode: 0o600 });
}

/** Upload only when the server has no avatar or the file changed since the last upload. */
export function avatarNeedsUpload(
  existingUrl: string,
  hashes: Record<string, string>,
  key: string,
  hash: string,
): boolean {
  return existingUrl === "" || hashes[key] !== hash;
}

function fileHash(path: string): string {
  return createHash("sha256").update(readFileSync(path)).digest("hex");
}

function agentAvatarArgs(agentId: string, avatarPath: string, context: string[]): string[] {
  return ["agent", "avatar", agentId, "--file", avatarPath, "--output", "json", ...context];
}

function buildAvatarPlans(
  manifest: MulticaManifest,
  state: LoadedState,
  context: string[],
  allowUnresolved: boolean,
  statePath: string,
): Plan[] {
  const plans: Plan[] = [];
  const hashes = readAvatarHashes(statePath);
  const workspaceKey = contextWorkspaceId(context) ?? "default";
  for (const desired of manifest.agents) {
    if (!desired.avatar) continue;
    const agent = state.agents.find((candidate) => candidate.name === desired.name);
    if (!agent) {
      if (!allowUnresolved) throw new Error(`agent not found after apply: ${desired.name}`);
      plans.push({
        kind: "agent",
        action: "update",
        name: `${desired.name}: avatar`,
        changes: ["pending"],
        args: [],
      });
      continue;
    }
    const existing = toAgentState(
      parseJSONObject(
        runMultica([...context, "agent", "get", agent.id, "--output", "json"]),
        "agent",
      ),
    );
    if (!existsSync(desired.avatar)) throw new Error(`avatar not found for ${desired.name}: ${desired.avatar}`);
    const hash = fileHash(desired.avatar);
    const key = `${workspaceKey}/${desired.name}`;
    if (!avatarNeedsUpload(existing.avatar_url, hashes, key, hash)) continue;
    plans.push({
      kind: "agent",
      action: "update",
      name: desired.name,
      changes: ["avatar"],
      diff: [{ field: "avatar", from: hashes[key] ? `sha256:${hashes[key].slice(0, 12)}` : existing.avatar_url ? "(unknown)" : "(none)", to: `sha256:${hash.slice(0, 12)} ${basename(desired.avatar)}` }],
      args: agentAvatarArgs(agent.id, desired.avatar, context),
      avatarPath: desired.avatar,
      avatarHash: hash,
      avatarStatePath: statePath,
      avatarKey: key,
    });
  }
  return plans;
}

// ---- gbot phase -------------------------------------------------------------

export interface GbotBotRecord {
  id: string;
  name: string;
  kind?: string;
  description?: string;
}

/** Looks up a bot by name or id; returns null when it does not exist. */
export type GbotLookup = (name: string) => GbotBotRecord | null;

function gbotBin(): string {
  return process.env.MULTICA_GBOT_PATH?.trim() || "gbot";
}

function runGbot(args: string[]): { status: number; stdout: string; stderr: string } {
  const result = spawnSync(gbotBin(), args, { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  if (result.error) throw new Error(`gbot ${args.slice(0, 2).join(" ")} failed: ${result.error.message}`);
  return { status: result.status ?? 1, stdout: result.stdout ?? "", stderr: result.stderr ?? "" };
}

export const gbotLookup: GbotLookup = (name) => {
  const result = runGbot(["bots", "get", name, "--json"]);
  if (result.status !== 0) {
    const detail = `${result.stderr}${result.stdout}`;
    if (/No bot or group named|not found/i.test(detail)) return null;
    throw new Error(`gbot bots get ${JSON.stringify(name)} failed: ${detail.trim().slice(0, 300)}`);
  }
  const parsed = parseJSONObject(result.stdout.trim(), "gbot bot");
  return {
    id: requiredString(parsed.id, "gbot bot.id"),
    name: requiredString(parsed.name, "gbot bot.name"),
    kind: optionalString(parsed.kind, "gbot bot.kind"),
    description: optionalString(parsed.description, "gbot bot.description") ?? "",
  };
};

export function isGbotAgent(agent: AgentManifest): boolean {
  return agent.runtime === "gbot" || agent.runtime.endsWith(":gbot");
}

/**
 * One plan per Grok Bot the manifest uses: create it when missing, update its
 * instructions when they differ. Bots are deduped by name; two agents that
 * name one bot must agree on its instructions.
 */
export function buildGbotPlans(
  manifest: MulticaManifest,
  lookup: GbotLookup,
  writeInstructions: (bot: string, text: string) => string,
): Plan[] {
  const wanted = new Map<string, { instructions?: string; agents: string[] }>();
  for (const agent of manifest.agents) {
    if (!isGbotAgent(agent)) {
      if (agent.gbot !== undefined || agent.gbot_instructions !== undefined) {
        throw new Error(`agent ${agent.name}: gbot/gbot_instructions need a gbot runtime`);
      }
      continue;
    }
    const bot = agent.gbot?.trim();
    if (!bot) throw new Error(`agent ${agent.name} runs on gbot but names no bot: add \`gbot: <bot name>\``);
    const entry = wanted.get(bot) ?? { agents: [] };
    if (
      agent.gbot_instructions !== undefined &&
      entry.instructions !== undefined &&
      entry.instructions.trim() !== agent.gbot_instructions.trim()
    ) {
      throw new Error(`gbot ${bot}: agents ${entry.agents.join(", ")} and ${agent.name} disagree on gbot_instructions`);
    }
    if (agent.gbot_instructions !== undefined) entry.instructions = agent.gbot_instructions;
    entry.agents.push(agent.name);
    wanted.set(bot, entry);
  }
  const plans: Plan[] = [];
  for (const [bot, entry] of wanted) {
    const existing = lookup(bot);
    const desired = entry.instructions?.replace(/\s+$/, "");
    if (!existing) {
      const args = ["bots", "create", "--name", bot, "--json"];
      plans.push({
        kind: "gbot-bot",
        action: "create",
        name: bot,
        changes: desired !== undefined ? ["instructions"] : [],
        diff: desired !== undefined ? [{ field: "instructions", from: "", to: desired }] : [],
        args: [],
        exec: () => {
          const full = desired !== undefined ? [...args, "--instructions-file", writeInstructions(bot, desired)] : args;
          const result = runGbot(full);
          if (result.status !== 0) throw new Error(`gbot bots create ${bot} failed: ${result.stderr.trim().slice(0, 300)}`);
        },
      });
      continue;
    }
    if (existing.kind === "group") throw new Error(`gbot ${bot} is a group, not a bot`);
    if (desired === undefined) continue;
    const current = (existing.description ?? "").replace(/\s+$/, "");
    if (current === desired) continue;
    plans.push({
      kind: "gbot-bot",
      action: "update",
      name: bot,
      changes: ["instructions"],
      diff: [{ field: "instructions", from: current, to: desired }],
      args: [],
      exec: () => {
        const result = runGbot(["bots", "update", existing.id, "--instructions-file", writeInstructions(bot, desired), "--json"]);
        if (result.status !== 0) throw new Error(`gbot bots update ${bot} failed: ${result.stderr.trim().slice(0, 300)}`);
      },
    });
  }
  return plans;
}

function instructionsWriter(projectRoot: string) {
  return (bot: string, text: string) => {
    const dir = join(artifactRoot(projectRoot), "tmp", "gbot");
    mkdirSync(dir, { recursive: true });
    const file = join(dir, `${bot.replace(/[^A-Za-z0-9._-]+/g, "-")}.instructions.md`);
    writeFileSync(file, `${text}\n`, { mode: 0o600 });
    return file;
  };
}

// ---- prune -----------------------------------------------------------------

/** Server agents and skills the merged manifest does not name. Archived agents are ignored. */
export function buildPrunePlans(manifest: MulticaManifest, state: Pick<LoadedState, "agents" | "skills">, context: string[]): Plan[] {
  const agents = new Set(manifest.agents.map((agent) => agent.name));
  const skills = new Set(manifest.skills.map((skill) => skill.name));
  const plans: Plan[] = [];
  for (const agent of state.agents) {
    if (agent.archived_at || agents.has(agent.name)) continue;
    plans.push({
      kind: "agent",
      action: "delete",
      name: agent.name,
      changes: ["archive"],
      args: [...context, "agent", "archive", agent.id, "--output", "json"],
      prune: true,
    });
  }
  for (const skill of state.skills) {
    if (skills.has(skill.name)) continue;
    plans.push({
      kind: "skill",
      action: "delete",
      name: skill.name,
      changes: ["delete"],
      args: [...context, "skill", "delete", skill.id, "--yes"],
      prune: true,
    });
  }
  return plans;
}

// ---- confirmation ------------------------------------------------------------

export type Ask = (question: string) => string | null;

const ttyAsk: Ask = (question) => {
  if (!process.stdin.isTTY) return null;
  return prompt(question);
};

/** True when the user (or --yes) approves. Without a TTY and without --yes this refuses. */
export function confirm(question: string, expected: string, yes: boolean, ask: Ask = ttyAsk): boolean {
  if (yes) return true;
  const answer = ask(`${question} Type "${expected}" to continue: `);
  if (answer === null) throw new Error("no TTY to confirm on; rerun with --apply --yes");
  return answer.trim() === expected;
}

function manifestFiles(path: string): string[] {
  if (!existsSync(path)) throw new Error(`manifest not found: ${path}`);
  try {
    const entries = readdirSync(path, { withFileTypes: true });
    return entries
      .filter((entry) => entry.isFile() && entry.name.endsWith(".yaml"))
      .map((entry) => join(path, entry.name))
      .sort();
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOTDIR") return [path];
    throw error;
  }
}

function workspaceCatalogs(): { path: string; manifest: MulticaManifest }[] {
  const directory = join(scriptDir(), "workspaces");
  if (!existsSync(directory)) return [];
  return manifestFiles(directory).map((path) => ({
    path,
    manifest: parseManifest(readFileSync(path, "utf8")),
  }));
}

function syncManifest(options: ReturnType<typeof parseArgs>, manifestPath: string) {
  const documents = parseManifestDocuments(readFileSync(manifestPath, "utf8"));
  const catalogs = workspaceCatalogs();
  const selected = documents.filter((document) => {
    if (!options.workspace || !document.workspace) return true;
    return (
      resolveWorkspaceId(options.workspace, options.profile) ===
      resolveWorkspaceId(document.workspace, options.profile)
    );
  });
  if (selected.length === 0) {
    throw new Error(`no workspace in ${manifestPath} matches ${options.workspace}`);
  }
  for (const document of selected) {
    syncWorkspace(options, manifestPath, document, catalogs);
  }
}

function syncWorkspace(
  options: ReturnType<typeof parseArgs>,
  manifestPath: string,
  projectDocument: MulticaManifest,
  catalogs: { path: string; manifest: MulticaManifest }[],
) {
  const globalManifest = resolveManifestSources(
    parseManifest(readFileSync(options.globalManifest, "utf8")),
    dirname(options.globalManifest),
  );
  const projectManifest = resolveManifestSources(projectDocument, dirname(manifestPath));
  const catalog = catalogs.find(
    (entry) =>
      entry.manifest.workspace !== undefined &&
      entry.manifest.workspace === projectManifest.workspace &&
      resolve(entry.path) !== resolve(manifestPath),
  );
  const catalogManifest = catalog
    ? resolveManifestSources(catalog.manifest, dirname(catalog.path))
    : undefined;
  const manifest = resolveSkillLibraries(
    applyInheritedSkills(
      applyDefaults(
        mergeManifests(
          catalogManifest ? mergeManifests(globalManifest, catalogManifest) : globalManifest,
          projectManifest,
        ),
      ),
    ),
  );
  validateManifest(manifest);
  const workspaceId = resolveWorkspaceId(manifest.workspace, options.profile);
  const context = contextArgs(workspaceId, options.profile);
  const state = loadState(context);

  const projectRoot = manifestProjectRoot(dirname(manifestPath));
  const statePath = avatarStatePath(projectRoot);
  const gbotPlans = buildGbotPlans(manifest, gbotLookup, instructionsWriter(projectRoot));
  const phase1 = [
    ...buildLabelPlans(manifest, state.labels, context),
    ...buildStatusPlans(manifest, state.statuses, context),
    ...buildSkillPlans(manifest, state, context),
    ...gbotPlans,
    ...buildAgentPlans(manifest, state, context),
    ...buildProjectPlans(manifest, state, context),
    ...buildSquadPlans(manifest, state, context),
  ];
  const prunePlans = buildPrunePlans(manifest, state, context);
  const preview = dedupePlans(phase1, [
    ...buildProjectPostPlans(manifest, state, context),
    ...buildSquadPostPlans(manifest, state, context),
    ...buildSquadMemberPlans(manifest, state, context, true),
    ...buildProjectResourcePlans(manifest, state, context, true),
    ...buildOrchestratorPlans(manifest, state, context, true),
    ...buildAgentSkillPlans(manifest, state, context, true),
    ...buildAvatarPlans(manifest, state, context, true, statePath),
  ]);
  const label = manifest.workspace ?? options.workspace ?? "default";
  console.log(`multica ${label} (${manifestPath}):`);
  for (const plan of [...phase1, ...preview, ...prunePlans]) printPlan(plan);
  const changes = phase1.length + preview.length;
  if (changes + prunePlans.length === 0) {
    console.log(`multica ${label}: no changes`);
    return;
  }
  console.log(
    `multica ${label}: ${changes} change(s)${prunePlans.length ? `, ${prunePlans.length} delete(s) ${options.prune ? "requested" : "shown only (add --prune)"}` : ""}`,
  );
  if (options.check) {
    process.exitCode = 1;
    return;
  }
  if (!options.apply) {
    console.log("dry run: nothing changed. Rerun with --apply to apply this plan.");
    return;
  }
  if (!confirm(`Apply ${changes} change(s) to ${label}?`, "yes", options.yes)) {
    console.log("not applied");
    return;
  }

  applyPlans(phase1);
  const nextState = loadState(context);
  const phase2 = [
    ...buildProjectPostPlans(manifest, nextState, context),
    ...buildSquadPostPlans(manifest, nextState, context),
    ...buildSquadMemberPlans(manifest, nextState, context, false),
    ...buildProjectResourcePlans(manifest, nextState, context, false),
    ...buildAgentSkillPlans(manifest, nextState, context, false),
    // after agents exist: the server validates the team's agent names
    ...buildOrchestratorPlans(manifest, nextState, context, false),
    ...buildAvatarPlans(manifest, nextState, context, false, statePath),
  ];
  applyPlans(phase2);
  let deleted = 0;
  if (options.prune && prunePlans.length > 0) {
    if (confirm(`Delete ${prunePlans.length} record(s) from ${label}: ${prunePlans.map((plan) => `${plan.kind} ${plan.name}`).join(", ")}?`, "delete", options.yes)) {
      applyPlans(prunePlans);
      deleted = prunePlans.length;
    } else {
      console.log("deletes not applied");
    }
  }
  console.log(
    `multica ${label}: ${phase1.length + phase2.length} change(s) applied (${phase1.filter((plan) => plan.action === "create").length + phase2.filter((plan) => plan.action === "create").length} created, ${phase1.filter((plan) => plan.action === "update").length + phase2.filter((plan) => plan.action === "update").length} updated, ${deleted} deleted)`,
  );
}

function main() {
  const options = parseArgs();
  for (const manifestPath of manifestFiles(options.manifest)) {
    syncManifest(options, manifestPath);
  }
}

if (import.meta.main) {
  try {
    main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exit(1);
  }
}

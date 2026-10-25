import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, test } from "vitest";
import {
  agentCreateArgs,
  agentDiff,
  gbotCustomArgs,
  agentUpdateArgs,
  applyDefaults,
  applyInheritedSkills,
  officialSkillsForAgent,
  catalogExtras,
  labelCreateArgs,
  labelDiff,
  statusDiff,
  mergeManifests,
  parseManifest,
  parseManifestDocuments,
  projectCreateArgs,
  projectDiff,
  projectResourceAddArgs,
  projectResourceRemoveArgs,
  projectResourceUpdateArgs,
  projectUpdateArgs,
  resolveLocalDirectoryPath,
  resolveManifestSources,
  resolveRuntimeId,
  skillFilesListArgs,
  skillGetArgs,
  squadCreateArgs,
  squadDiff,
  squadMemberAddArgs,
  squadMemberRemoveArgs,
  unmatchedSquadMembers,
  validateManifest,
  agentFieldDiff,
  artifactRoot,
  avatarNeedsUpload,
  avatarStatePath,
  buildGbotPlans,
  buildPrunePlans,
  confirm,
  formatDiff,
  lineDiff,
  parseArgs,
  resolveSkillLibraries,
  sha256Text,
  skillFileMatches,
} from "./sync";

const runtimes = [
  {
    id: "runtime-mac-pi",
    name: "Pi (Mac Studio)",
    runtime_mode: "local",
    provider: "pi",
  },
  {
    id: "runtime-mac-grok",
    name: "Grok (Mac Studio)",
    runtime_mode: "local",
    provider: "grok",
  },
  {
    id: "runtime-vps-grok",
    name: "Grok (vmiss-hk)",
    runtime_mode: "local",
    provider: "grok",
  },
];

describe("manifest parsing", () => {
  test("parses agents and projects", () => {
    const manifest = parseManifest(`
agents:
  - name: Frontend Reviewer
    runtime: mac:pi
    description: Reviews frontend changes
    model: grok-4.6
    custom_args:
      - --verbose
    access: workspace
    max_concurrent_tasks: 4
projects:
  - title: Platform
    status: in_progress
    start_date: 2026-08-01
    repos:
      - https://github.com/example/platform
`);
    expect(manifest.agents[0]).toMatchObject({
      name: "Frontend Reviewer",
      runtime: "mac:pi",
      model: "grok-4.6",
      access: "workspace",
      max_concurrent_tasks: 4,
    });
    expect(manifest.projects[0]).toMatchObject({
      title: "Platform",
      status: "in_progress",
      start_date: "2026-08-01",
    });
  });

  test("allows empty strings for clearing fields", () => {
    const manifest = parseManifest(`
agents:
  - name: Reviewer
    runtime: mac:pi
    model: ""
projects:
  - title: Platform
    description: ""
`);
    expect(manifest.agents[0]?.model).toBe("");
    expect(manifest.projects[0]?.description).toBe("");
  });

  test("rejects duplicates and invalid values", () => {
    const duplicate = parseManifest(`
agents:
  - name: Same
    runtime: pi
  - name: Same
    runtime: grok
`);
    expect(() => validateManifest(duplicate)).toThrow(/duplicate agent name/);
    expect(() =>
      parseManifest(`
projects:
  - title: X
    status: bogus
`),
    ).toThrow(/status must be one of/);
    expect(() =>
      parseManifest(`
projects:
  - title: X
    due_date: 2026/08/01
`),
    ).toThrow(/YYYY-MM-DD/);
  });
});

describe("runtime resolution", () => {
  test("resolves by id, provider, or device/provider", () => {
    expect(resolveRuntimeId(runtimes, "runtime-vps-grok")).toBe("runtime-vps-grok");
    expect(resolveRuntimeId(runtimes, "pi")).toBe("runtime-mac-pi");
    expect(resolveRuntimeId(runtimes, "mac:pi")).toBe("runtime-mac-pi");
    expect(resolveRuntimeId(runtimes, "vmiss/grok")).toBe("runtime-vps-grok");
  });

  test("rejects ambiguous provider selectors", () => {
    expect(() => resolveRuntimeId(runtimes, "grok")).toThrow(/ambiguous/);
  });
});

describe("gbot assignment", () => {
  test("pins the declared Grok bot without dropping other args", () => {
    expect(
      gbotCustomArgs({
        name: "Researcher",
        runtime: "mac:gbot",
        gbot: "Heavy Rust Master",
        custom_args: ["--verbose", "MULTICA_GBOT_BOT=old"],
      }),
    ).toEqual(["--verbose", "MULTICA_GBOT_BOT=Heavy Rust Master"]);
  });

  test("updates an agent when its Grok bot changes", () => {
    const changes = agentDiff(
      {
        id: "agent-1",
        name: "Researcher",
        runtime_id: "runtime-gbot",
        description: "",
        instructions: "",
        model: "",
        thinking_level: "",
        service_tier: "",
        custom_args: ["MULTICA_GBOT_BOT=Grok Bot"],
        permission_mode: "private",
        invocation_targets: [],
        max_concurrent_tasks: 1,
        avatar_url: "",
      },
      { name: "Researcher", runtime: "mac:gbot", gbot: "Heavy Rust Master" },
      "runtime-gbot",
    );
    expect(changes).toEqual(["gbot"]);
  });
});

describe("agent reconciliation", () => {
  const existing = {
    id: "agent-1",
    name: "Reviewer",
    runtime_id: "runtime-mac-pi",
    description: "old",
    instructions: "",
    model: "old-model",
    thinking_level: "",
    service_tier: "",
    custom_args: [],
    permission_mode: "private",
    invocation_targets: [],
    max_concurrent_tasks: 6,
  };

  test("finds only drifted fields", () => {
    expect(
      agentDiff(
        existing,
        { name: "Reviewer", runtime: "mac:pi", model: "new-model" },
        "runtime-mac-pi",
      ),
    ).toEqual(["model"]);
  });

  test("builds update args that clear a model", () => {
    const changes = agentDiff(
      existing,
      { name: "Reviewer", runtime: "mac:pi", model: "" },
      "runtime-mac-pi",
    );
    expect(changes).toEqual(["model"]);
    const args = agentUpdateArgs(
      "agent-1",
      { name: "Reviewer", runtime: "mac:pi", model: "" },
      "runtime-mac-pi",
      changes,
      "ws-1",
      undefined,
    );
    expect(args).toContain("--model");
    expect(args.at(args.indexOf("--model") + 1)).toBe("");
  });

  test("builds create args with workspace access", () => {
    const args = agentCreateArgs(
      {
        name: "Reviewer",
        runtime: "mac:pi",
        access: "workspace",
        custom_args: ["--verbose"],
      },
      "runtime-mac-pi",
      "ws-1",
      undefined,
    );
    expect(args).toEqual([
      "agent",
      "create",
      "--name",
      "Reviewer",
      "--runtime-id",
      "runtime-mac-pi",
      "--output",
      "json",
      "--custom-args",
      '["--verbose"]',
      "--permission-mode",
      "public_to",
      "--public-to-workspace",
      "--workspace-id",
      "ws-1",
    ]);
  });
});

describe("project reconciliation", () => {
  const existing = {
    id: "project-1",
    title: "Platform",
    description: "old",
    icon: "",
    status: "planned",
    start_date: "",
    due_date: "2026-09-01",
    lead_type: "agent",
    lead_id: "agent-1",
  };

  test("detects changed fields and clears dates", () => {
    const changes = projectDiff(
      existing,
      { title: "Platform", status: "in_progress", due_date: "" },
      undefined,
    );
    expect(changes).toEqual(["status", "due-date"]);
    const args = projectUpdateArgs(
      "project-1",
      { title: "Platform", status: "in_progress", due_date: "" },
      changes,
      "ws-1",
      undefined,
    );
    expect(args).toContain("--status");
    expect(args).toContain("--due-date");
  });

  test("builds create args with repos", () => {
    const args = projectCreateArgs(
      { title: "Platform", status: "in_progress", repos: ["https://github.com/example/platform"] },
      "ws-1",
      "staging",
    );
    expect(args).toEqual([
      "project",
      "create",
      "--title",
      "Platform",
      "--output",
      "json",
      "--status",
      "in_progress",
      "--repo",
      "https://github.com/example/platform",
      "--profile",
      "staging",
      "--workspace-id",
      "ws-1",
    ]);
  });
});

describe("manifest validation", () => {
  test("enforces max concurrent task bounds and repo URL shape", () => {
    const manifest = parseManifest(`
agents:
  - name: A
    runtime: pi
    max_concurrent_tasks: 99
projects:
  - title: P
    repos:
      - not-a-url
`);
    expect(() => validateManifest(manifest)).toThrow(/between 1 and 50/);
  });
});

describe("full project DSL", () => {
  const manifest = applyDefaults(
    parseManifest(`
defaults:
  agents:
    access: workspace
    max_concurrent_tasks: 4
  projects:
    status: in_progress
labels:
  - name: frontend
    color: "#0ea5e9"
agents:
  - name: lead
    runtime: mac:grok
  - name: implementer
    runtime: mac:pi
projects:
  - title: Verx
    lead: lead
    resources:
      - type: local_directory
        local_path: /workspace/verx
        runtime: mac:pi
        execution_mode: worktree
squads:
  - name: Verx Team
    leader: lead
    members:
      - type: agent
        ref: implementer
        role: implementation
      - type: member
        ref: Ray
        role: reviewer
`),
  );

  test("parses and validates the full project shape", () => {
    expect(() => validateManifest(manifest)).not.toThrow();
    expect(manifest.agents[0]).toMatchObject({ access: "workspace", max_concurrent_tasks: 4 });
    expect(manifest.projects[0]?.status).toBe("in_progress");
    expect(manifest.squads[0]?.members).toEqual([
      { type: "agent", ref: "implementer", role: "implementation" },
      { type: "member", ref: "Ray", role: "reviewer" },
    ]);
  });

  test("rejects missing local runtime selectors", () => {
    const invalidResource = parseManifest(`
agents:
  - name: lead
    runtime: mac:grok
projects:
  - title: Verx
    resources:
      - type: local_directory
        local_path: /tmp/verx
`);
    expect(() => validateManifest(invalidResource)).toThrow(/local_directory requires runtime/);
  });

  test("one file declares both workspaces and the project squad wins over the catalog", () => {
    const documents = parseManifestDocuments(`
agents:
  - name: yielded-researcher
    runtime: mac:yielded
workspaces:
  - workspace: ox-id
    agents:
      - name: grok-engineer
        runtime: mac:pi
    projects:
      - title: PocketJS
        resources:
          - type: local_directory
            local_path: .
            runtime: mac:pi
    squads:
      - name: OX
        leader: grok-engineer
  - workspace: studio-id
    agents:
      - name: frontend-engineer
        runtime: mac:pi
    squads:
      - name: Studio
        leader: frontend-engineer
`);
    expect(documents.map((document) => document.workspace)).toEqual(["ox-id", "studio-id"]);
    expect(documents.every((document) => document.agents.some((agent) => agent.name === "yielded-researcher"))).toBe(
      true,
    );
    expect(documents[1]?.agents.some((agent) => agent.name === "grok-engineer")).toBe(false);
    const catalog = parseManifest(`
workspace: ox-id
labels:
  - name: area:text
    color: "#8b5cf6"
squads:
  - name: OX
    leader: grok-engineer
    description: catalog copy
`);
    const merged = mergeManifests(catalog, documents[0]);
    expect(merged.labels.map((label) => label.name)).toEqual(["area:text"]);
    expect(merged.squads[0]?.leader).toBe("grok-engineer");
    expect(merged.squads[0]?.description).toBeUndefined();
    expect(merged.projects[0]?.resources?.[0]?.local_path).toBe(".");
  });

  test("resolves . to the repo that contains .multica", () => {
    const manifest = parseManifest(`
agents:
  - name: lead
    runtime: mac:pi
projects:
  - title: PocketJS
    resources:
      - type: local_directory
        local_path: .
        runtime: mac:pi
`);
    const resolved = resolveManifestSources(manifest, "/work/pocketjs/.multica");
    expect(resolved.projects[0]?.resources?.[0]?.local_path).toBe(resolve("/work/pocketjs"));
    expect(parseManifest("workspace: OX\nagents: []\nprojects: []\nsquads: []\nlabels: []\nskills: []\n").workspace).toBe(
      "OX",
    );
    expect(resolveLocalDirectoryPath("~/Workspace/boom/pocketjs", "/work/pocketjs/.multica")).toBe(
      resolve(homedir(), "Workspace/boom/pocketjs"),
    );
  });
});

describe("label and squad reconciliation", () => {
  test("builds label create/update args and detects drift", () => {
    expect(
      labelDiff(
        { id: "l1", name: "frontend", color: "#111111" },
        { name: "frontend", color: "#0ea5e9" },
      ),
    ).toEqual(["color"]);
    expect(labelCreateArgs({ name: "frontend", color: "#0ea5e9" }, "ws-1", undefined)).toEqual([
      "label",
      "create",
      "--name",
      "frontend",
      "--color",
      "#0ea5e9",
      "--output",
      "json",
      "--workspace-id",
      "ws-1",
    ]);
    expect(
      labelDiff(
        { id: "l1", name: "frontend", color: "#0ea5e9", description: "", resource_type: "issue" },
        { name: "frontend", color: "#0EA5E9", description: "UI" },
      ),
    ).toEqual(["description"]);
    expect(catalogExtras(["area:text", "area:docs"], ["area:docs"])).toEqual(["area:text"]);
    expect(
      statusDiff(
        {
          id: "s1",
          key: "backlog",
          name: "Backlog",
          description: "Parked.",
          category: "backlog",
          color: "#6b7280",
          is_system: true,
          archived: false,
        },
        {
          key: "backlog",
          name: "Backlog",
          description: "Parked.",
          category: "backlog",
          color: "#6b7280",
        },
      ),
    ).toEqual([]);
  });

  test("builds squad create/update/member args", () => {
    const squad = {
      name: "Verx Team",
      leader: "lead",
      description: "Team",
      instructions: "Review first.",
    };
    expect(squadCreateArgs(squad, "ws-1", undefined)).toContain("--leader");
    expect(
      squadDiff(
        {
          id: "s1",
          name: "Verx Team",
          description: "old",
          instructions: "",
          leader_id: "agent-2",
          avatar_url: "",
        },
        squad,
        "agent-1",
      ),
    ).toEqual(["description", "instructions", "leader"]);
    expect(squadMemberAddArgs("s1", "agent-1", "agent", "architect", "ws-1", undefined)).toContain(
      "--role",
    );
    expect(squadMemberRemoveArgs("s1", "agent-9", "agent", "ws-1", undefined)).toEqual([
      "squad",
      "member",
      "remove",
      "s1",
      "--member-id",
      "agent-9",
      "--type",
      "agent",
      "--output",
      "json",
      "--workspace-id",
      "ws-1",
    ]);
    expect(
      unmatchedSquadMembers(
        [
          { member_id: "lead", member_type: "agent", role: "leader" },
          { member_id: "old", member_type: "agent", role: "implementation" },
          { member_id: "keep", member_type: "agent", role: "research" },
        ],
        [{ id: "keep", type: "agent" }],
        "lead",
      ),
    ).toEqual([{ member_id: "old", member_type: "agent", role: "implementation" }]);
  });
});

describe("project resource reconciliation", () => {
  test("builds project resource add args", () => {
    const args = projectResourceAddArgs(
      "p1",
      {
        type: "local_directory",
        local_path: "/tmp/verx",
        runtime: "mac:pi",
        execution_mode: "worktree",
      },
      "daemon-1",
      "ws-1",
      undefined,
    );
    expect(args).toEqual([
      "project",
      "resource",
      "add",
      "p1",
      "--type",
      "local_directory",
      "--output",
      "json",
      "--ref",
      '{"local_path":"/tmp/verx","daemon_id":"daemon-1","execution_mode":"worktree"}',
      "--workspace-id",
      "ws-1",
    ]);
  });

  test("builds project resource update and remove args", () => {
    const update = projectResourceUpdateArgs(
      "p1",
      "r1",
      { type: "local_directory", local_path: "/tmp/verx", runtime: "mac:pi", label: "drift" },
      "daemon-1",
      ["label"],
      "ws-1",
      undefined,
    );
    expect(update).toEqual([
      "project",
      "resource",
      "update",
      "p1",
      "r1",
      "--output",
      "json",
      "--ref",
      '{"local_path":"/tmp/verx","daemon_id":"daemon-1","label":"drift"}',
      "--workspace-id",
      "ws-1",
    ]);
    expect(projectResourceRemoveArgs("p1", "r1", "ws-1", undefined)).toEqual([
      "project",
      "resource",
      "remove",
      "p1",
      "r1",
      "--output",
      "json",
      "--workspace-id",
      "ws-1",
    ]);
  });
});

describe("global and project manifest merging", () => {
  test("project entries override global entries while defaults combine", () => {
    const global = parseManifest(`
defaults:
  agents:
    access: private
    max_concurrent_tasks: 3
labels:
  - name: architecture
    color: "#111111"
agents:
  - name: lead
    runtime: mac-studio:grok
projects: []
squads: []
`);
    const project = parseManifest(`
defaults:
  agents:
    access: workspace
labels:
  - name: architecture
    color: "#2563eb"
agents:
  - name: lead
    runtime: mac-studio:pi
projects: []
squads: []
`);
    const merged = mergeManifests(global, project);
    expect(merged.labels).toEqual([{ name: "architecture", color: "#2563eb" }]);
    expect(merged.agents[0]).toMatchObject({ name: "lead", runtime: "mac-studio:pi" });
    const applied = applyDefaults(merged);
    expect(applied.agents[0]).toMatchObject({ access: "workspace", max_concurrent_tasks: 3 });
  });

  test("merges global and project skills and validates agent skill references", () => {
    const global = parseManifest(`
skills:
  - name: shared
    description: Shared skill
    path: /tmp/shared
agents: []
projects: []
squads: []
labels: []
`);
    const project = parseManifest(`
skills:
  - name: project-skill
    description: Project skill
    path: /tmp/project
agents:
  - name: lead
    runtime: mac-studio:grok
    skills:
      - shared
      - project-skill
projects: []
squads: []
labels: []
`);
    const merged = mergeManifests(global, project);
    expect(merged.skills.map((skill) => skill.name)).toEqual(["shared", "project-skill"]);
    expect(() => validateManifest(merged)).toThrow(/does not contain SKILL.md/);

    const withKnownSkill = parseManifest(`
agents:
  - name: lead
    runtime: mac-studio:grok
    skills:
      - missing
projects: []
squads: []
labels: []
`);
    expect(() => validateManifest(withKnownSkill)).toThrow(/references unknown skill/);
  });

  test("bind:all skills are inherited by every agent; listed stay role-only", () => {
    const merged = applyInheritedSkills(
      applyDefaults(
        mergeManifests(
          parseManifest(`
skills:
  - name: effect-best-practice
    bind: all
  - name: agent-browser
    bind: listed
agents: []
projects: []
squads: []
labels: []
`),
          parseManifest(`
skills:
  - name: testing
    bind: all
  - name: rn-macos-dev
    bind: listed
agents:
  - name: reviewer
    runtime: VMISS:grok
    skills:
      - tdd
  - name: frontend-engineer
    runtime: VMISS:grok
    skills:
      - rn-macos-dev
projects: []
squads: []
labels: []
`),
        ),
      ),
    );
    expect(officialSkillsForAgent(merged, "reviewer")).toEqual([
      "effect-best-practice",
      "testing",
      "tdd",
    ]);
    expect(officialSkillsForAgent(merged, "frontend-engineer")).toEqual([
      "effect-best-practice",
      "testing",
      "rn-macos-dev",
    ]);
    expect(officialSkillsForAgent(merged, "reviewer")).not.toContain("rn-macos-dev");
    expect(officialSkillsForAgent(merged, "frontend-engineer")).not.toContain("agent-browser");
  });

  test("project can promote a global skill to bind:all without dropping path", () => {
    const merged = mergeManifests(
      parseManifest(`
skills:
  - name: effect-best-practice
    bind: listed
    path: /tmp/effect
agents: []
projects: []
squads: []
labels: []
`),
      parseManifest(`
skills:
  - name: effect-best-practice
    bind: all
agents: []
projects: []
squads: []
labels: []
`),
    );
    expect(merged.skills).toEqual([
      {
        name: "effect-best-practice",
        bind: "all",
        path: "/tmp/effect",
      },
    ]);
  });
});

describe("Holt manifest fixture", () => {
  test("parses and validates the checked-in project manifest", () => {
    const url = new URL("./examples/holt/project.yaml", import.meta.url);
    const documents = parseManifestDocuments(readFileSync(url, "utf8"));
    expect(documents.map((document) => document.workspace)).toEqual([
      "0d35514a-7f4e-4e49-9e69-3026028608ad",
      "da70b409-e6e8-4ba5-932a-a143d85e9d6f",
    ]);
    const sourceDir = dirname(fileURLToPath(url));
    for (const document of documents) {
      const resolved = resolveManifestSources(applyDefaults(document), sourceDir);
      // Agent skills such as handoff-metadata come from the preset library, not this file.
      for (const skill of resolved.skills) {
        if (skill.path) expect(existsSync(join(skill.path, "SKILL.md")), skill.name).toBe(true);
      }
    }
    const [ox, studio] = documents;
    expect(ox?.projects.map((project) => project.title)).toEqual(["Holt"]);
    expect(studio?.projects.map((project) => project.title)).toEqual(["Studio Parts"]);
    expect(ox?.agents.map((agent) => agent.name)).toEqual([
      "yielded-lead",
      "yielded-researcher",
      "yielded-engineer",
      "gbot",
      "yielded-reviewer",
      "heavy-engineer",
      "heavy-engineer-2",
      "heavy-engineer-3",
      "heavy-engineer-4",
      "heavy-engineer-5",
      "heavy-engineer-6",
      "code-auditor",
    ]);
    expect(ox?.agents.find((agent) => agent.name === "gbot")?.gbot).toBe("Holt Expert");
    expect(ox?.projects[0]?.orchestrator).toMatchObject({ preset: "impl-review-merge" });
    expect(studio?.agents.map((agent) => agent.name)).toContain("frontend-engineer");
    expect(studio?.agents.map((agent) => agent.name)).toContain("control-plane-engineer");
    expect(ox?.agents.every((agent) => /:(yielded|gbot)$/.test(agent.runtime))).toBe(true);
  });
});

describe("skill content reads", () => {
  test("reads bodies without --with-content, which multica 42 rejects", () => {
    const context = ["--workspace-id", "ws-1"];
    expect(skillGetArgs("skill-1", context)).toEqual([
      "--workspace-id",
      "ws-1",
      "skill",
      "get",
      "skill-1",
      "--output",
      "json",
    ]);
    expect(skillFilesListArgs("skill-1", context)).toEqual([
      "--workspace-id",
      "ws-1",
      "skill",
      "files",
      "list",
      "skill-1",
      "--output",
      "json",
    ]);
  });
});


describe("dry run by default", () => {
  test("no flag means dry run; --apply/--yes/--prune are explicit", () => {
    const dry = parseArgs([]);
    expect(dry.apply).toBe(false);
    expect(dry.check).toBe(false);
    const apply = parseArgs(["--apply", "--yes", "--prune"]);
    expect([apply.apply, apply.yes, apply.prune]).toEqual([true, true, true]);
  });

  test("confirm needs the exact word, refuses without a TTY, and --yes skips it", () => {
    expect(confirm("Apply?", "yes", false, () => "yes")).toBe(true);
    expect(confirm("Apply?", "yes", false, () => "y")).toBe(false);
    expect(confirm("Delete?", "delete", false, () => "yes")).toBe(false);
    expect(() => confirm("Apply?", "yes", false, () => null)).toThrow(/--apply --yes/);
    expect(confirm("Apply?", "yes", true, () => null)).toBe(true);
  });
});

describe("field-level diff", () => {
  test("single-line fields print before -> after", () => {
    expect(formatDiff([{ field: "model", from: "a", to: "b" }])).toEqual(['    model: "a" -> "b"']);
  });

  test("multi-line fields print a line diff", () => {
    expect(lineDiff(["one", "two", "three"], ["one", "2", "three"])).toEqual(["  one", "- two", "+ 2", "  three"]);
    const lines = formatDiff([{ field: "instructions", from: "a\nb", to: "a\nc" }]);
    expect(lines[0]).toBe("    instructions:");
    expect(lines).toContain("      - b");
    expect(lines).toContain("      + c");
  });

  test("agent update plans carry from/to per changed field", () => {
    const existing = {
      id: "a1", name: "x", runtime_id: "r-old", description: "old", instructions: "", model: "m1",
      thinking_level: "", service_tier: "", custom_args: [], permission_mode: "private",
      invocation_targets: [], max_concurrent_tasks: 1, avatar_url: "",
    } as never;
    const diff = agentFieldDiff(existing, { name: "x", runtime: "mac:pi", description: "new", model: "m2" }, "r-new", ["runtime", "description", "model"]);
    expect(diff).toEqual([
      { field: "runtime", from: "r-old", to: "r-new" },
      { field: "description", from: "old", to: "new" },
      { field: "model", from: "m1", to: "m2" },
    ]);
  });
});

const gbotManifest = (extra = "") => parseManifest(`
agents:
  - name: gbot
    runtime: mac-studio:gbot
    gbot: Expert Bot
    gbot_instructions: |
      You are the expert.
      Line two.
${extra}
`);

describe("gbot phase", () => {
  const writer = (_bot: string, _text: string) => "/tmp/instructions.md";

  test("a gbot agent without a bot name is an error", () => {
    const manifest = parseManifest(`
agents:
  - name: gbot
    runtime: mac-studio:gbot
`);
    expect(() => buildGbotPlans(manifest, () => null, writer)).toThrow(/names no bot/);
  });

  test("a missing bot becomes a create plan with its instructions", () => {
    const plans = buildGbotPlans(gbotManifest(), () => null, writer);
    expect(plans).toHaveLength(1);
    expect(plans[0]).toMatchObject({ kind: "gbot-bot", action: "create", name: "Expert Bot", changes: ["instructions"] });
    expect(plans[0].diff?.[0].to).toBe("You are the expert.\nLine two.");
  });

  test("an existing bot with other instructions becomes an update with a diff", () => {
    const plans = buildGbotPlans(gbotManifest(), () => ({ id: "b1", name: "Expert Bot", kind: "bot", description: "You are the expert.\nOld line." }), writer);
    expect(plans[0]).toMatchObject({ action: "update", changes: ["instructions"] });
    expect(formatDiff(plans[0].diff ?? [])).toContain("      + Line two.");
  });

  test("matching instructions, or no gbot_instructions, plan nothing", () => {
    expect(buildGbotPlans(gbotManifest(), () => ({ id: "b1", name: "Expert Bot", description: "You are the expert.\nLine two.\n" }), writer)).toEqual([]);
    const plain = parseManifest(`
agents:
  - name: gbot
    runtime: mac-studio:gbot
    gbot: Expert Bot
`);
    expect(buildGbotPlans(plain, () => ({ id: "b1", name: "Expert Bot", description: "anything" }), writer)).toEqual([]);
  });

  test("two agents naming one bot must agree on its instructions", () => {
    const manifest = gbotManifest(`  - name: gbot-2
    runtime: mac:gbot
    gbot: Expert Bot
    gbot_instructions: something else
`);
    expect(() => buildGbotPlans(manifest, () => null, writer)).toThrow(/disagree/);
  });

  test("a group is not a bot, and gbot keys need a gbot runtime", () => {
    expect(() => buildGbotPlans(gbotManifest(), () => ({ id: "g", name: "Expert Bot", kind: "group" }), writer)).toThrow(/group/);
    const wrong = parseManifest(`
agents:
  - name: pi
    runtime: mac:pi
    gbot: Expert Bot
`);
    expect(() => buildGbotPlans(wrong, () => null, writer)).toThrow(/need a gbot runtime/);
  });
});

describe("prune", () => {
  test("only server records the manifest does not name, flagged as prune", () => {
    const manifest = parseManifest(`
skills:
  - name: keep
    path: ./skills/keep
agents:
  - name: kept
    runtime: mac:pi
`);
    const plans = buildPrunePlans(manifest, {
      agents: [
        { id: "1", name: "kept", archived_at: undefined },
        { id: "2", name: "stale", archived_at: undefined },
        { id: "3", name: "gone", archived_at: "2026-10-17T00:00:00Z" },
      ] as never,
      skills: [{ id: "s1", name: "keep" }, { id: "s2", name: "old" }] as never,
    }, ["--workspace-id", "w"]);
    expect(plans.map((plan) => `${plan.kind}:${plan.name}:${plan.prune}`)).toEqual(["agent:stale:true", "skill:old:true"]);
    expect(plans[0].args).toContain("archive");
  });
});

describe("artifact root", () => {
  test("sibling of the worktrunk root, keyed by the checkout directory name", () => {
    expect(artifactRoot("/Users/kee/Workspace/boom/holt", { MULTICA_ARTIFACT_ROOT: "/state" })).toBe("/state/holt");
    expect(artifactRoot("/x/holt", {})).toBe(`${homedir()}/.cursor/artifacts/holt`);
    expect(avatarStatePath("/x/holt", { MULTICA_ARTIFACT_ROOT: "/s" })).toBe("/s/holt/avatars/state.json");
  });

  test("avatars upload only when missing on the server or changed", () => {
    expect(avatarNeedsUpload("", { "w/a": "h" }, "w/a", "h")).toBe(true);
    expect(avatarNeedsUpload("https://x/a.png", { "w/a": "h" }, "w/a", "h")).toBe(false);
    expect(avatarNeedsUpload("https://x/a.png", { "w/a": "h" }, "w/a", "h2")).toBe(true);
    expect(avatarNeedsUpload("https://x/a.png", {}, "w/a", "h")).toBe(true);
  });
});

describe("skill libraries", () => {
  test("an undeclared agent skill comes from the first library that has it", () => {
    const { mkdtempSync, mkdirSync, writeFileSync } = require("node:fs");
    const { join } = require("node:path");
    const { tmpdir } = require("node:os");
    const root = mkdtempSync(join(tmpdir(), "skill-lib-"));
    for (const [lib, name] of [["a", "plan-split"], ["b", "plan-split"], ["b", "swarm"]]) {
      mkdirSync(join(root, lib, name), { recursive: true });
      writeFileSync(join(root, lib, name, "SKILL.md"), `---\nname: ${name}\ndescription: from ${lib}\n---\nbody\n`);
    }
    const manifest = { ...parseManifest(`
agents:
  - name: lead
    runtime: mac:pi
    skills: [plan-split, swarm]
`), skill_libraries: [join(root, "a"), join(root, "b")] };
    const resolved = resolveSkillLibraries(manifest);
    expect(resolved.skills.map((skill) => [skill.name, skill.description, skill.bind])).toEqual([
      ["plan-split", "from a", "listed"],
      ["swarm", "from b", "listed"],
    ]);
    expect(() => validateManifest(resolved)).not.toThrow();
    const missingLib = { ...manifest, skill_libraries: [join(root, "nope")] };
    expect(() => resolveSkillLibraries(missingLib)).toThrow(/preset export/);
  });
});

describe("skill content compare", () => {
  test("uses content_hash when the server omits bodies", () => {
    expect(sha256Text("abc")).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    expect(skillFileMatches({ content: "", content_hash: sha256Text("x\n") }, "x\n")).toBe(true);
    expect(skillFileMatches({ content: "", content_hash: sha256Text("x") }, "y")).toBe(false);
    expect(skillFileMatches({ content: "same", content_hash: "" }, "same")).toBe(true);
    expect(skillFileMatches(undefined, "x")).toBe(false);
  });
});

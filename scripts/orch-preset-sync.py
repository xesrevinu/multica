#!/usr/bin/env python3
"""Push the rendered orchestrator preset to a team's agents in one workspace.

  scripts/orch-preset-sync.py -f project.yaml [--apply] [--cli multica] [-- <cli global flags>]

For the spec's lead / owners / reviewer it
  1. renders each agent's instructions (`team preset render --out`),
  2. exports the preset (`team preset export`) and creates or updates every
     skill a role mounts (SKILL.md plus extra files such as scripts/),
  3. sets each agent's instructions and skills: the rendered skills, plus any
     already attached skill that is not a pstack skill (pstack skills a role
     no longer mounts are detached - that is the slimming).
Without --apply it only prints the plan. Use the CLI's own profile/workspace
flags after `--` (e.g. `-- --profile dev --workspace-id <id>`); nothing here
reads tokens. Re-run after changing the preset or manifest.
"""
import argparse, json, os, re, subprocess, sys, tempfile

ap = argparse.ArgumentParser()
ap.add_argument("-f", "--file", required=True, help="spec (project.yaml with orchestrator:, or bare spec)")
ap.add_argument("--apply", action="store_true")
ap.add_argument("--cli", default=os.environ.get("MULTICA", "multica"))
ap.add_argument("--preset", default="impl-review-merge")
ap.add_argument("rest", nargs="*", help="global CLI flags after --")
a = ap.parse_args()


def cli(*args, js=False):
    cmd = [a.cli, *a.rest, *args] + (["--output", "json"] if js else [])
    r = subprocess.run(cmd, capture_output=True, text=True)
    if r.returncode != 0:
        sys.exit(f"failed: {' '.join(args[:3])}: {r.stderr.strip()[:400]}")
    return json.loads(r.stdout) if js else r.stdout


def items(d, key):
    return d if isinstance(d, list) else d.get(key) or d.get("items") or []


tmp = tempfile.mkdtemp(prefix="orch-preset-")
agents_dir, export_dir = os.path.join(tmp, "agents"), os.path.join(tmp, "preset")
plan = {}  # agent -> (role, [skills], instructions file)
for line in subprocess.run([a.cli, "team", "preset", "render", "-f", a.file, "--out", agents_dir],
                           capture_output=True, text=True, check=True).stdout.splitlines():
    m = re.match(r"(.+?\.md)\t(\w+)\tskills: (.*)$", line)
    if m:
        plan[os.path.basename(m[1])[:-3]] = (m[2], [s.strip() for s in m[3].split(",") if s.strip()], m[1])
subprocess.run([a.cli, "team", "preset", "export", "--preset", a.preset, export_dir], capture_output=True, check=True)

# skill name -> exported directory (preset skills, then pstack keep/adapt)
managed, pstack = {}, set()
for root in ("skills", "pstack/skills"):
    base = os.path.join(export_dir, root)
    for n in sorted(os.listdir(base)) if os.path.isdir(base) else []:
        if os.path.isfile(os.path.join(base, n, "SKILL.md")):
            managed.setdefault(n, os.path.join(base, n))
            if root == "pstack/skills":
                pstack.add(n)
needed = sorted({s for _, sk, _ in plan.values() for s in sk})
missing = [s for s in needed if s not in managed]
if missing:
    sys.exit(f"rendered skills not in the export: {missing}")


def description(path):
    m = re.search(r"^description:\s*(.+)$", open(path).read(), re.M)
    return (m[1].strip().strip('"') if m else "")[:500]


have = {s["name"]: s["id"] for s in items(cli("skill", "list", js=True), "skills")}
print(f"skills: {len(needed)} mounted; create {sum(s not in have for s in needed)}, update {sum(s in have for s in needed)}")
for n in needed:
    d = managed[n]
    extra = [os.path.relpath(os.path.join(r, f), d) for r, _, fs in os.walk(d) for f in fs if f != "SKILL.md"]
    print(f"  {'update' if n in have else 'create'} {n}" + (f" (+{', '.join(sorted(extra))})" if extra else ""))
    if not a.apply:
        continue
    md = os.path.join(d, "SKILL.md")
    if n in have:
        cli("skill", "update", have[n], "--content-file", md, "--description", description(md), js=True)
    else:
        have[n] = cli("skill", "create", "--name", n, "--description", description(md), "--content-file", md, js=True)["id"]
    for rel in sorted(extra):
        cli("skill", "files", "upsert", have[n], "--path", rel, "--content-file", os.path.join(d, rel), js=True)

agents = {x["name"]: x for x in items(cli("agent", "list", js=True), "agents") if not x.get("archived_at")}
for name, (role, skills, instr) in plan.items():
    ag = agents.get(name)
    if not ag:
        print(f"agent {name}: MISSING in workspace, skipped")
        continue
    cur = [s["name"] for s in ag.get("skills") or []]
    # Only pstack skills the role no longer mounts are detached; preset and
    # hand-attached skills stay.
    keep = [s for s in cur if s not in pstack]
    want = skills + [s for s in keep if s not in skills]
    drop = [s for s in cur if s not in want]
    add = [s for s in want if s not in cur]
    print(f"agent {name} ({role}): skills {len(cur)} -> {len(want)}; +{add or '-'} -{drop or '-'}; instructions {len(ag.get('instructions') or '')} -> {os.path.getsize(instr)} bytes")
    if a.apply:
        cli("agent", "update", ag["id"], "--instructions", open(instr).read(), js=True)
        cli("agent", "skills", "set", ag["id"], "--skill-ids", ",".join(have[s] if s in have else s for s in want), js=True)
print("applied" if a.apply else "dry run (add --apply)")

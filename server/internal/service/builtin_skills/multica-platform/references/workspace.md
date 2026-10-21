# Mentions, agents, autopilots, projects, runtimes, skill import

`multica <noun> --help` covers the flags. These are the traps.

## Mentions

`[@Name](mention://<type>/<uuid>)`. Only `agent` mentions start a run; `member`
and `issue` only render a link. A name instead of a UUID never parses; a
wrong UUID is reported blocked with `invocation_not_allowed`, identical to a
real permission denial, so check the UUID against `agent list` before touching
any setting. Nothing errors: read `trigger_outcomes` in the comment response
(`blocked`, `coalesced` into a pending task, `deferred`). In an orchestrated
project mentions start nothing for the engine.

## Agents

- Only `instructions` reaches the runtime; `description` is a label.
- `agent create` binds no skills. `skills set` replaces all bindings; `add` adds.
- `agent update` rejects `custom_env` (use the env endpoint); `agent get` shows
  only key counts. `--mcp-config null` clears MCP config.
- An invalid model/thinking combination fails at run time, not at create.
- Writes that change secrets, runtimes or visibility need the user's approval.

## Autopilots

`create_issue` files an issue each tick (and starts its assignee); `run_only`
runs the agent with no issue. "Why didn't it run": check it is enabled, the
next run time, and the agent's runtime is online.

## Projects

Resources (`github_repo`, `local_directory`) decide the checkout a run gets.
Wrong files in a run usually mean a missing or wrong resource, not a bug.
Refer to a project with its mention link from `project list`. A project
workflow restricts statuses and can hand off on a status change.

## Runtimes

A run needs an online runtime for the agent's provider (`runtime list`).
`multica repo checkout` is the only supported way to get another repo inside
a task. A queued task with no online runtime waits; it does not fail.

## Skill import

`multica skill import --url <url> --output json` (or an archive). A same-name
skill fails by default (409 `existing_skill`); `--on-conflict overwrite` keeps
its id and bindings, `rename` imports a copy, `skip` leaves it. Bind with
`multica agent skills add`; `set` is the replace-all path. Read back with
`skill get --with-content`.

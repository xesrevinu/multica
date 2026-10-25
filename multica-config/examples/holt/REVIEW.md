# REVIEW.md — what review means in holt

Read by the reviewer on every orchestrated review. Short on purpose.

## What failure looks like here
- **Host**: a window, CAMetalLayer or AppKit object created outside `crates/host-apple`; a DrawList or retained-tree change that renders differently on macos-app vs web-gpu; a frame-time regression without a number (`principle-explain-the-number`).
- **Compiler / framework**: Solid, Vue Vapor and Octane no longer compiling to the same retained tree; a test that only asserts the implementation shape.
- **Studio**: studio/app or studio/control-plane editing the runtime host instead of filing an OX contract issue; a screen change with no screenshot in `.build/validation/`.
- **Docs**: they lie (describe behavior the code does not have). **Config**: it misroutes.

## Risk (metadata `risk`, set by the reviewer from the change, not the author)
- **high** (forces Ray's acceptance): `crates/host-*` unsafe/FFI, `.github/workflows/**`, release and bundle scripts, installers/updaters, `studio/control-plane` license certificates, update channel or analytics/PII, `flake.nix` / `.envrc` toolchain, anything that sets a pattern other areas will copy.
- **medium**: ordinary feature or bug fix in one area.
- **low**: docs, tests only, internal tooling, additive change behind a flag.

## Findings
- Each finding: file:line, a concrete failing case, the minimal fix, and its bug class.
- Tags: `important` blocks; `nit` at most 3 and never blocks; `pre-existing` never blocks (note it for the lead).
- Skip: `.build/`, generated bindings, lockfiles (unless a dependency was added), snapshot files the PR regenerated on purpose.
- Start the review comment with the count ("1 important, 0 nit, 2 pre-existing") or "nothing to fix".

## Environment (the review runs where it was launched)
- The daemon starts you in the task's worktree with its direnv environment already loaded. Review there: no `cd` to another checkout, no new worktree (`wt switch --create`, `git worktree add`), no `direnv exec`/`nix develop`, no `bun install`, no toolchain/SDK hunting (`which`, `rustup`, `xcrun`, `DEVELOPER_DIR`).
- Read the diff and run the checks the change touches (`holt-checks` scope). Do not cold-build release/dist profiles or re-run a whole measurement to verify a number; if a number needs re-measuring, make it an `important` finding with the exact command and let the owner run it.
- If a tool the check needs is missing from the environment, say so in the verdict instead of installing or searching for it.

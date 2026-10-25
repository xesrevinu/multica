---
name: holt-checks
description: Pick and run the checks that cover a holt change by area, the same way for the owner and the independent reviewer. Use in orchestrated Holt and Studio runs.
---

# Holt checks

1. Area from the issue label (`area:*`) and the changed paths (`git diff --name-only origin/main...HEAD`).
2. Run the checks for every touched area, and only those. Never assemble cargo or tsc by hand. Run in a wt-prepared worktree (toolchain via `direnv exec . <cmd>` if not loaded); never copy `node_modules`/`vendor` in by hand.

| Paths                                           | Check                                                                                                                                                                                                           |
| ----------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| crates/, host code, compiler/native-plan/apple/ | `bun run test --project host`; plus one `pocket build apps/hero --target macos-app` (owner), or (reviewer) only when the diff changes the lowered project/xcodebuild behavior and the unit tests cannot show it |
| compiler/                                       | `bun run test --project compiler`                                                                                                                                                                               |
| framework/                                      | `bun run test --project framework`                                                                                                                                                                              |
| cli/                                            | `bun run test --project cli`                                                                                                                                                                                    |
| apps/<app>/                                     | `pocket check apps/<app>`                                                                                                                                                                                       |
| studio/app/                                     | `pocket dev studio`, check the named screen, screenshot into `studio/app/.build/validation/<KEY>/`                                                                                                              |
| studio/control-plane/                           | that package's tests                                                                                                                                                                                            |
| any TS source                                   | `bun check`                                                                                                                                                                                                     |

3. Put each command and the tail of its output in the result comment. A check you could not run is reported as not verified, with the reason.
4. Reviewer: a full macOS app build (minutes) is not a default gate. Docs-only, TS-only (cli/, framework/, apps/) or test-only changes never need it; prefer the area tests plus a direct probe of the changed function.
5. Screenshots and logs stay in ignored `.build/validation/`; never commit them.

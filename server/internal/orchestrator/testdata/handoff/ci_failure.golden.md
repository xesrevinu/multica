## Orchestrator handoff: ci_failure
This issue came back to you (owner). Address this before anything else.

- reason: precheck checks_red: test=FAILURE
- attempt: 2 (impl=2 infra=0 quota=0 review=1 unblock_count=0)
- previous head: 0123456789ab (reviewed: fedcba987654)
- PR: https://github.com/x/y/pull/7

### Previous diff
2 files, +10 -3: src/a.js (+8 -1), test/a.test.js (+2 -2)

### Review findings
- [github_inline rev] `src/a.js:12` [important] off-by-one loop skips last

### Failing checks
- test: failure https://github.com/x/y/runs/1
  - failure test/a.test.js:4 expected 3 got 2
  ```
1 failing
  ```

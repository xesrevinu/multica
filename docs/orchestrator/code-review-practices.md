# Code-review practices folded into the orchestrator

Source: "How the Claude Code team does code review" (claude.dev, 2026-10-15; the
URL `solving-code-review-at-anthropic` serves this post).

| Practice in the article | Where it lives here | Status |
|---|---|---|
| Verify before anyone looks (agent shows output, fresh-context self-review) | owner.md gates + pstack `principle-prove-it-works`, `tdd`, `unslop` | existing + pstack |
| Several finders, then a separate verification/vote; only verified findings post | reviewer.md step 4, review-verdict step 5, pstack `interrogate` (+ comment-sicko lens) | **new** |
| Drop style/cleanup-only findings | reviewer.md, review-verdict | **new** (style was already non-blocking) |
| Severity tags Important / Nit / Pre-existing; pre-existing not the author's fault | reviewer.md: `important` blocks, `nit` ≤3 never blocks, `pre-existing` → note to lead | **new** |
| Every finding answered (fix or reason) | owner.md step 2 | **new** |
| Summary starts with a count; say plainly "nothing to fix" | reviewer.md step 6, review-verdict | **new** |
| REVIEW.md written by code owners | reviewer reads `REVIEW.md` / `.multica/REVIEW.md` | **new** (PocketJS patch adds one) |
| Security review can block | `risk=high` → human acceptance; dedicated security lens in interrogate | partial: no separate security agent |
| Risk tiers set by the change, not the author; sensitive paths always need a person | metadata `risk` (low/medium/high), reviewer sets it; `risk=high` → `NeedsAcceptance` → T10 | **new engine** (guard validates values) |
| An agent's approval never replaces a required human | T10 acceptance is a member action (`flow accept`); agents cannot write `acceptance` | existing |
| Approval covers one commit; new commits need a new approval | `reviewed_sha == head`, T3b re-review, T3c patch-id carry | existing |
| Review must converge: round cap, then step back | `limits.impl` blocks to lead with "round cap: reconsider the approach"; lead.md handles it | **new wording + lead rule** |
| Diff-only re-review after the first pass; no repeats; flag the bug class | reviewer.md re-review rule | **new** |
| Merge queue re-runs checks against main | precheck merge with origin/main + checks rollup on head | existing |
| Incidents become a reviewer eval | replay harness scenarios (each bug becomes a scenario) | existing for the engine; no reviewer eval yet |
| Measure noise (fixed / wrong / intentional / ignored) and rounds-to-pass | not implemented | backlog: activity_log already has the T3 count per issue; a report could derive rounds-to-pass |
| Nightly review of existing code | not implemented | backlog (could be a lead-planned recurring issue) |

Not adopted: trusted-author lists (every agent author is untrusted by design;
the reviewer is always independent), feature flags and staged release (that is
project policy, not the orchestrator's).

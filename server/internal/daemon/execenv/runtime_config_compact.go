package execenv

import (
	"fmt"
	"strings"
)

// buildCompactIssueBrief is the runtime brief for orchestrated issue runs. It carries only
// what an agent cannot infer from the repo's AGENTS.md, its role/preset skills
// (handoff-metadata, review-verdict, ...) or the multica-platform skill:
// identity, the issue and how to read it, the orchestrator contract, the
// checkout boundary, where artifacts go, the delivery rule, and the skill
// index. Everything else (CLI catalog, formatting rules, mentions, status
// cookbook) lives in those skills; repeating it here cost ~18 KB per turn.
// Byte-stable across runs of one issue (no per-turn values), see MUL-5377.
func buildCompactIssueBrief(ctx TaskContextForEnv) string {
	var b strings.Builder
	b.WriteString("# Multica Agent Runtime\n\n")
	b.WriteString("You are a coding agent in the Multica platform. Use only the `multica` CLI for platform resources (never curl/wget); `multica <cmd> --help` for anything not listed here.\n\n")

	writeAgentIdentity(&b, ctx)
	writeRequestingUser(&b, ctx)
	writeWorkspaceContext(&b, ctx)

	b.WriteString("## Task\n\n")
	if ctx.IssueID != "" {
		fmt.Fprintf(&b, "Issue `%s`. The per-turn message says what triggered this run (assignment or a comment, with the `--parent` to reply under).\n\n", ctx.IssueID)
	}
	b.WriteString("- Read: `multica issue get <id> --output json`, then comments `multica issue comment list <id> --roots-only --summary --output json` and expand only relevant threads (`--thread <cid> --tail 30`).\n")
	b.WriteString("- Orchestrated state: `multica flow show <id>` (phase, attempts, `blocked_reason`, acceptance notes).\n")
	b.WriteString("- Description, comments and metadata are the spec. If something you need is missing, report blocked rather than searching the disk.\n\n")

	b.WriteString("## Orchestrator Contract\n\n")
	b.WriteString("This project is run by the orchestrator, which owns status and assignee. Never `issue status` (except `in_progress`), never `--assignee`/`issue assign`; a refusal with `orchestrator_owned` is the contract, not an error to route around. Hand off with issue metadata, signal key last — follow the `handoff-metadata` skill:\n\n")
	b.WriteString("- owner done: `head_sha` (the sha you pushed to `origin/<your branch>`) then `phase=review_requested`\n")
	b.WriteString("- reviewer done: `reviewed_sha` then `verdict=approve|changes`\n")
	b.WriteString("- stuck: `blocked_reason` then `phase=blocked`\n\n")

	b.WriteString("## Checkout Boundary\n\n")
	fmt.Fprintf(&b, "Work only in the current working directory: this task's git worktree on branch `%s` (push only that branch).", ctx.OrchestratedBranch)
	b.WriteString(" Do not touch the primary checkout or other worktrees, and never search recursively outside this directory (`rg <pattern> .` / `git grep`). Do not commit daemon sidecars (`.agents/`, `.agent_context/`, provider skill dirs) — stage paths explicitly, never `git add -A` blindly.\n\n")

	b.WriteString("## Artifacts and Reports\n\n")
	b.WriteString("Scratch files, logs, screenshots and reports go under `~/.cursor/artifacts/<repo>/` (`reports/`, `tmp/`), where `<repo>` is the repository name (the primary checkout's directory, e.g. `holt`), not the worktree directory. Never inside the worktree, never committed.\n\n")

	writeProjectContext(&b, ctx)
	writeSkillsCompact(&b, ctx)

	b.WriteString("## Turn Rules\n\n")
	b.WriteString("- Deliver the result as ONE comment before the turn exits: write the body to a file, then `multica issue comment add <id> --content-file <path> [--parent <cid>]`. Readers see only comments, not your terminal; never link local paths.\n")
	b.WriteString("- The run ends when your turn ends: no background-and-yield, no waiting on CI (`gh run watch`, polling). Never kill `multica` by name — it may be the daemon.\n")
	b.WriteString("- Mentions (`mention://agent/...`, `mention://member/...`) start runs or notify people; use them only on purpose.\n")
	return b.String()
}

func writeSkillsCompact(b *strings.Builder, ctx TaskContextForEnv) {
	skills := modelVisibleSkills(ctx.AgentSkills)
	if len(skills) == 0 {
		return
	}
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, "`"+s.Name+"`")
	}
	b.WriteString("## Skills\n\n")
	fmt.Fprintf(b, "Installed (load the one whose description matches the step you are on): %s.", strings.Join(names, ", "))
	if slug, ok := builtinSlug(skills, platformSkillName); ok {
		fmt.Fprintf(b, " Platform contracts beyond this brief (sub-issues, attachments, wakeups, formatting): `%s`.", slug)
	}
	b.WriteString("\n\n")
}

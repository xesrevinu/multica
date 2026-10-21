#!/usr/bin/env bash
# pr-review-comment.sh <pr-number> <approve|changes> <sha> <issue-url>
# The reviewer's only GitHub write: a one-line PR review *comment* pointing at
# the multica issue, where the full verdict lives. Never approve /
# request-changes, never a commit status or check run; the orchestrator
# publishes the "review-verdict" status itself, as the GitHub App.
set -euo pipefail
die() { echo "pr-review-comment: refused: $*" >&2; exit 2; }
[ $# -eq 4 ] || die "usage: pr-review-comment.sh <pr-number> <approve|changes> <sha> <issue-url>"
pr=$1 verdict=$2 sha=$3 url=$4
[[ $pr =~ ^[0-9]+$ ]] || die "PR number must be numeric, got '$pr'"
[[ $verdict == approve || $verdict == changes ]] || die "verdict must be approve or changes, got '$verdict'"
[[ $sha =~ ^[0-9a-f]{7,40}$ ]] || die "sha must be hex, got '$sha'"
[[ $url =~ ^https?://[^[:space:]]+$ ]] || die "issue url must be an http(s) URL"
exec gh pr review "$pr" --comment --body "Review verdict: **$verdict** on \`${sha:0:12}\`. Full verdict: $url"

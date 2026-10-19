#!/usr/bin/env bash
# pr-review-comment.sh <pr-number> <body-file>
# The reviewer's only GitHub write: a PR review *comment* (never approve /
# request-changes, never a commit status or check run; the orchestrator
# publishes the "review-verdict" status itself, as the GitHub App).
set -euo pipefail
die() { echo "pr-review-comment: refused: $*" >&2; exit 2; }
[ $# -eq 2 ] || die "usage: pr-review-comment.sh <pr-number> <body-file>"
pr=$1 body=$2
[[ $pr =~ ^[0-9]+$ ]] || die "PR number must be numeric, got '$pr'"
[ -s "$body" ] || die "body file '$body' is missing or empty"
exec gh pr review "$pr" --comment --body-file "$body"

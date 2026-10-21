package execenv

import "strings"

type commitIdentity struct {
	name, email string
	trailer     string // full "Co-authored-by: N <E>" or ""
}

// baselineIdentityArgs makes the baseline commit match the agent's own
// commits when the GitHub App identity is on: authored by the bot, with the
// human (global git identity) as Co-authored-by. The baseline is written while
// the worktree is prepared, before the daemon mints the App token, so this
// looks at the same preconditions applyAgentGitHubIdentity checks (switch on,
// github.com origin). nil keeps the previous behaviour.
func baselineIdentityArgs(worktreePath string) *commitIdentity {
	if !AgentGitHubAppEnabled() {
		return nil
	}
	url, err := runGitTrimmed(worktreePath, "remote", "get-url", "origin")
	if err != nil || !strings.Contains(url, "github.com") {
		return nil
	}
	name, email := AgentGitIdentity()
	id := &commitIdentity{name: name, email: email}
	if hn, he := humanCoAuthor(); hn != "" && he != "" && !strings.EqualFold(he, email) &&
		!strings.ContainsAny(hn+he, "<>\"\n\r") {
		id.trailer = "Co-authored-by: " + hn + " <" + he + ">"
	}
	return id
}

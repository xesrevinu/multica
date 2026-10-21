package execenv

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Agent GitHub identity: when the server can mint a GitHub App token for the
// task's repo, agent commits are authored as the App's bot user and git/gh
// authenticate with that token, so automation stops acting as the human.
// MULTICA_AGENT_GITHUB_APP=0 turns it off.
const (
	defaultAgentGitName  = "woolpup[bot]"
	defaultAgentGitEmail = "187665180+woolpup[bot]@users.noreply.github.com"
)

// AgentGitHubAppEnabled reports whether the daemon should try the App identity.
func AgentGitHubAppEnabled() bool {
	return strings.TrimSpace(os.Getenv("MULTICA_AGENT_GITHUB_APP")) != "0"
}

// AgentGitIdentity is the bot author for agent commits.
func AgentGitIdentity() (name, email string) {
	name = strings.TrimSpace(os.Getenv("MULTICA_AGENT_GIT_NAME"))
	email = strings.TrimSpace(os.Getenv("MULTICA_AGENT_GIT_EMAIL"))
	if name == "" || email == "" {
		return defaultAgentGitName, defaultAgentGitEmail
	}
	return name, email
}

// agentGitHelper reads the token from the agent's environment at use time,
// so the value is never written to disk.
const agentGitHelper = `!f() { test "$1" = get || exit 0; test -n "${GH_TOKEN:-}" || exit 0; echo username=x-access-token; echo "password=${GH_TOKEN}"; }; f`

// ConfigureWorktreeGitHubIdentity sets the bot author and token-based GitHub
// auth in this worktree's own config (extensions.worktreeConfig), leaving the
// user's global config and other worktrees untouched. SSH GitHub remotes are
// rewritten to HTTPS for this worktree so pushes use the App token.
func ConfigureWorktreeGitHubIdentity(workDir string) error {
	name, email := AgentGitIdentity()
	steps := [][]string{
		{"config", "extensions.worktreeConfig", "true"},
		{"config", "--worktree", "user.name", name},
		{"config", "--worktree", "user.email", email},
		{"config", "--worktree", "--unset-all", "credential.https://github.com.helper"},
		{"config", "--worktree", "--add", "credential.https://github.com.helper", ""},
		{"config", "--worktree", "--add", "credential.https://github.com.helper", agentGitHelper},
		{"config", "--worktree", "--replace-all", "url.https://github.com/.insteadOf", "git@github.com:"},
		{"config", "--worktree", "--add", "url.https://github.com/.insteadOf", "ssh://git@github.com/"},
	}
	for i, args := range steps {
		cmd := exec.Command("git", args...)
		cmd.Dir = workDir
		if out, err := cmd.CombinedOutput(); err != nil {
			// --unset-all exits 5 when nothing was set.
			if i == 3 {
				continue
			}
			return fmt.Errorf("execenv: git %s: %v: %s", args[1], err, firstLine(string(out)))
		}
	}
	// --replace-all then --add would duplicate on reruns; normalise.
	return dedupeWorktreeValues(workDir, "url.https://github.com/.insteadOf", "git@github.com:", "ssh://git@github.com/")
}

func dedupeWorktreeValues(workDir, key string, values ...string) error {
	cmd := exec.Command("git", "config", "--worktree", "--unset-all", key)
	cmd.Dir = workDir
	_ = cmd.Run()
	for _, v := range values {
		c := exec.Command("git", "config", "--worktree", "--add", key, v)
		c.Dir = workDir
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("execenv: git config %s: %v: %s", key, err, firstLine(string(out)))
		}
	}
	return nil
}

// humanCoAuthor is the user's own git identity (global config, read only),
// used as Co-authored-by when the bot is the commit author.
func humanCoAuthor() (string, string) {
	get := func(k string) string {
		out, err := exec.Command("git", "config", "--global", "--get", k).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	return get("user.name"), get("user.email")
}

// ClearWorktreeGitHubIdentity removes what ConfigureWorktreeGitHubIdentity
// set, so a worktree reused without an App token falls back to the user's
// own git identity and credentials. Missing keys are fine.
func ClearWorktreeGitHubIdentity(workDir string) {
	for _, k := range []string{"user.name", "user.email", "credential.https://github.com.helper", "url.https://github.com/.insteadOf"} {
		cmd := exec.Command("git", "config", "--worktree", "--unset-all", k)
		cmd.Dir = workDir
		_ = cmd.Run()
	}
}

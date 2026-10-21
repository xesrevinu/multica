package execenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// The bot identity lives only in the linked worktree's config: the main
// checkout keeps the user's identity, the token never reaches disk, SSH
// remotes resolve to HTTPS, and Clear restores the fallback.
func TestWorktreeGitHubIdentityIsWorktreeLocal(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	_ = os.MkdirAll(home, 0o755)
	env := []string{"HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "GIT_CONFIG_NOSYSTEM=1"}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	main := filepath.Join(root, "main")
	gitT(t, root, env, "init", "-q", main)
	gitT(t, main, env, "config", "user.name", "Human")
	gitT(t, main, env, "config", "user.email", "human@example.com")
	gitT(t, main, env, "remote", "add", "origin", "git@github.com:o/r.git")
	gitT(t, main, env, "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(root, "wt")
	gitT(t, main, env, "worktree", "add", "-q", wt)

	for i := 0; i < 2; i++ { // idempotent
		if err := ConfigureWorktreeGitHubIdentity(wt); err != nil {
			t.Fatal(err)
		}
	}
	if got := gitT(t, wt, env, "config", "user.email"); got != defaultAgentGitEmail {
		t.Fatalf("worktree email %q", got)
	}
	if got := gitT(t, main, env, "config", "user.email"); got != "human@example.com" {
		t.Fatalf("main checkout email changed to %q", got)
	}
	if got := gitT(t, wt, env, "ls-remote", "--get-url", "origin"); got != "https://github.com/o/r.git" {
		t.Fatalf("push url %q", got)
	}
	if got := gitT(t, wt, env, "config", "--worktree", "--get-all", "url.https://github.com/.insteadOf"); strings.Count(got, "\n") != 1 {
		t.Fatalf("insteadOf not deduped: %q", got)
	}
	cred := exec.Command("git", "credential", "fill")
	cred.Dir = wt
	cred.Env = append(os.Environ(), append(env, "GH_TOKEN=ghs_test", "GIT_TERMINAL_PROMPT=0")...)
	cred.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	out, err := cred.Output()
	if err != nil || !strings.Contains(string(out), "password=ghs_test") || !strings.Contains(string(out), "username=x-access-token") {
		t.Fatalf("credential fill: %v %s", err, out)
	}
	cfg, _ := os.ReadFile(filepath.Join(main, ".git", "worktrees", "wt", "config.worktree"))
	if strings.Contains(string(cfg), "ghs_test") {
		t.Fatal("token written to disk")
	}
	ClearWorktreeGitHubIdentity(wt)
	if got := gitT(t, wt, env, "config", "user.email"); got != "human@example.com" {
		t.Fatalf("after clear email %q", got)
	}
	if got := gitT(t, wt, env, "ls-remote", "--get-url", "origin"); got != "git@github.com:o/r.git" {
		t.Fatalf("after clear url %q", got)
	}
}

// With the App identity on, a bot co-author becomes the human's own identity.
func TestCoAuthorTrailerCreditsHumanWhenBotAuthors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	gitT(t, home, nil, "config", "--global", "user.name", "Ray")
	gitT(t, home, nil, "config", "--global", "user.email", "ray@example.com")
	t.Setenv("MULTICA_CO_AUTHOR_NAME", defaultAgentGitName)
	t.Setenv("MULTICA_CO_AUTHOR_EMAIL", defaultAgentGitEmail)
	t.Setenv("MULTICA_AGENT_GITHUB_APP", "")
	if got := localCoAuthorTrailer(); got != "Co-authored-by: Ray <ray@example.com>" {
		t.Fatalf("trailer %q", got)
	}
	t.Setenv("MULTICA_AGENT_GITHUB_APP", "0")
	if got := localCoAuthorTrailer(); !strings.Contains(got, defaultAgentGitEmail) {
		t.Fatalf("disabled trailer %q", got)
	}
}

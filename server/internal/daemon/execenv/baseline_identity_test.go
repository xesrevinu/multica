package execenv

import (
	"path/filepath"
	"strings"
	"testing"
)

func baselineRepo(t *testing.T, origin string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	gitT(t, home, nil, "config", "--global", "user.name", "Ray")
	gitT(t, home, nil, "config", "--global", "user.email", "ray@example.com")
	repo := t.TempDir()
	gitT(t, repo, nil, "init", "-q")
	gitT(t, repo, nil, "commit", "-q", "--allow-empty", "-m", "root")
	if origin != "" {
		gitT(t, repo, nil, "remote", "add", "origin", origin)
	}
	return repo
}

// The baseline commit is authored like the agent's own commits: the App bot,
// with the human credited as Co-authored-by.
func TestBaselineCommitUsesBotIdentityOnGitHub(t *testing.T) {
	t.Setenv("MULTICA_AGENT_GITHUB_APP", "")
	t.Setenv("MULTICA_AGENT_GIT_NAME", "")
	t.Setenv("MULTICA_AGENT_GIT_EMAIL", "")
	repo := baselineRepo(t, "git@github.com:example/repo.git")
	if _, err := commitBaseline(repo, false, false); err != nil {
		t.Fatal(err)
	}
	author := gitT(t, repo, nil, "log", "-1", "--format=%an <%ae>")
	if author != defaultAgentGitName+" <"+defaultAgentGitEmail+">" {
		t.Fatalf("author %q", author)
	}
	body := gitT(t, repo, nil, "log", "-1", "--format=%B")
	if strings.Count(body, "Co-authored-by: Ray <ray@example.com>") != 1 {
		t.Fatalf("trailer missing or duplicated: %q", body)
	}
}

func TestBaselineCommitKeepsUserIdentityOtherwise(t *testing.T) {
	for _, tc := range []struct{ name, origin, app string }{
		{"disabled", "https://github.com/example/repo.git", "0"},
		{"non-github", "https://gitlab.com/example/repo.git", ""},
		{"no-origin", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MULTICA_AGENT_GITHUB_APP", tc.app)
			repo := baselineRepo(t, tc.origin)
			if _, err := commitBaseline(repo, false, false); err != nil {
				t.Fatal(err)
			}
			if a := gitT(t, repo, nil, "log", "-1", "--format=%an"); a != "Ray" {
				t.Fatalf("author %q", a)
			}
			if b := gitT(t, repo, nil, "log", "-1", "--format=%B"); strings.Contains(b, "Co-authored-by") {
				t.Fatalf("unexpected trailer %q", b)
			}
		})
	}
}

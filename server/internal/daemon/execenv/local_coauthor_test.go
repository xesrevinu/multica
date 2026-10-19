package execenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalWorktreeCoAuthorTrailerSkipsTheUsersOwnCommits(t *testing.T) {
	requireGit(t)
	t.Setenv("MULTICA_CO_AUTHOR_NAME", "woolpup[bot]")
	t.Setenv("MULTICA_CO_AUTHOR_EMAIL", "187665180+woolpup[bot]@users.noreply.github.com")
	t.Setenv("MULTICA_TASK_ID", "task-coauthor")

	repo := newTestRepo(t)
	state := filepath.Join(t.TempDir(), "coauthor")
	if err := os.WriteFile(state, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := prepareWithCoAuthor(t, repo, state)
	defer removeLocalWorktreeDir(repo, wt.Path, worktreeTestLogger())

	baseline := gitRun(t, wt.Path, "log", "-1", "--format=%B")
	const trailer = "Co-authored-by: woolpup[bot] <187665180+woolpup[bot]@users.noreply.github.com>"
	if !strings.Contains(baseline, trailer) {
		t.Fatalf("baseline commit missing co-author trailer:\n%s", baseline)
	}

	// A commit from the user's environment — no daemon variables — shares the
	// hooks directory and must not pick up the bot.
	if err := os.WriteFile(filepath.Join(repo, "user.txt"), []byte("user\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "user.txt")
	commit := exec.Command("git", "-C", repo, "commit", "-m", "user commit")
	commit.Env = envWithoutMulticaIdentity(os.Environ())
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("user commit: %s: %v", out, err)
	}
	userMsg := gitRun(t, repo, "log", "-1", "--format=%B")
	if strings.Contains(userMsg, "Co-authored-by:") {
		t.Fatalf("user commit picked up the bot trailer:\n%s", userMsg)
	}

	if err := os.WriteFile(state, []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "off.txt"), []byte("off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wt.Path, "add", "off.txt")
	gitRun(t, wt.Path, "commit", "-m", "toggle off")
	off := gitRun(t, wt.Path, "log", "-1", "--format=%B")
	if strings.Contains(off, "Co-authored-by:") {
		t.Fatalf("toggle-off commit still has the trailer:\n%s", off)
	}
}

func TestLocalWorktreeCoAuthorHookLeavesAUserHookAlone(t *testing.T) {
	requireGit(t)
	repo := newTestRepo(t)
	hooks := filepath.Join(repo, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	const userHook = "#!/bin/sh\n# user hook\nexit 0\n"
	if err := os.WriteFile(filepath.Join(hooks, "prepare-commit-msg"), []byte(userHook), 0o755); err != nil {
		t.Fatal(err)
	}
	wt := prepareWithCoAuthor(t, repo, "")
	defer removeLocalWorktreeDir(repo, wt.Path, worktreeTestLogger())
	got, err := os.ReadFile(filepath.Join(hooks, "prepare-commit-msg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != userHook {
		t.Fatalf("user hook was replaced:\n%s", got)
	}
}

func prepareWithCoAuthor(t *testing.T, repo, statePath string) *LocalWorktree {
	t.Helper()
	wt, err := PrepareLocalWorktree(LocalWorktreeParams{
		LocalPath:         repo,
		EnvRoot:           t.TempDir(),
		AgentName:         "J",
		TaskID:            "11112222-3333-4444-5555-666677778888",
		CoAuthorStatePath: statePath,
	}, worktreeTestLogger())
	if err != nil {
		t.Fatalf("PrepareLocalWorktree: %v", err)
	}
	return wt
}

func envWithoutMulticaIdentity(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, entry := range environ {
		if strings.HasPrefix(entry, "MULTICA_CO_AUTHOR_NAME=") ||
			strings.HasPrefix(entry, "MULTICA_CO_AUTHOR_EMAIL=") ||
			strings.HasPrefix(entry, "MULTICA_TASK_ID=") {
			continue
		}
		out = append(out, entry)
	}
	return out
}

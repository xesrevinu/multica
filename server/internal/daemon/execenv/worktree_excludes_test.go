package execenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func excludesGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func excludesFixture(t *testing.T) (repo, worktree, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv(ArtifactRootEnv, filepath.Join(home, "artifacts"))
	if err := os.MkdirAll(filepath.Join(home, ".config", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "git", "ignore"), []byte("*.globalignored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo = filepath.Join(t.TempDir(), "holt")
	if err := os.MkdirAll(filepath.Join(repo, "skills", "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	excludesGit(t, repo, "init", "-q", "-b", "main")
	excludesGit(t, repo, "config", "user.email", "t@example.com")
	excludesGit(t, repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "skills", "real", "SKILL.md"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	excludesGit(t, repo, "add", ".")
	excludesGit(t, repo, "commit", "-q", "-m", "init")
	worktree = filepath.Join(t.TempDir(), "task-wt")
	excludesGit(t, repo, "worktree", "add", "-q", "-b", "agent/x", worktree)
	return repo, worktree, home
}

func writeProbe(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeExcludesHideSidecarsOnlyInThatWorktree(t *testing.T) {
	repo, worktree, home := excludesFixture(t)
	for _, dir := range []string{".pi/skills/probe", ".agent_context/skills/probe"} {
		writeProbe(t, filepath.Join(worktree, dir))
		writeProbe(t, filepath.Join(repo, dir))
	}
	if err := os.WriteFile(filepath.Join(worktree, "x.globalignored"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := applyWorktreeExcludes(repo, worktree, worktree, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(file, filepath.Join(home, "artifacts", "holt", "tmp", "worktree-excludes")) {
		t.Fatalf("excludes file %s is not under the artifact root", file)
	}
	if got := excludesGit(t, worktree, "status", "--porcelain", "--untracked-files=all"); got != "" {
		t.Fatalf("worktree status not clean:\n%s", got)
	}
	main := excludesGit(t, repo, "status", "--porcelain")
	if !strings.Contains(main, ".pi/") || !strings.Contains(main, ".agent_context/") {
		t.Fatalf("the user's checkout must be unaffected, got:\n%s", main)
	}
	// The main worktree reads common + its own config; it must not see ours.
	if out, err := exec.Command("git", "-C", repo, "config", "--get", "core.excludesFile").Output(); err == nil {
		t.Fatalf("main worktree sees core.excludesFile=%s", out)
	}
	if _, err := os.Stat(filepath.Join(worktree, ".pi", "skills", "probe", "SKILL.md")); err != nil {
		t.Fatal("excluded skill must stay on disk for discovery")
	}
}

func TestWorktreeExcludesKeepTrackedSkillDirAndRepoConfig(t *testing.T) {
	repo, worktree, _ := excludesFixture(t)
	// openclaw's skill dir is <workdir>/skills, which this repo tracks.
	patterns := sidecarExcludePatterns(worktree, worktree, "openclaw")
	for _, pattern := range patterns {
		if pattern == "/skills/" {
			t.Fatalf("tracked skills/ must not be excluded: %v", patterns)
		}
		if strings.Contains(pattern, ".multica") {
			t.Fatalf(".multica is repo config and must not be excluded: %v", patterns)
		}
	}
	if _, err := applyWorktreeExcludes(repo, worktree, worktree, "openclaw"); err != nil {
		t.Fatal(err)
	}
	writeProbe(t, filepath.Join(worktree, "skills", "new-real"))
	if got := excludesGit(t, worktree, "status", "--porcelain"); !strings.Contains(got, "skills/new-real/") {
		t.Fatalf("new repo content under a tracked skills/ must stay visible, got %q", got)
	}
}

func TestWorktreeExcludesFollowWorkdirOffset(t *testing.T) {
	_, worktree, _ := excludesFixture(t)
	patterns := sidecarExcludePatterns(worktree, filepath.Join(worktree, "services", "api"), "claude")
	if len(patterns) == 0 || patterns[0] != "/services/api/.claude/skills/" {
		t.Fatalf("patterns = %v", patterns)
	}
}

func TestRemoveWorktreeExcludes(t *testing.T) {
	repo, worktree, _ := excludesFixture(t)
	file, err := applyWorktreeExcludes(repo, worktree, worktree, "pi")
	if err != nil {
		t.Fatal(err)
	}
	removeWorktreeExcludes(repo, worktree)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("excludes file still present: %v", err)
	}
}

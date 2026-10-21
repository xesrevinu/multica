package execenv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Per-worktree git excludes for the files Prepare writes into a task
// worktree (provider skill dirs, .agent_context). They would otherwise show
// up as untracked in the agent's `git status`, and an agent that runs
// `git add -A` would commit them.
//
// A linked worktree reads info/exclude from the repo's COMMON git dir, which
// is the user's own checkout, so that file is off limits. Instead each task
// worktree gets its own excludes file outside every repo, under the artifact
// root (~/.cursor/artifacts/<repo>/tmp/worktree-excludes/), and points
// `core.excludesFile` at it in that worktree's own config
// (extensions.worktreeConfig). The worktree config value replaces the user's
// global core.excludesFile, so the user's global patterns are copied in first.
//
// Exclusion only hides untracked files: a skill dir that already holds
// tracked files (it is the repo's own content) is left alone. Discovery is
// unaffected: runtimes list their skill dirs directly. The pre-Finalize
// CleanupRuntimeConfig / CleanupSidecars pass stays as the backstop.

// ArtifactRootEnv overrides the base of the per-project artifact root.
const ArtifactRootEnv = "MULTICA_ARTIFACT_ROOT"

// artifactBase is ~/.cursor/artifacts, the sibling of the worktrunk worktree
// root ~/.cursor/worktrees.
func artifactBase() string {
	if base := strings.TrimSpace(os.Getenv(ArtifactRootEnv)); base != "" {
		return base
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "multica-artifacts")
	}
	return filepath.Join(home, ".cursor", "artifacts")
}

// worktreeExcludesPath is deterministic in (gitRoot, worktreePath) so removal
// can find it without extra state. <repo> is the checkout's directory name,
// as worktrunk's {{ repo }}.
func worktreeExcludesPath(gitRoot, worktreePath string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(worktreePath)))
	name := filepath.Base(worktreePath) + "-" + hex.EncodeToString(sum[:4]) + ".exclude"
	return filepath.Join(artifactBase(), filepath.Base(filepath.Clean(gitRoot)), "tmp", "worktree-excludes", name)
}

// sidecarExcludePatterns lists the anchored patterns for one task: the
// provider's skill dir and .agent_context, both under the agent's workdir
// offset inside the worktree. A skill dir with tracked files is skipped.
func sidecarExcludePatterns(worktreePath, workDir, provider string) []string {
	rel, err := filepath.Rel(worktreePath, workDir)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = "."
	}
	prefix := "/"
	if rel != "." {
		prefix = "/" + filepath.ToSlash(rel) + "/"
	}
	var patterns []string
	add := func(dir string) {
		pattern := prefix + filepath.ToSlash(dir) + "/"
		for _, existing := range patterns {
			if existing == pattern {
				return
			}
		}
		patterns = append(patterns, pattern)
	}
	if provider != "" {
		skills := skillsDirPath(workDir, provider)
		if skillRel, err := filepath.Rel(workDir, skills); err == nil && !strings.HasPrefix(skillRel, "..") {
			if out, err := runGit(worktreePath, "ls-files", "--", filepath.ToSlash(filepath.Join(rel, skillRel))); err == nil && strings.TrimSpace(out) == "" {
				add(skillRel)
			}
		}
	}
	for _, name := range multicaSidecarDirNames {
		if name == ".multica" {
			// .multica is also a repo's own Multica config dir (project.yaml,
			// skills); hiding new files there would drop real edits.
			continue
		}
		add(name)
	}
	return patterns
}

// userGlobalExcludes returns the user's global excludes text: the file named
// by `git config --global core.excludesFile`, else $XDG_CONFIG_HOME/git/ignore
// (default ~/.config/git/ignore). Missing files yield "".
func userGlobalExcludes(dir string) (string, string) {
	path := ""
	if out, err := runGit(dir, "config", "--global", "--path", "--get", "core.excludesFile"); err == nil {
		path = strings.TrimSpace(out)
	}
	if path == "" {
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			if home, err := os.UserHomeDir(); err == nil {
				base = filepath.Join(home, ".config")
			}
		}
		if base != "" {
			path = filepath.Join(base, "git", "ignore")
		}
	}
	if path == "" {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return path, ""
	}
	return path, string(data)
}

// applyWorktreeExcludes writes the per-worktree excludes file and points the
// worktree's own core.excludesFile at it. It returns the file it wrote.
func applyWorktreeExcludes(gitRoot, worktreePath, workDir, provider string) (string, error) {
	if out, _ := runGit(worktreePath, "config", "--get", "core.bare"); strings.TrimSpace(out) == "true" {
		return "", fmt.Errorf("repository is bare; per-worktree config not enabled")
	}
	if out, _ := runGit(worktreePath, "config", "--get", "core.worktree"); strings.TrimSpace(out) != "" {
		return "", fmt.Errorf("repository sets core.worktree; per-worktree config not enabled")
	}
	patterns := sidecarExcludePatterns(worktreePath, workDir, provider)
	globalPath, globalText := userGlobalExcludes(worktreePath)
	var b strings.Builder
	b.WriteString("# Multica task worktree excludes (generated; removed with the worktree).\n")
	b.WriteString("# worktree: " + worktreePath + "\n")
	if globalText != "" {
		b.WriteString("\n# from " + globalPath + "\n")
		b.WriteString(globalText)
		if !strings.HasSuffix(globalText, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n# daemon sidecars\n")
	for _, pattern := range patterns {
		b.WriteString(pattern + "\n")
	}
	file := worktreeExcludesPath(gitRoot, worktreePath)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", fmt.Errorf("create excludes dir: %w", err)
	}
	if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("write excludes file: %w", err)
	}
	if out, err := runGit(worktreePath, "config", "extensions.worktreeConfig", "true"); err != nil {
		return file, fmt.Errorf("enable extensions.worktreeConfig: %s: %w", strings.TrimSpace(out), err)
	}
	if out, err := runGit(worktreePath, "config", "--worktree", "core.excludesFile", file); err != nil {
		return file, fmt.Errorf("set worktree core.excludesFile: %s: %w", strings.TrimSpace(out), err)
	}
	return file, nil
}

// excludeWorktreeSidecars is the Prepare hook. Failure is logged, not fatal:
// the pre-Finalize cleanup still keeps sidecars out of the delivery.
func excludeWorktreeSidecars(wt *LocalWorktree, workDir, provider string, logger *slog.Logger) {
	if wt == nil || wt.Path == "" || wt.GitRoot == "" {
		return
	}
	if _, err := applyWorktreeExcludes(wt.GitRoot, wt.Path, workDir, provider); err != nil && logger != nil {
		logger.Warn("execenv: per-worktree excludes not applied (sidecars may show as untracked; cleanup still runs before Finalize)",
			"worktree", wt.Path, "error", err)
	}
}

// removeWorktreeExcludes deletes the excludes file of a removed worktree.
func removeWorktreeExcludes(gitRoot, worktreePath string) {
	_ = os.Remove(worktreeExcludesPath(gitRoot, worktreePath))
}

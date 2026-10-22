package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const localCoAuthorHookMarker = "# multica:prepare-commit-msg:co-authored-by"

// installLocalCoAuthorHook installs the Co-authored-by prepare-commit-msg hook
// into the repository that owns this worktree.
//
// Linked worktrees run hooks from the git common directory, not from their own
// git dir, so the hook has to live next to the user's checkout. That same
// directory also runs for the user's own commits. The script therefore no-ops
// unless the process has the daemon environment (MULTICA_CO_AUTHOR_NAME, which
// the daemon and the agent it spawns both have, or MULTICA_TASK_ID). A commit
// the user makes in their terminal is left unchanged.
//
// An existing prepare-commit-msg that this daemon did not write is left in
// place. Replacing it would drop a hook the user or another tool installed.
//
// statePath is the workspace toggle file the bare-cache hook already re-reads
// at commit time. Empty installs an ungated hook, which matches a checkout
// made before any toggle was published: the hook only exists because co-author
// is on.
func installLocalCoAuthorHook(worktreePath, statePath string, logger *slog.Logger) error {
	common, err := runGitTrimmed(worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("execenv: resolve git common dir for co-author hook: %w", err)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(worktreePath, common)
	}
	hooksDir := filepath.Join(common, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("execenv: create hooks dir: %w", err)
	}
	hookPath := filepath.Join(hooksDir, "prepare-commit-msg")
	current, readErr := os.ReadFile(hookPath)
	if readErr == nil && !strings.Contains(string(current), localCoAuthorHookMarker) {
		if logger != nil {
			logger.Warn("execenv: leaving the existing prepare-commit-msg hook in place; Co-authored-by trailer was not installed",
				"path", hookPath)
		}
		return nil
	}
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("execenv: read prepare-commit-msg hook: %w", readErr)
	}
	if err := writeLocalCoAuthorHook(hookPath, localCoAuthorHook(statePath)); err != nil {
		return fmt.Errorf("execenv: install co-author hook: %w", err)
	}
	if logger != nil {
		logger.Info("execenv: installed co-author hook for the local worktree", "path", hookPath)
	}
	return nil
}

func localCoAuthorHook(statePath string) string {
	var gate string
	if statePath != "" {
		gate = fmt.Sprintf(`# Current workspace setting, refreshed by the daemon. "0" means the
# Co-authored-by toggle is off — leave the message alone.
STATE_FILE=%s
if [ -f "$STATE_FILE" ]; then
  case "$(cat "$STATE_FILE" 2>/dev/null)" in
    0) exit 0 ;;
  esac
fi

`, shellSingleQuoted(filepath.ToSlash(statePath)))
	}
	return `#!/bin/sh
` + localCoAuthorHookMarker + `
# Multica: add Co-authored-by trailer for the Multica Agent.
# Installed by the Multica daemon. Do not edit — it will be overwritten.

COMMIT_MSG_FILE="$1"
COMMIT_SOURCE="$2"

# Skip merge and squash commits.
case "$COMMIT_SOURCE" in
  merge|squash) exit 0 ;;
esac

# This hook lives in the user's repository. Linked worktrees share that hooks
# directory, so the user's own commits run it too. Those commits do not carry
# the daemon environment; leave them unchanged. The daemon process has
# MULTICA_CO_AUTHOR_NAME; the agent it spawns also has MULTICA_TASK_ID.
if [ -z "${MULTICA_TASK_ID:-}" ] && [ -z "${MULTICA_CO_AUTHOR_NAME:-}" ]; then
  exit 0
fi

` + gate + `TRAILER="` + localCoAuthorTrailer() + `"

# Don't add if already present.
if grep -qF "$TRAILER" "$COMMIT_MSG_FILE"; then
  exit 0
fi

git interpret-trailers --in-place --trailer "$TRAILER" "$COMMIT_MSG_FILE"
`
}

func localCoAuthorTrailer() string {
	name, email := AgentCoAuthor()
	if name == "" {
		name = "multica-agent"
	}
	if email == "" {
		email = "github@multica.ai"
	}
	if strings.ContainsAny(name, "<>\"\n\r$`\\") || strings.ContainsAny(email, "<>\"\n\r$`\\ \t") || !strings.Contains(email, "@") {
		name = "multica-agent"
		email = "github@multica.ai"
	}
	return "Co-authored-by: " + name + " <" + email + ">"
}

func shellSingleQuoted(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func writeLocalCoAuthorHook(hookPath, contents string) error {
	dir := filepath.Dir(hookPath)
	tmp, err := os.CreateTemp(dir, ".multica-hook-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(contents); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, hookPath); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

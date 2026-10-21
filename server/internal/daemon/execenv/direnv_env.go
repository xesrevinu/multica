package execenv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// direnvTimeout bounds `direnv export json`. A cached nix-direnv flake answers
// in well under a second; an uncached `use flake` can build, and a task must
// not wait on that, so it falls back to the daemon env instead.
var direnvTimeout = 45 * time.Second

// direnvProtectedPrefixes are keys the daemon owns; .envrc cannot override them.
var direnvProtectedKeys = map[string]struct{}{"HOME": {}, "USER": {}, "LOGNAME": {}, "SHELL": {}, "TMPDIR": {}}

const direnvProtectedPrefix = "MULTICA_"

// LoadDirenvEnv layers the environment a worktree's .envrc exports (the repo's
// toolchain: cargo, rustc, xcode wrappers, ...) onto agentEnv, the way an
// interactive shell with the direnv hook would see it. A launchd/systemd daemon
// never runs that hook, so without this the agent's shell lacks the toolchain
// and agents rebuild it by sourcing .direnv/*.rc by hand.
//
// Only an .envrc the user already allowed loads: `direnv export` refuses a
// blocked one and the task keeps the daemon env. Any failure (no direnv, no
// .envrc, timeout, bad JSON) is a logged fallback, never a task failure.
// PATH from direnv replaces agentEnv's PATH but keeps keepFirst (the multica
// binary dir) in front. MULTICA_* and HOME-like keys are never overridden.
func LoadDirenvEnv(workDir string, agentEnv map[string]string, keepFirst string, logger *slog.Logger) bool {
	if workDir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(workDir, ".envrc")); err != nil {
		return false
	}
	base := worktrunkEnv(os.Environ())
	bin := lookPathIn("direnv", envValue(base, "PATH"))
	if bin == "" {
		bin = lookPathIn("direnv", "/etc/profiles/per-user/"+os.Getenv("USER")+"/bin:/run/current-system/sw/bin:/opt/homebrew/bin:/usr/local/bin")
	}
	if bin == "" {
		return false
	}
	vars, err := direnvExport(bin, workDir, base)
	if err != nil {
		if logger != nil {
			logger.Warn("execenv: direnv env not loaded; agent keeps the daemon env", "workdir", workDir, "error", err)
		}
		return false
	}
	applyDirenvVars(agentEnv, vars, keepFirst)
	if logger != nil {
		logger.Info("execenv: loaded worktree direnv env", "workdir", workDir, "vars", len(vars))
	}
	return true
}

func direnvExport(bin, workDir string, env []string) (map[string]*string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), direnvTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "export", "json")
	cmd.Dir = workDir
	// Start from a shell that has not loaded anything, so the export is the
	// full .envrc diff rather than relative to whatever the daemon inherited.
	cmd.Env = stripDirenvState(env)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("direnv export timed out")
		}
		return nil, errors.New("direnv export failed: " + firstLine(stderr.String()))
	}
	out := bytes.TrimSpace(stdout.Bytes())
	if len(out) == 0 {
		// Blocked (not allowed) .envrc, or nothing to export.
		return nil, errors.New("direnv exported nothing: " + firstLine(stderr.String()))
	}
	vars := map[string]*string{}
	if err := json.Unmarshal(out, &vars); err != nil {
		return nil, errors.New("direnv export: bad json")
	}
	return vars, nil
}

func applyDirenvVars(agentEnv map[string]string, vars map[string]*string, keepFirst string) {
	for k, v := range vars {
		if v == nil || strings.HasPrefix(k, direnvProtectedPrefix) {
			continue
		}
		if _, ok := direnvProtectedKeys[k]; ok {
			continue
		}
		if k == "PATH" && keepFirst != "" {
			agentEnv[k] = prependPathDir(*v, keepFirst)
			continue
		}
		agentEnv[k] = *v
	}
}

func prependPathDir(path, dir string) string {
	parts := []string{dir}
	for _, p := range filepath.SplitList(path) {
		if p != dir && p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func stripDirenvState(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "DIRENV_DIR=") || strings.HasPrefix(kv, "DIRENV_DIFF=") || strings.HasPrefix(kv, "DIRENV_WATCHES=") || strings.HasPrefix(kv, "DIRENV_FILE=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func envValue(env []string, key string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return strings.TrimPrefix(kv, key+"=")
		}
	}
	return ""
}

func lookPathIn(name, path string) string {
	for _, d := range filepath.SplitList(path) {
		if d == "" {
			continue
		}
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

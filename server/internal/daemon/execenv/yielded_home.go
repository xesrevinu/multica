package execenv

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// yieldedSessionRoot is where the yielded-agent binary keeps one SQLite file per
// conversation id (`~/.local/state/yielded-agent/sessions/<id>.sqlite`). The
// daemon resumes a session by passing `--session <id>` whenever the file exists,
// so an abandoned issue keeps its whole transcript and every tool result on
// disk — and in every later prompt — until something removes it.
func yieldedSessionRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "state", "yielded-agent", "sessions")
}

// PruneYieldedSessions reclaims yielded-agent session files untouched for at
// least retention. A session is skipped while a `-shm` sidecar exists: SQLite
// removes that file when the last connection closes, so its presence means a
// turn still has the database open. Retention 0 disables the sweep.
func PruneYieldedSessions(retention time.Duration, now time.Time, logger *slog.Logger) (removed int, bytesFreed int64) {
	if retention <= 0 {
		return 0, 0
	}
	root := yieldedSessionRoot()
	if root == "" {
		return 0, 0
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, 0 // not created yet, or unreadable — nothing to prune
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sqlite") {
			continue
		}
		base := filepath.Join(root, entry.Name())
		if _, err := os.Stat(base + "-shm"); err == nil {
			continue // a turn still holds the database open
		}
		newest, size := yieldedSessionStat(base)
		if newest.IsZero() || now.Sub(newest) <= retention {
			continue
		}
		var freed int64
		failed := false
		for _, suffix := range []string{"", "-wal", "-shm"} {
			path := base + suffix
			info, statErr := os.Stat(path)
			if statErr != nil {
				continue
			}
			if rmErr := os.Remove(path); rmErr != nil {
				logger.Warn("execenv: prune yielded session failed", "session", path, "error", rmErr)
				failed = true
				continue
			}
			freed += info.Size()
		}
		if failed {
			continue
		}
		removed++
		bytesFreed += freed + max(size-freed, 0)
	}
	return removed, bytesFreed
}

// yieldedSessionStat reports the newest mtime across a session database and its
// WAL sidecar, plus the bytes they occupy.
func yieldedSessionStat(base string) (newest time.Time, size int64) {
	for _, suffix := range []string{"", "-wal"} {
		info, err := os.Stat(base + suffix)
		if err != nil {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		size += info.Size()
	}
	return newest, size
}

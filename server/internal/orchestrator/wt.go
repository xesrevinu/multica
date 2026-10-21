package orchestrator

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Worktrees are created by wt (worktrunk); its `worktree-path` template is
// the single source of truth for where they live. repo.disk_path stays as an
// explicit override for the free-disk check.

var wtPathRe = regexp.MustCompile(`(?m)^\s*worktree-path\s*=\s*["']([^"']+)["']`)

// wtTemplate returns the worktree-path template: the repo's .config/wt.toml
// first, then the user config ($WORKTRUNK_CONFIG_PATH, $XDG_CONFIG_HOME or
// ~/.config/worktrunk/config.toml).
func wtTemplate(hub string) string {
	var files []string
	if hub != "" {
		files = append(files, filepath.Join(hub, ".config", "wt.toml"))
	}
	if p := os.Getenv("WORKTRUNK_CONFIG_PATH"); p != "" {
		files = append(files, p)
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		files = append(files, filepath.Join(x, "worktrunk", "config.toml"))
	}
	if h, err := os.UserHomeDir(); err == nil {
		files = append(files, filepath.Join(h, ".config", "worktrunk", "config.toml"))
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if m := wtPathRe.FindSubmatch(b); m != nil {
			return string(m[1])
		}
	}
	return ""
}

// wtRoot is the static directory prefix of a worktree-path template
// ("~/.cursor/worktrees/{{ repo }}/{{ branch }}" → "$HOME/.cursor/worktrees").
// Relative templates resolve against the hub (wt resolves them against the
// main worktree).
func wtRoot(tmpl, hub string) string {
	if tmpl == "" {
		return hub
	}
	if i := strings.Index(tmpl, "{"); i >= 0 {
		tmpl = tmpl[:i]
	}
	if strings.HasPrefix(tmpl, "~") {
		if h, err := os.UserHomeDir(); err == nil {
			tmpl = h + tmpl[1:]
		}
	}
	if !filepath.IsAbs(tmpl) {
		if hub == "" {
			return ""
		}
		tmpl = filepath.Join(hub, tmpl)
	}
	d := filepath.Clean(tmpl)
	// The prefix may end mid-name ("/x/wt-{{ branch }}"); use its directory.
	for d != "/" && d != "." {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
		d = filepath.Dir(d)
	}
	return d
}

var wtCache sync.Map // hub → wtCached

type wtCached struct {
	root string
	at   time.Time
}

// diskPath is where the free-disk check looks: repo.disk_path when set,
// otherwise the wt worktree root (cached for a minute).
func diskPath(sp *Spec) string {
	if sp.Repo.DiskPath != "" {
		return sp.Repo.DiskPath
	}
	if c, ok := wtCache.Load(sp.Repo.Hub); ok && time.Since(c.(wtCached).at) < time.Minute {
		return c.(wtCached).root
	}
	root := wtRoot(wtTemplate(sp.Repo.Hub), sp.Repo.Hub)
	wtCache.Store(sp.Repo.Hub, wtCached{root: root, at: time.Now()})
	return root
}

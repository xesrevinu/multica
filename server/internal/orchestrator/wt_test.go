package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWtRootAndDiskPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("WORKTRUNK_CONFIG_PATH", "")
	hub := filepath.Join(home, "hub")
	wts := filepath.Join(home, ".cursor", "worktrees")
	for _, d := range []string{hub, wts, filepath.Join(home, ".config", "worktrunk")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := wtRoot("", hub); got != hub {
		t.Fatalf("no template → hub, got %s", got)
	}
	if got := wtRoot("~/.cursor/worktrees/{{ repo }}/{{ branch | sanitize }}", hub); got != wts {
		t.Fatalf("got %s want %s", got, wts)
	}
	if got := wtRoot("../{{ repo }}.{{ branch }}", hub); got != home {
		t.Fatalf("relative: got %s want %s", got, home)
	}

	// User config is read when the repo has none; repo config wins.
	_ = os.WriteFile(filepath.Join(home, ".config", "worktrunk", "config.toml"), []byte("# c\nworktree-path = \"~/.cursor/worktrees/{{ repo }}/{{ branch | sanitize }}\"\n"), 0o644)
	sp := &Spec{Repo: RepoSpec{Hub: hub}}
	if got := diskPath(sp); got != wts {
		t.Fatalf("user config: got %s", got)
	}
	sp2 := &Spec{Repo: RepoSpec{Hub: hub, DiskPath: "/override"}}
	if got := diskPath(sp2); got != "/override" {
		t.Fatalf("override: got %s", got)
	}
	_ = os.MkdirAll(filepath.Join(hub, ".config"), 0o755)
	_ = os.WriteFile(filepath.Join(hub, ".config", "wt.toml"), []byte("worktree-path = '.worktrees/{{ branch }}'\n"), 0o644)
	if got := wtRoot(wtTemplate(hub), hub); got != hub {
		// .worktrees does not exist yet → nearest existing parent is the hub.
		t.Fatalf("repo config: got %s", got)
	}
}

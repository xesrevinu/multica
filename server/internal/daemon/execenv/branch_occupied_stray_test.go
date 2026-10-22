package execenv

import "testing"

func TestStrayDirectoryFromWtOutput(t *testing.T) {
	detail := "▲ Branch agent/ox-112 exists on remote\n✗ Directory already exists: ~/.cursor/worktrees/holt/agent-ox-112\n↳ To remove manually"
	if got := strayDirectoryFrom(detail); got != "~/.cursor/worktrees/holt/agent-ox-112" {
		t.Fatalf("got %q", got)
	}
	if got := strayDirectoryFrom("fatal: 'agent/x' is already checked out"); got != "" {
		t.Fatalf("got %q", got)
	}
}

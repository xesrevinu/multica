//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/creack/pty"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func startPTY(msg protocol.PTYControl) (*ptySession, error) {
	argv := msg.Argv
	if len(argv) == 0 {
		argv = defaultPTYArgv()
	}
	cwd, err := resolvePTYCwd(msg.Cwd)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = ptyEnv(os.Environ())
	// creack/pty already sets Setsid+Setctty. Setpgid together with Setsid is
	// EPERM on Darwin (`fork/exec /bin/zsh: operation not permitted`). After
	// Setsid the child is its own process group, so kill(-pid) still works.

	cols, rows := msg.Cols, msg.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, fmt.Errorf("open pty: %w", err)
	}

	return &ptySession{
		file: file,
		resize: func(c, r uint16) error {
			if c == 0 || r == 0 {
				return nil
			}
			return pty.Setsize(file, &pty.Winsize{Cols: c, Rows: r})
		},
		kill: func() {
			if cmd.Process == nil {
				return
			}
			pgid := cmd.Process.Pid
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
			_ = cmd.Process.Kill()
		},
	}, nil
}

func ptyEnv(parent []string) []string {
	out := make([]string, 0, len(parent)+2)
	for _, kv := range parent {
		if strings.HasPrefix(kv, "TERM=") || strings.HasPrefix(kv, "COLORTERM=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "TERM=xterm-256color", "COLORTERM=truecolor")
}

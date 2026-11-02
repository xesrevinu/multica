//go:build !windows

package execenv

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/multica-ai/multica/server/internal/daemon/processtree"
)

type preparationProcessController struct {
	pid int
}

func newPreparationProcessController(cmd *exec.Cmd) (*preparationProcessController, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return &preparationProcessController{}, nil
}

func (c *preparationProcessController) attach(cmd *exec.Cmd) error {
	if cmd.Process != nil {
		c.pid = cmd.Process.Pid
	}
	return nil
}

func (c *preparationProcessController) stop(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	c.pid = cmd.Process.Pid
	// Snapshot+kill while the helper is still the parent of its hooks.
	// After SIGKILL those hooks are reparented to init; a later KillTree
	// of a dead helper pid cannot find them.
	return processtree.KillTree(c.pid)
}

func (c *preparationProcessController) finish() error {
	if c.pid <= 1 {
		return nil
	}
	return processtree.KillTree(c.pid)
}

func (*preparationProcessController) close() {}

//go:build !windows

package processtree

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

const processTreeFinishTimeout = 5 * time.Second

type controller struct {
	tree *unixTree
}

func newController(cmd *exec.Cmd) (*controller, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return &controller{}, nil
}

func (*controller) attach(_ *exec.Cmd) error { return nil }

func (c *controller) capture(cmd *exec.Cmd) *unixTree {
	if cmd.Process == nil {
		return c.tree
	}
	if c.tree == nil {
		c.tree = snapshotTree(cmd.Process.Pid)
	} else {
		c.tree.refresh()
	}
	return c.tree
}

func (c *controller) interrupt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	// Snapshot while the leader is alive, then SIGTERM the whole tree.
	// A group-only SIGTERM lets setsid hooks (wt pre-start) survive, and
	// once the leader dies those orphans are reparented to init.
	c.capture(cmd).signal(syscall.SIGTERM)
	return nil
}

func (c *controller) stop(cmd *exec.Cmd) error {
	if cmd.Process == nil && c.tree == nil {
		return os.ErrProcessDone
	}
	return c.capture(cmd).kill()
}

func (c *controller) finish(cmd *exec.Cmd) error {
	if c.tree == nil && cmd.Process == nil {
		return nil
	}
	return c.capture(cmd).kill()
}

func (*controller) close() {}

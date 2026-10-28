//go:build !windows

package processtree

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type procIDs struct {
	pid    int
	ppid   int
	pgid   int
	zombie bool
}

// unixTree is a snapshot of a process and every descendant, including those
// that started a new session or process group. After the leader is killed
// those descendants are reparented to init, so the snapshot must be taken
// while the leader is still alive and reused for the final SIGKILL.
type unixTree struct {
	seen  map[int]struct{}
	pgids map[int]struct{}
}

func snapshotTree(root int) *unixTree {
	t := &unixTree{
		seen:  map[int]struct{}{root: {}},
		pgids: map[int]struct{}{},
	}
	if pgid, err := syscall.Getpgid(root); err == nil && pgid > 1 {
		t.pgids[pgid] = struct{}{}
	}
	if procs, err := listProcesses(); err == nil {
		growProcessTree(t.seen, t.pgids, procs)
	}
	return t
}

func (t *unixTree) refresh() {
	if t == nil {
		return
	}
	if procs, err := listProcesses(); err == nil {
		growProcessTree(t.seen, t.pgids, procs)
	}
}

func (t *unixTree) signal(sig syscall.Signal) {
	if t == nil {
		return
	}
	t.refresh()
	self := os.Getpid()
	selfPgid, err := syscall.Getpgid(self)
	if err != nil {
		selfPgid = self
	}
	for pid := range t.seen {
		if pid <= 1 || pid == self {
			continue
		}
		_ = syscall.Kill(pid, sig)
	}
	for pgid := range t.pgids {
		if pgid <= 1 || pgid == self || pgid == selfPgid {
			continue
		}
		_ = syscall.Kill(-pgid, sig)
	}
}

func (t *unixTree) kill() error {
	if t == nil {
		return nil
	}
	self := os.Getpid()
	selfPgid, err := syscall.Getpgid(self)
	if err != nil {
		selfPgid = self
	}
	deadline := time.Now().Add(processTreeFinishTimeout)
	for {
		t.refresh()
		for pid := range t.seen {
			killPID(pid, self, selfPgid)
		}
		for pgid := range t.pgids {
			killGroup(pgid, self, selfPgid)
		}
		procs, listErr := listProcesses()
		if listErr != nil {
			return listErr
		}
		grew := growProcessTree(t.seen, t.pgids, procs)
		if !grew && !treeAlive(t.seen, t.pgids, procs) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("process tree still active after %s", processTreeFinishTimeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// KillTree terminates pid and every descendant, including processes that
// started a new session or process group (worktrunk pre-start hooks do this).
//
// A process-group kill of the leader is not enough: after SIGKILL the orphans
// are reparented to init (PPID 1) and keep running. Snapshot the tree first,
// then kill every recorded pid and pgid, and repeat until the set is gone.
func KillTree(root int) error {
	if root <= 1 {
		return nil
	}
	return snapshotTree(root).kill()
}

func growProcessTree(seen, pgids map[int]struct{}, procs []procIDs) bool {
	added := false
	for {
		grew := false
		for _, p := range procs {
			if p.pid <= 1 || p.zombie {
				continue
			}
			if _, ok := seen[p.pid]; ok {
				if p.pgid > 1 {
					if _, have := pgids[p.pgid]; !have {
						pgids[p.pgid] = struct{}{}
						grew = true
					}
				}
				continue
			}
			_, parentSeen := seen[p.ppid]
			_, pgidSeen := pgids[p.pgid]
			if parentSeen || pgidSeen {
				seen[p.pid] = struct{}{}
				if p.pgid > 1 {
					pgids[p.pgid] = struct{}{}
				}
				grew = true
				added = true
			}
		}
		if !grew {
			return added
		}
		added = true
	}
}

func treeAlive(seen, pgids map[int]struct{}, procs []procIDs) bool {
	for _, p := range procs {
		if p.zombie {
			continue
		}
		if _, ok := seen[p.pid]; ok {
			return true
		}
		if _, ok := pgids[p.pgid]; ok && p.pgid > 1 {
			return true
		}
	}
	return false
}

func killPID(pid, self, selfPgid int) {
	if pid <= 1 || pid == self {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

func killGroup(pgid, self, selfPgid int) {
	if pgid <= 1 || pgid == self || pgid == selfPgid {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

func listProcesses() ([]procIDs, error) {
	// pid/ppid/pgid with suppressed headers. Works on Linux procps and Darwin.
	cmd := exec.Command("ps", "-axo", "pid=,ppid=,pgid=,state=")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	var procs []procIDs
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		pgid, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		procs = append(procs, procIDs{
			pid:    pid,
			ppid:   ppid,
			pgid:   pgid,
			zombie: strings.HasPrefix(fields[3], "Z"),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return procs, nil
}

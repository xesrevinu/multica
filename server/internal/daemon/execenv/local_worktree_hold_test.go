package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// installFakeWt puts a wt on PATH that logs every invocation and creates or
// removes worktrees with git. The real wt would run whatever hooks the
// checkout declares.
func installFakeWt(t *testing.T) (logPath, destRoot string) {
	t.Helper()
	logPath = filepath.Join(t.TempDir(), "wt.log")
	destRoot = filepath.Join(t.TempDir(), "worktrees")
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$WT_LOG"
gitroot=
base=HEAD
create=0
remove=0
prev=
last=
for arg in "$@"; do
  case "$prev" in
    -C) gitroot=$arg ;;
    --base) base=$arg ;;
  esac
  case "$arg" in
    --create) create=1 ;;
    remove) remove=1 ;;
  esac
  prev=$arg
  last=$arg
done
if [ "$remove" = 1 ]; then
  git -C "$gitroot" worktree remove --force "$last"
  exit 0
fi
san=$(printf '%s' "$last" | tr '/\\' '--')
dest="$WT_DEST_ROOT/$(basename "$gitroot")/$san"
mkdir -p "$(dirname "$dest")"
if [ "$create" = 1 ]; then
  git -C "$gitroot" worktree add -b "$last" "$dest" "$base"
else
  git -C "$gitroot" worktree add "$dest" "$last"
fi
`
	if err := os.WriteFile(filepath.Join(bin, "wt"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WT_LOG", logPath)
	t.Setenv("WT_DEST_ROOT", destRoot)
	t.Setenv(IdleWorktreeRegistryEnv, filepath.Join(t.TempDir(), "idle-roots.json"))
	return logPath, destRoot
}

func enableWorktrunk(t *testing.T, repo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repo, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".config", "wt.toml"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, ".gitignore"), "node_modules/\n")
	gitRun(t, repo, "add", ".gitignore", ".config/wt.toml")
	gitRun(t, repo, "commit", "-m", "ignore node_modules")
}

func prepareHeldTurn(t *testing.T, repo, key, taskID string) *LocalWorktree {
	t.Helper()
	wt, err := PrepareLocalWorktree(LocalWorktreeParams{
		LocalPath:       repo,
		EnvRoot:         t.TempDir(),
		AgentName:       "J",
		TaskID:          taskID,
		ConversationKey: key,
		WorkspaceID:     testBranchOwner.WorkspaceID,
		AgentID:         testBranchOwner.AgentID,
		ConversationID:  testBranchOwner.ConversationID,
		HolderPID:       os.Getpid(),
	}, worktreeTestLogger())
	if err != nil {
		t.Fatalf("PrepareLocalWorktree(%s): %v", taskID, err)
	}
	return wt
}

func TestParkedWorktreeIsReusedWithoutStartingAgain(t *testing.T) {
	repo := newTestRepo(t)
	logPath, _ := installFakeWt(t)
	enableWorktrunk(t, repo)

	first := prepareHeldTurn(t, repo, "MUL-6881", turnOneTask)
	if err := os.MkdirAll(filepath.Join(first.Path, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(first.Path, "node_modules", "keep.txt"), "cached\n")
	writeFile(t, filepath.Join(first.WorkDir, "turn-one.txt"), "from turn one\n")
	if outcome := finalizeOK(t, first); outcome.Branch != "agent/mul-6881" {
		t.Fatalf("branch = %q", outcome.Branch)
	}
	if _, err := os.Stat(first.Path); err != nil {
		t.Fatalf("finalize removed the parked worktree: %v", err)
	}

	second := prepareHeldTurn(t, repo, "MUL-6881", turnTwoTask)
	if second.Path != first.Path {
		t.Fatalf("second path = %q, want the parked checkout %q", second.Path, first.Path)
	}
	if !second.Continued || second.Branch != first.Branch {
		t.Fatalf("second branch = %q continued = %v", second.Branch, second.Continued)
	}
	if got := readFile(t, filepath.Join(second.Path, "node_modules", "keep.txt")); got != "cached\n" {
		t.Fatalf("cached file = %q, want it kept across reuse", got)
	}
	if got := readFile(t, filepath.Join(second.WorkDir, "turn-one.txt")); got != "from turn one\n" {
		t.Fatalf("turn one work = %q", got)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(log), "switch") != 1 {
		t.Fatalf("follow-up should not start a new worktree:\n%s", log)
	}
	if strings.Contains(string(log), "\nremove") || strings.HasPrefix(string(log), "remove") {
		t.Fatalf("finalize should park, not remove:\n%s", log)
	}
	second.Discard(worktreeTestLogger())
}

func TestBusyWorktreeIsNotReusedOrReaped(t *testing.T) {
	repo := newTestRepo(t)
	installFakeWt(t)
	enableWorktrunk(t, repo)

	first := prepareHeldTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "turn-one.txt"), "in progress\n")

	second := prepareHeldTurn(t, repo, "MUL-6881", turnTwoTask)
	if second.Path == first.Path || second.Branch == first.Branch {
		t.Fatalf("busy checkout was reused: branch %q path %q", second.Branch, second.Path)
	}
	if _, err := os.Stat(filepath.Join(first.WorkDir, "turn-one.txt")); err != nil {
		t.Fatalf("in-use worktree was removed: %v", err)
	}

	hold, ok, err := readWorktreeHold(repo, first.Branch)
	if err != nil || !ok {
		t.Fatalf("active hold missing: ok=%v err=%v", ok, err)
	}
	hold.At = time.Now().Add(-48 * time.Hour)
	if err := writeWorktreeHold(hold); err != nil {
		t.Fatal(err)
	}
	removed, err := ReapIdleLocalWorktrees(time.Hour, 1, time.Now(), worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("reap removed %d checkouts, want 0 while the run is active", removed)
	}
	if _, err := os.Stat(first.Path); err != nil {
		t.Fatalf("active worktree reaped: %v", err)
	}
	second.Discard(worktreeTestLogger())
	first.Discard(worktreeTestLogger())
}

func TestMergedBranchDropsTheParkedWorktree(t *testing.T) {
	repo := newTestRepo(t)
	logPath, _ := installFakeWt(t)
	enableWorktrunk(t, repo)

	first := prepareHeldTurn(t, repo, "MUL-6881", turnOneTask)
	if err := os.MkdirAll(filepath.Join(first.Path, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(first.Path, "node_modules", "keep.txt"), "cached\n")
	writeFile(t, filepath.Join(first.WorkDir, "turn-one.txt"), "done\n")
	finalizeOK(t, first)
	gitRun(t, repo, "merge", "--ff-only", first.Branch)

	second := prepareHeldTurn(t, repo, "MUL-6881", turnTwoTask)
	defer second.Discard(worktreeTestLogger())
	if _, err := os.Stat(filepath.Join(first.Path, "node_modules", "keep.txt")); !os.IsNotExist(err) {
		t.Fatalf("merged checkout kept its cached files: %v", err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "remove") {
		t.Fatalf("merged checkout was not removed through wt:\n%s", log)
	}
}

func TestReapRemovesIdleWorktreesPastTTLAndCap(t *testing.T) {
	repo := newTestRepo(t)
	installFakeWt(t)
	enableWorktrunk(t, repo)

	var parked []*LocalWorktree
	for i, key := range []string{"MUL-1", "MUL-2", "MUL-3"} {
		wt := prepareHeldTurn(t, repo, key, []string{turnOneTask, turnTwoTask, turnThreeTask}[i])
		writeFile(t, filepath.Join(wt.WorkDir, "f.txt"), key+"\n")
		finalizeOK(t, wt)
		parked = append(parked, wt)
	}

	oldest, ok, err := readWorktreeHold(repo, parked[0].Branch)
	if err != nil || !ok {
		t.Fatalf("oldest hold missing: ok=%v err=%v", ok, err)
	}
	oldest.At = time.Now().Add(-48 * time.Hour)
	if err := writeWorktreeHold(oldest); err != nil {
		t.Fatal(err)
	}

	removed, err := ReapIdleLocalWorktrees(time.Hour, 8, time.Now(), worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("ttl reap removed %d, want 1", removed)
	}
	if _, err := os.Stat(parked[0].Path); !os.IsNotExist(err) {
		t.Fatalf("expired checkout still present: %v", err)
	}
	if _, err := os.Stat(parked[1].Path); err != nil {
		t.Fatalf("fresh checkout was removed: %v", err)
	}

	removed, err = ReapIdleLocalWorktrees(24*time.Hour, 1, time.Now(), worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("cap reap removed %d, want 1", removed)
	}
	surviving := 0
	for _, wt := range parked[1:] {
		if _, err := os.Stat(wt.Path); err == nil {
			surviving++
		}
	}
	if surviving != 1 {
		t.Fatalf("checkouts surviving the cap = %d, want 1", surviving)
	}
}

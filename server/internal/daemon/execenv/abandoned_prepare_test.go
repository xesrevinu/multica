package execenv

import (
	"encoding/json"
	"os"
	"testing"
)

// OX-173: a helper that finished after the prepare deadline left a complete
// worktree with an active hold for the live daemon. The caller now discards it.
func TestDiscardAbandonedPreparationReleasesWorktreeAndHold(t *testing.T) {
	repo := newTestRepo(t)
	installFakeWt(t)
	enableWorktrunk(t, repo)
	wt := prepareHeldTurn(t, repo, "OX-173", turnOneTask)
	if _, ok, _ := readWorktreeHold(repo, wt.Branch); !ok {
		t.Fatal("expected an active hold after prepare")
	}
	raw, err := json.Marshal(preparationResponse{Environment: &Environment{LocalWorktree: wt}})
	if err != nil {
		t.Fatal(err)
	}
	discardAbandonedPreparation(raw, worktreeTestLogger())
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree still there: %v", err)
	}
	if _, ok, _ := readWorktreeHold(repo, wt.Branch); ok {
		t.Fatal("hold still recorded")
	}
}

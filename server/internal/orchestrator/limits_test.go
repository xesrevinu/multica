package orchestrator

import (
	"encoding/json"
	"testing"
)

// Limits go through ParseSpec → Defaults exactly as the server loads them
// (store.go also calls Defaults on the stored JSON).
func TestLimitsUnsetVersusZeroOrNegative(t *testing.T) {
	base := "roles: {lead: l, reviewer: r, owners: [a]}\nrepo: {mode: github, slug: x/y}\n"
	cases := []struct {
		limits                     string
		idle, freeGB, queued, unbl int
	}{
		{"", 8, 20, 15, 2},
		{"limits: {idle_min: -1, min_free_gb: 0, queued_min: -1, unblocks: 0}\n", -1, 0, -1, 0},
		{"limits: {idle_min: 0}\n", 0, 20, 15, 2},
		{"limits: {idle_min: 15, min_free_gb: 40, queued_min: 30, unblocks: 5}\n", 15, 40, 30, 5},
	}
	for _, c := range cases {
		sp, err := ParseSpec([]byte(base + c.limits))
		if err != nil {
			t.Fatal(err)
		}
		if sp.Limits.Idle() != c.idle || sp.Limits.FreeGB() != c.freeGB || sp.Limits.Queued() != c.queued || sp.Limits.MaxUnblocks() != c.unbl {
			t.Fatalf("%q: got idle=%d free=%d queued=%d unblocks=%d", c.limits, sp.Limits.Idle(), sp.Limits.FreeGB(), sp.Limits.Queued(), sp.Limits.MaxUnblocks())
		}
		// the stored JSON round-trips through Defaults unchanged (store.go)
		var back Spec
		if err := json.Unmarshal(sp.JSON(), &back); err != nil {
			t.Fatal(err)
		}
		back.Defaults()
		if back.Limits.Idle() != c.idle || back.Limits.FreeGB() != c.freeGB {
			t.Fatalf("%q: JSON round trip changed limits", c.limits)
		}
	}
}

func TestDiskGuardZeroDisables(t *testing.T) {
	sp, err := ParseSpec([]byte("roles: {lead: l, reviewer: r, owners: [a]}\nrepo: {mode: github, slug: x/y}\nlimits: {min_free_gb: 0}\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Snapshot{Issue: Issue{Key: "K-1", Status: StTodo, Meta: map[string]string{}}, Busy: map[string]bool{}, FreeGB: 1}
	if d := sp.Decide(s, nil); d == nil || d.Tid != "T1_start" {
		t.Fatalf("min_free_gb: 0 must disable the disk guard, got %+v", d)
	}
}

func TestIdleByBackendDefaults(t *testing.T) {
	sp, err := ParseSpec([]byte("roles: {lead: l, reviewer: r, owners: [a]}\nrepo: {mode: github, slug: x/y}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if sp.Limits.IdleFor("gbot") != -1 || sp.Limits.IdleFor("GBot") != -1 || sp.Limits.IdleFor("claude") != 8 || sp.Limits.IdleFor("") != 8 {
		t.Fatalf("preset: gbot off, others idle_min: %+v", sp.Limits.IdleMinByBackend)
	}
	sp, err = ParseSpec([]byte("roles: {lead: l, reviewer: r, owners: [a]}\nrepo: {mode: github, slug: x/y}\nlimits: {idle_min_by_backend: {gbot: 45}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if sp.Limits.IdleFor("gbot") != 45 {
		t.Fatal("an explicit gbot value wins over the preset")
	}
}

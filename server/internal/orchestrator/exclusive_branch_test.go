package orchestrator

import "testing"

// The daemon is told agent/<key> is exclusive only while the engine actually
// drives the project: paused, shadow, unknown and disabled teams keep the
// daemon's ordinary sibling forking.
func TestExclusiveBranchOnlyForOwnedProjects(t *testing.T) {
	sp, err := ParseSpec([]byte("roles: {lead: l, reviewer: r, owners: [a]}\nrepo: {mode: github, slug: x/y}\n"))
	if err != nil {
		t.Fatal(err)
	}
	shadow := *sp
	shadow.Shadow = true
	s := &Service{teams: map[string]*Team{
		"p":      {ProjectID: "p", Spec: sp},
		"paused": {ProjectID: "paused", Spec: sp, Paused: true},
		"shadow": {ProjectID: "shadow", Spec: &shadow},
	}}
	for proj, want := range map[string]string{"p": "agent/att-62", "paused": "", "shadow": "", "none": ""} {
		if got := s.ExclusiveBranch(proj, "ATT-62"); got != want {
			t.Errorf("%s: got %q want %q", proj, got, want)
		}
	}
	if got := s.ExclusiveBranch("p", ""); got != "" {
		t.Errorf("no issue key: got %q", got)
	}
	s.disabled = true
	if got := s.ExclusiveBranch("p", "ATT-62"); got != "" {
		t.Errorf("disabled engine: got %q", got)
	}
	var nilSvc *Service
	if nilSvc.ExclusiveBranch("p", "ATT-62") != "" {
		t.Error("nil service")
	}
}

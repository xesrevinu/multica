package orchestrator

import (
	"strings"
	"testing"
)

const expertTeam = `orchestrator:
  roles:
    lead: lead
    reviewer: rev
    owners: [heavy]
    routes:
      - {labels: [area:hard, area:host], owners: [heavy]}
  experts:
    - name: deep-work
      agents: [gbot]
      when:
        labels: [area:hard, area:cross-cutting]
        not_labels: [needs-GUI-Master]
      skills: [swarm]
      limits: {concurrent: 1}
      instructions: |
        Do the hard part.
    - name: research
      agents: [gbot]
      when: {labels: [area:research]}
      mode: consult
      limits: {concurrent: 1}
    - name: pinned
      agents: [gbot]
      when: {metadata: {ask: deep}}
  repo: {mode: github, slug: x/y}
  limits: {slots: {cargo: 2}}
  acceptance: {default_for_labels: [area:apps]}
`

func TestExpertSpec(t *testing.T) {
	sp, err := ParseSpec([]byte(expertTeam))
	if err != nil {
		t.Fatal(err)
	}
	if contains(sp.Roles.Owners, "gbot") {
		t.Fatal("expert agents must not be merged into roles.owners")
	}
	if !sp.isOwner("gbot") || !sp.isOwner("heavy") {
		t.Fatal("expert agents count as owners for held-assignee and flow override")
	}
	if sp.Experts[0].Mode != "implement" || sp.Experts[1].Mode != "consult" {
		t.Fatalf("modes: %+v", sp.Experts)
	}
	// an expert who is also a declared owner may appear on a route
	if _, err := ParseSpec([]byte(`orchestrator:
  roles: {lead: l, reviewer: r, owners: [heavy, gbot], routes: [{labels: [area:host], owners: [gbot]}]}
  experts: [{name: deep, agents: [gbot], when: {labels: [area:hard]}}]
  repo: {mode: github, slug: x/y}
`)); err != nil {
		t.Fatal(err)
	}
	bad := []struct{ name, y, want string }{
		{"dup", "experts: [{name: a, agents: [g], when: {labels: [x]}}, {name: a, agents: [g], when: {labels: [y]}}]", "duplicated"},
		{"empty name", "experts: [{name: '', agents: [g], when: {labels: [x]}}]", "empty"},
		{"no agents", "experts: [{name: a, agents: [], when: {labels: [x]}}]", "at least one agent"},
		{"empty when", "experts: [{name: a, agents: [g], when: {}}]", "when needs"},
		{"bad mode", "experts: [{name: a, agents: [g], when: {labels: [x]}, mode: pair}]", "implement or consult"},
		{"reviewer", "experts: [{name: a, agents: [rev], when: {labels: [x]}}]", "reviewer"},
		{"route", "experts: [{name: a, agents: [gbot], when: {labels: [x]}}]\n  roles: {lead: lead, reviewer: rev, owners: [heavy], routes: [{labels: [x], owners: [gbot]}]}", "roles.routes"},
		{"cap", "experts: [{name: a, agents: [g], when: {labels: [x]}, limits: {concurrent: -1}}]", "concurrent"},
	}
	for _, c := range bad {
		y := "orchestrator:\n  roles: {lead: lead, reviewer: rev, owners: [heavy]}\n  " + c.y + "\n  repo: {mode: github, slug: x/y}\n"
		if c.name == "route" {
			y = "orchestrator:\n  " + c.y + "\n  repo: {mode: github, slug: x/y}\n"
		}
		_, err := ParseSpec([]byte(y))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: want %q, got %v", c.name, c.want, err)
		}
	}
}

func TestMatchExpertPrecedence(t *testing.T) {
	sp, err := ParseSpec([]byte(expertTeam))
	if err != nil {
		t.Fatal(err)
	}
	start := func(labels []string, meta map[string]string) *Decision {
		t.Helper()
		s := fresh()
		s.Issue.Labels = labels
		for k, v := range meta {
			s.Issue.Meta[k] = v
		}
		return sp.Decide(s, nil)
	}
	d := start([]string{"area:hard"}, nil)
	want(t, d, "T1_start", StTodo, "gbot")
	if d.Meta["expert"] != "deep-work" {
		t.Fatalf("expert meta: %+v", d)
	}
	d = start([]string{"area:host"}, nil)
	want(t, d, "T1_start", StTodo, "heavy")
	if _, ok := d.Meta["expert"]; ok {
		t.Fatalf("route must not set expert: %+v", d)
	}
	d = start(nil, nil)
	want(t, d, "T1_start", StTodo, "heavy")
	if _, ok := d.Meta["expert"]; ok {
		t.Fatalf("unlabelled issue reached an expert: %+v", d)
	}
	d = start([]string{"area:hard", "needs-GUI-Master"}, nil)
	want(t, d, "T1_start", StTodo, "heavy")
	d = start([]string{"area:research"}, nil)
	want(t, d, "T1_start", StTodo, "gbot")
	if d.Meta["expert"] != "research" {
		t.Fatalf("consult expert: %+v", d)
	}
	d = start([]string{"area:research"}, map[string]string{"expert_done": "research"})
	want(t, d, "T1_start", StTodo, "heavy")
	d = start(nil, map[string]string{"ask": "deep"})
	want(t, d, "T1_start", StTodo, "gbot")
	if d.Meta["expert"] != "pinned" {
		t.Fatalf("metadata opt-in: %+v", d)
	}
	s := fresh()
	s.Issue.Labels = []string{"area:hard"}
	s.Busy["gbot"] = true
	if d := sp.Decide(s, nil); d == nil || d.Wait != "expert deep-work busy" || d.Assignee != "" {
		t.Fatalf("busy expert must wait, not fall back: %+v", d)
	}
	s = fresh()
	s.Issue.Labels = []string{"area:hard"}
	s.Siblings = []Issue{{Key: "ATT-8", Status: StInProgress, Meta: map[string]string{"expert": "deep-work"}}}
	if d := sp.Decide(s, nil); d == nil || !strings.Contains(d.Wait, "expert deep-work") {
		t.Fatalf("cap: %+v", d)
	}
	s.Siblings[0].Status = StInReview
	want(t, sp.Decide(s, nil), "T1_start", StTodo, "gbot")
}

func TestConsultRoundTrip(t *testing.T) {
	sp, err := ParseSpec([]byte(expertTeam))
	if err != nil {
		t.Fatal(err)
	}
	s := fresh()
	s.Issue.Labels = []string{"area:research"}
	d := sp.Decide(s, nil)
	want(t, d, "T1_start", StTodo, "gbot")
	s.Issue.Status = StInProgress
	s.Issue.AssigneeType, s.Issue.Assignee = "agent", "gbot"
	s.Issue.Meta = cloneMeta(d.Meta)
	s.Issue.Meta["phase"] = PhReviewRequested
	s.Busy["gbot"] = true // this issue is what makes the consult agent look busy
	d = sp.Decide(s, nil)
	want(t, d, "T_expert_consulted", StTodo, "heavy")
	if d.Meta["expert_done"] != "research" || d.Meta["owner"] != "heavy" || d.Meta["phase"] != PhImplementing || d.Meta["expert"] != "" {
		t.Fatalf("consult handoff: %+v", d)
	}
	// implement mode still goes to review, and does not need a consult transition
	s = fresh()
	s.Issue.Labels = []string{"area:hard"}
	d = sp.Decide(s, nil)
	s.Issue.Status = StInProgress
	s.Issue.AssigneeType, s.Issue.Assignee = "agent", d.Assignee
	s.Issue.Meta = cloneMeta(d.Meta)
	s.Issue.Meta["phase"] = PhReviewRequested
	s.Head = "abc"
	want(t, sp.Decide(s, ok(false)), "T2_review", StInReview, "rev")
}

func TestRenderExpertPreset(t *testing.T) {
	sp, err := ParseSpec([]byte(expertTeam))
	if err != nil {
		t.Fatal(err)
	}
	roles, err := RenderPreset(sp, "ws")
	if err != nil {
		t.Fatal(err)
	}
	var lead, experts int
	for _, r := range roles {
		if strings.Contains(r.Instructions, "{{") {
			t.Fatalf("%s: unfilled placeholder", r.Agent)
		}
		switch r.Role {
		case "lead":
			lead++
			for _, want := range []string{"Routing (first match wins):", "deep-work ← area:hard, area:cross-cutting", "research ← area:research (gbot, consult)", "area:hard, area:host → heavy", "otherwise: heavy", "Slots: needs:cargo (2)", "default for area:apps", "Label each sub-issue"} {
				if !strings.Contains(r.Instructions, want) {
					t.Errorf("lead missing %q", want)
				}
			}
		case "expert":
			experts++
			if r.Expert == "" || !contains(r.Skills, "open-pr") || !contains(r.Skills, "figure-it-out") {
				t.Fatalf("expert role: %+v", r.Skills)
			}
			if !strings.Contains(r.Instructions, "Consult mode") || !strings.Contains(r.Instructions, "Mode: "+modeOf(sp, r.Expert)) {
				t.Fatalf("expert instructions for %s", r.Expert)
			}
		}
	}
	if lead != 1 || experts != 3 {
		t.Fatalf("lead %d experts %d (roles %d)", lead, experts, len(roles))
	}
	plain, err := RenderPreset(spec(t), "ws")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range plain {
		if r.Role == "lead" && strings.Contains(r.Instructions, "- experts:") {
			t.Fatal("specs without experts must not invent an experts line")
		}
	}
}

func modeOf(sp *Spec, name string) string {
	if e := sp.expertByName(name); e != nil {
		return e.Mode
	}
	return ""
}

func TestExpertSkillBudgetWarns(t *testing.T) {
	y := `orchestrator:
  roles: {lead: lead, reviewer: rev, owners: [heavy]}
  experts:
    - name: wide
      agents: [gbot]
      when: {labels: [area:hard]}
      skills: [interrogate, architect, poteto-mode, tdd, why, how, recall, ketchup]
  repo: {mode: github, slug: x/y}
`
	sp, err := ParseSpec([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	roles, err := RenderPreset(sp, "ws")
	if err != nil {
		t.Fatal(err)
	}
	var warned bool
	for _, r := range roles {
		if r.Expert == "wide" && r.Warn != "" {
			warned = true
		}
	}
	if !warned {
		t.Fatal("per-expert skills over the context budget must warn, not fail")
	}
}

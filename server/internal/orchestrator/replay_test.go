package orchestrator

// Deterministic replay of recorded scenarios. Each testdata/scenarios/*.yaml
// is a sequence of steps: world changes (what agents, people, GitHub and the
// clock did) followed by the decision the engine must take. The simulator
// applies every decision the way the actuator would, so a scenario reads
// like the transition log of a real run. Scenarios that mirror an e2e run
// name it in `recorded:`; their transition sequence must equal the line for
// that issue in testdata/recorded.txt (exported from the e2e database).

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type probeSpec struct {
	OK      bool   `yaml:"ok"`
	Kind    string `yaml:"kind"`
	Pending bool   `yaml:"pending"`
	Detail  string `yaml:"detail"`
	SHA     string `yaml:"sha"`
	PR      string `yaml:"pr"`
}

func (p *probeSpec) result() ProbeResult {
	if p == nil {
		return ProbeResult{OK: true, Kind: "dry", SHA: "merged"}
	}
	d := p.Detail
	if d == "" {
		d = p.Kind
	}
	return ProbeResult{OK: p.OK, Kind: p.Kind, Pending: p.Pending, Detail: d, SHA: p.SHA, PR: p.PR}
}

type worldChange struct {
	Meta      map[string]string `yaml:"meta"`       // agent/member metadata write ("" deletes)
	Head      *string           `yaml:"head"`       // branch head on the remote
	Run       string            `yaml:"run"`        // queued | running | completed | failed | cancelled
	Reason    string            `yaml:"reason"`     // failure_reason of a failed run
	Backend   string            `yaml:"backend"`    // runtime provider of the run (gbot, claude, ...)
	Error     string            `yaml:"error"`      // agent_task_queue.error of a finished run
	Activity  bool              `yaml:"activity"`   // the running run emitted a message now
	Status    string            `yaml:"status"`     // out-of-band status edit
	Assignee  string            `yaml:"assignee"`   // out-of-band assignee edit (agent name)
	Labels    []string          `yaml:"labels"`     // replace labels
	Busy      map[string]bool   `yaml:"busy"`       // owners busy elsewhere
	FreeGB    *int              `yaml:"free_gb"`    // disk free
	Advance   string            `yaml:"advance"`    // move the clock, e.g. 25m
	Siblings  []Issue           `yaml:"siblings"`   // replace project siblings
	SamePatch [][2]string       `yaml:"same_patch"` // pairs with equal patch-id
	Precheck  *probeSpec        `yaml:"precheck"`   // result of the review pre-check from now on
	Merge     *probeSpec        `yaml:"merge"`      // result of the merge gate from now on
}

type expectation struct {
	Tid      string            `yaml:"tid"`  // transition id; "wait" or "none" for no transition
	Wait     string            `yaml:"wait"` // substring of the wait reason
	Status   string            `yaml:"status"`
	Assignee string            `yaml:"assignee"`
	Meta     map[string]string `yaml:"meta"`
}

type step struct {
	Note   string       `yaml:"note"`
	Do     *worldChange `yaml:"do"`
	Expect expectation  `yaml:"expect"`
}

type scenario struct {
	Name     string    `yaml:"name"`
	Recorded string    `yaml:"recorded"`
	Spec     yaml.Node `yaml:"spec"`
	Issue    Issue     `yaml:"issue"`
	Steps    []step    `yaml:"steps"`
}

type world struct {
	sp        *Spec
	snap      Snapshot
	precheck  *probeSpec
	merge     *probeSpec
	samePatch map[string]bool
	tids      []string
}

const defaultScenarioSpec = `orchestrator:
  roles: {lead: lead, reviewer: rev, owners: [a, b]}
  repo: {mode: github, slug: x/y, required_checks: [test]}
`

func loadScenario(t *testing.T, path string) *scenario {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sc scenario
	if err := yaml.Unmarshal(b, &sc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return &sc
}

func newWorld(t *testing.T, sc *scenario) *world {
	specYAML := defaultScenarioSpec
	if sc.Spec.Kind != 0 {
		b, err := yaml.Marshal(map[string]*yaml.Node{"orchestrator": &sc.Spec})
		if err != nil {
			t.Fatal(err)
		}
		specYAML = string(b)
	}
	sp, err := ParseSpec([]byte(specYAML))
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	i := sc.Issue
	if i.Key == "" {
		i.Key = "ATT-1"
	}
	if i.ID == "" {
		i.ID = i.Key
	}
	if i.Status == "" {
		i.Status = StTodo
	}
	if i.Meta == nil {
		i.Meta = map[string]string{}
	}
	return &world{sp: sp, samePatch: map[string]bool{},
		snap: Snapshot{Now: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC), Issue: i, Busy: map[string]bool{}, FreeGB: 100}}
}

func (w *world) change(t *testing.T, c *worldChange) {
	s := &w.snap
	for k, v := range c.Meta {
		if v == "" {
			delete(s.Issue.Meta, k)
		} else {
			s.Issue.Meta[k] = v
		}
	}
	if c.Head != nil {
		s.Head = *c.Head
	}
	if c.Advance != "" {
		d, err := time.ParseDuration(c.Advance)
		if err != nil {
			t.Fatal(err)
		}
		s.Now = s.Now.Add(d)
	}
	switch c.Run {
	case "":
	case "queued":
		at := s.Now
		s.ActiveRun = &Run{ID: "run-" + at.Format("150405"), Status: "queued", CreatedAt: &at}
	case "running":
		at := s.Now
		created := at
		if s.ActiveRun != nil && s.ActiveRun.CreatedAt != nil {
			created = *s.ActiveRun.CreatedAt
		}
		s.ActiveRun = &Run{ID: "run-" + at.Format("150405"), Status: "running", StartedAt: &at, CreatedAt: &created, Backend: c.Backend}
	default:
		at := s.Now
		s.ActiveRun = nil
		s.LastRun = &Run{ID: "run-done", Status: c.Run, Reason: c.Reason, EndedAt: &at, Error: c.Error}
	}
	if c.Activity && s.ActiveRun != nil {
		at := s.Now
		s.ActiveRun.LastActivity = &at
	}
	if c.Status != "" {
		s.Issue.Status = c.Status
	}
	if c.Assignee != "" {
		s.Issue.AssigneeType, s.Issue.Assignee = "agent", c.Assignee
	}
	if c.Labels != nil {
		s.Issue.Labels = c.Labels
	}
	for k, v := range c.Busy {
		s.Busy[k] = v
	}
	if c.FreeGB != nil {
		s.FreeGB = *c.FreeGB
	}
	if c.Siblings != nil {
		s.Siblings = c.Siblings
	}
	for _, p := range c.SamePatch {
		w.samePatch[p[0]+"|"+p[1]], w.samePatch[p[1]+"|"+p[0]] = true, true
	}
	if c.Precheck != nil {
		w.precheck = c.Precheck
	}
	if c.Merge != nil {
		w.merge = c.Merge
	}
}

func (w *world) probe(_ *Snapshot, merge bool) ProbeResult {
	if merge {
		return w.merge.result()
	}
	if w.precheck == nil {
		return ProbeResult{OK: true, Kind: "dry"}
	}
	return w.precheck.result()
}

// apply does to the simulated world what the actuator does to the database.
func (w *world) apply(d *Decision) {
	s := &w.snap
	for k, v := range d.Meta {
		if v == "" {
			delete(s.Issue.Meta, k)
		} else {
			s.Issue.Meta[k] = v
		}
	}
	if d.Status != "" {
		s.Issue.Status = d.Status
	}
	switch d.AssigneeKind {
	case "agent":
		s.Issue.AssigneeType, s.Issue.Assignee = "agent", d.Assignee
	case "member":
		s.Issue.AssigneeType, s.Issue.Assignee = "member", d.Assignee
	}
	if d.CancelRun != "" {
		s.ActiveRun, s.LastRun = nil, &Run{ID: d.CancelRun, Status: "cancelled"}
	}
	if d.StartsRun() {
		// a fresh run is queued; the scenario reports how it goes
		s.ActiveRun, s.LastRun = nil, nil
	}
}

func normTid(tid string) string {
	if strings.HasPrefix(tid, "T5_rerun") {
		return "T5_rerun"
	}
	return tid
}

func (w *world) step(t *testing.T, n int, st step) {
	t.Helper()
	if st.Do != nil {
		w.change(t, st.Do)
	}
	w.snap.SamePatch = func(a, b string) bool { return w.samePatch[a+"|"+b] }
	d := w.sp.Decide(&w.snap, w.probe)
	e := st.Expect
	where := func() string { return "step " + itoa(n+1) + " (" + st.Note + ")" }
	switch {
	case e.Tid == "none":
		if d != nil {
			t.Fatalf("%s: want no decision, got %+v", where(), d)
		}
		return
	case e.Tid == "wait" || e.Wait != "":
		if d == nil || d.Tid != "" || !strings.Contains(d.Wait, e.Wait) {
			t.Fatalf("%s: want wait %q, got %+v", where(), e.Wait, d)
		}
		return
	}
	if d == nil || normTid(d.Tid) != normTid(e.Tid) {
		t.Fatalf("%s: want %s, got %+v", where(), e.Tid, d)
	}
	if e.Status != "" && d.Status != e.Status {
		t.Fatalf("%s: want status %s, got %+v", where(), e.Status, d)
	}
	if e.Assignee != "" && d.Assignee != e.Assignee {
		t.Fatalf("%s: want assignee %s, got %+v", where(), e.Assignee, d)
	}
	for k, v := range e.Meta {
		if got, ok := d.Meta[k]; !ok || (v != "*" && got != v) {
			t.Fatalf("%s: want meta %s=%q, got %q (%+v)", where(), k, v, got, d)
		}
	}
	w.tids = append(w.tids, normTid(d.Tid))
	w.apply(d)
}

func recordedRuns(t *testing.T) map[string][]string {
	f, err := os.Open("testdata/recorded.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fs := strings.Fields(line)
		for _, tid := range fs[1:] {
			if strings.Contains(tid, "(") { // failed/shadow attempts are not engine decisions
				continue
			}
			out[fs[0]] = append(out[fs[0]], normTid(tid))
		}
	}
	return out
}

func TestReplayScenarios(t *testing.T) {
	files, _ := filepath.Glob("testdata/scenarios/*.yaml")
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no scenarios")
	}
	rec := recordedRuns(t)
	for _, f := range files {
		sc := loadScenario(t, f)
		t.Run(strings.TrimSuffix(filepath.Base(f), ".yaml"), func(t *testing.T) {
			w := newWorld(t, sc)
			for n, st := range sc.Steps {
				w.step(t, n, st)
			}
			if sc.Recorded != "" {
				want, ok := rec[sc.Recorded]
				if !ok {
					t.Fatalf("recorded run %s missing from testdata/recorded.txt", sc.Recorded)
				}
				if strings.Join(w.tids, " ") != strings.Join(want, " ") {
					t.Fatalf("transition sequence differs from recorded %s\n got: %v\nwant: %v", sc.Recorded, w.tids, want)
				}
			}
		})
	}
}

package orchestrator

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	depLineRe = regexp.MustCompile(`(?im)^[\s>*\-]*(?:依赖|depends on|dependencies|deps)\s*[:：]\s*(.*)$`)
	depKeyRe  = regexp.MustCompile(`\b[A-Z][A-Z0-9]*-\d+\b`)
	noDepsRe  = regexp.MustCompile(`(?i)^[\s"'「」]*(无|none|n/a|-)`)
)

// parseDeps reads the first 依赖:/Depends on: line of a description. ok=false
// when the line exists but names no issue and does not say "none".
// found=false when there is no such line.
func parseDeps(desc string) (keys []string, found, ok bool) {
	m := depLineRe.FindStringSubmatch(desc)
	if m == nil {
		return nil, false, true
	}
	keys = depKeyRe.FindAllString(m[1], -1)
	if len(keys) == 0 && !noDepsRe.MatchString(m[1]) {
		return nil, true, false
	}
	return keys, true, true
}

// startCandidate: StartsHere, plus backlog issues when auto_promote is on.
func (sp *Spec) startCandidate(status string, meta map[string]string) bool {
	if StartsHere(status, meta) {
		return true
	}
	return sp.autoStartMode() != AutoStartOff && status == StBacklog && meta["phase"] == ""
}

// stageWait names the first unfinished lower-stage sibling under the same
// parent, "" when the stage barrier is clear.
func stageWait(s *Snapshot) string {
	i := &s.Issue
	if i.ParentID == "" || i.Stage <= 0 {
		return ""
	}
	for _, x := range s.Siblings {
		if x.ParentID == i.ParentID && x.Stage > 0 && x.Stage < i.Stage && x.Status != StDone && x.Status != StCancelled {
			return fmt.Sprintf("waiting: stage %d sibling %s not done", x.Stage, x.Key)
		}
	}
	return ""
}

// depsWait: the 依赖: line of the description plus metadata deps must all be
// done. A line naming no issue, or a dependency outside the project, waits
// for a member instead of guessing.
func depsWait(s *Snapshot) string {
	i := &s.Issue
	deps, _, ok := parseDeps(i.Description)
	if !ok {
		return "waiting: 依赖: line names no issue; fix it or start by hand"
	}
	for _, d := range strings.Split(i.Meta["deps"], ",") {
		if d = strings.TrimSpace(d); d != "" {
			deps = append(deps, d)
		}
	}
	by := map[string]Issue{}
	for _, x := range s.Siblings {
		by[strings.ToUpper(x.Key)] = x
	}
	for _, d := range deps {
		x, known := by[strings.ToUpper(d)]
		if !known {
			return "waiting: dependency " + d + " is not in this project"
		}
		if x.Status != StDone && x.Status != StCancelled {
			return "waiting: dependency " + d + " not done"
		}
	}
	return ""
}

// decideEpic: an issue with sub-issues is an epic. No agent runs on it; it
// reports "epic: n/m done", follows its children into in_progress, and once
// every child is done or cancelled it is done, or goes to the human for its
// own acceptance when it needs one. epic=false: not an epic.
func (sp *Spec) decideEpic(s *Snapshot) (*Decision, bool) {
	i := &s.Issue
	total, done, started := 0, 0, false
	for _, x := range s.Siblings {
		if x.ParentID != i.ID {
			continue
		}
		total++
		switch x.Status {
		case StDone, StCancelled:
			done++
		case StBacklog:
		default:
			started = true
		}
	}
	if total == 0 {
		return nil, false
	}
	progress := fmt.Sprintf("epic: %d/%d sub-issues done", done, total)
	if i.Status == StBacklog {
		return &Decision{Wait: progress + " (backlog: move it to todo to track it)"}, true
	}
	if done < total {
		if (started || done > 0) && i.Status != StInProgress && i.Status != StBlocked {
			return &Decision{Tid: "T_epic_progress", Status: StInProgress, Why: progress}, true
		}
		return &Decision{Wait: progress}, true
	}
	if !sp.NeedsAcceptance(i) {
		return &Decision{Tid: "T_epic_done", Status: StDone, Why: progress}, true
	}
	switch a := i.Meta["acceptance"]; {
	case a == "ok":
		return &Decision{Tid: "T_epic_done", Status: StDone, Why: progress + ", accepted"}, true
	case strings.HasPrefix(a, "reject"):
		return &Decision{Wait: progress + "; acceptance rejected: add follow-up sub-issues"}, true
	}
	if i.Meta["phase"] == PhAwaitAcceptance {
		return &Decision{Wait: progress + "; waiting for acceptance"}, true
	}
	return &Decision{Tid: "T_epic_acceptance", Status: StInReview, AssigneeKind: "member", Assignee: "@human",
		Meta:   map[string]string{"phase": PhAwaitAcceptance, "acceptance": ""},
		Notify: fmt.Sprintf("%s (%s): all %d sub-issues are done; it waits for your acceptance: `multica flow accept %s` or `multica flow reject %s --reason ...`", i.Key, i.Title, total, i.Key, i.Key)}, true
}

// decidePromote is T0: an eligible backlog issue is started directly
// (auto_start: on) or its verdict recorded (dry_run). Every not-yet reason is
// a visible wait.
func (sp *Spec) decidePromote(s *Snapshot) *Decision {
	d := sp.promoteVerdict(s)
	if sp.autoStartMode() == AutoStartDryRun {
		if d != nil && d.Tid != "" {
			return &Decision{Wait: "auto-start dry run: would start → " + d.Assignee}
		}
		why := "no 依赖: line"
		if d != nil {
			why = d.Wait
		}
		return &Decision{Wait: "auto-start dry run: would not start (" + why + ")"}
	}
	return d
}

func (sp *Spec) promoteVerdict(s *Snapshot) *Decision {
	i := &s.Issue
	for _, x := range s.Siblings {
		if x.ParentID == i.ID {
			return &Decision{Wait: "epic: its sub-issues carry the work"}
		}
	}
	if _, found, _ := parseDeps(i.Description); !found && i.Meta["deps"] == "" && i.ParentID == "" {
		return nil // no declared plan: leave it to a member
	}
	if w := depsWait(s); w != "" {
		return &Decision{Wait: w}
	}
	if w := stageWait(s); w != "" {
		return &Decision{Wait: w}
	}
	d := sp.decideStart(s)
	if d == nil || d.Tid != "T1_start" {
		return d
	}
	d.Tid, d.Status = "T0_dispatch", StInProgress
	d.Why = "auto-start: dependencies done, stage open → " + d.Assignee
	return d
}

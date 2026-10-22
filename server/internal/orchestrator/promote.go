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
	doneish := func(st string) bool { return st == StDone || st == StCancelled }
	by := map[string]Issue{}
	for _, x := range s.Siblings {
		by[strings.ToUpper(x.Key)] = x
		if x.ParentID == i.ID {
			return &Decision{Wait: "epic: its sub-issues carry the work"}
		}
	}
	deps, found, ok := parseDeps(i.Description)
	if !ok {
		return &Decision{Wait: "dependency line names no issue; start it by hand"}
	}
	for _, d := range strings.Split(i.Meta["deps"], ",") {
		if d = strings.TrimSpace(d); d != "" {
			deps = append(deps, d)
		}
	}
	if !found && len(deps) == 0 && i.ParentID == "" {
		return nil // no declared plan: leave it to a member
	}
	for _, d := range deps {
		x, known := by[strings.ToUpper(d)]
		if !known {
			return &Decision{Wait: "dependency " + d + " is not in this project; start it by hand"}
		}
		if !doneish(x.Status) {
			return &Decision{Wait: "waiting: dependency " + d + " not done"}
		}
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

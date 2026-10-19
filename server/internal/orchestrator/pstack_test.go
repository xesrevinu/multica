package orchestrator

import (
	"io/fs"
	"strings"
	"testing"
)

func TestPstackManifestCoversEverySkill(t *testing.T) {
	base := "preset/impl-review-merge"
	m, contract, err := loadPstack(base)
	if err != nil || m == nil || !strings.Contains(contract, "force-push") {
		t.Fatal("load", err)
	}
	dirs, _ := fs.ReadDir(presetFS, base+"/pstack/skills")
	seen := map[string]bool{}
	for _, d := range dirs {
		seen[d.Name()] = true
		if _, ok := m.Skills[d.Name()]; !ok {
			t.Errorf("skill %s missing from manifest", d.Name())
		}
	}
	valid := map[string]bool{"lead": true, "owner": true, "reviewer": true, "acceptance": true}
	for n, e := range m.Skills {
		if !seen[n] {
			t.Errorf("manifest skill %s not vendored", n)
		}
		switch e.Action {
		case "keep":
		case "adapt", "drop", "operator":
			if e.Note == "" {
				t.Errorf("%s: %s needs a reason", n, e.Action)
			}
		default:
			t.Errorf("%s: bad action %q", n, e.Action)
		}
		if _, clash := presetSkillSet()[n]; clash {
			t.Errorf("%s clashes with a native preset skill", n)
		}
	}
	for r, rs := range m.Roles {
		if !valid[r] {
			t.Errorf("bad role %s", r)
		}
		for _, n := range rs.Skills {
			if e, ok := m.Skills[n]; !ok || (e.Action != "keep" && e.Action != "adapt") {
				t.Errorf("role %s mounts %s, which is not a keep/adapt skill", r, n)
			}
		}
	}
	files, err := PresetFiles("")
	if err != nil {
		t.Fatal(err)
	}
	if f := files["pstack/skills/tdd/SKILL.md"]; !strings.Contains(f, "Workspace scope") {
		t.Fatal("contract not appended")
	}
	if f := files["pstack/skills/poteto-mode/SKILL.md"]; !strings.Contains(f, "overrides the text above") {
		t.Fatal("adapt note missing")
	}
	for p := range files {
		if strings.HasPrefix(p, "pstack/skills/setup-pstack/") || strings.HasPrefix(p, "pstack/skills/make-bot-ui/") {
			t.Fatalf("dropped skill exported: %s", p)
		}
	}
}

func presetSkillSet() map[string]bool {
	s := map[string]bool{}
	for _, l := range presetSkills {
		for _, n := range l {
			s[n] = true
		}
	}
	return s
}

// Every role mounts only its own skill set and stays inside an explicit
// context budget (bytes of instructions + mounted SKILL.md, all loaded).
func TestRoleContextBudget(t *testing.T) {
	for _, role := range []string{"lead", "owner", "reviewer", "acceptance"} {
		n, skills, b, err := RoleContextBytes("", role)
		if err != nil {
			t.Fatal(err)
		}
		if b.BudgetBytes <= 0 || b.MaxSkills <= 0 {
			t.Fatalf("%s: no explicit budget in manifest roles", role)
		}
		pst := len(skills) - len(presetSkills[role])
		t.Logf("%-10s %5d / %5d bytes, %d pstack skills (max %d), mounted: %v", role, n, b.BudgetBytes, pst, b.MaxSkills, skills)
		if n > b.BudgetBytes {
			t.Errorf("%s: context %d bytes > budget %d", role, n, b.BudgetBytes)
		}
		if pst > b.MaxSkills {
			t.Errorf("%s: %d pstack skills > max %d", role, pst, b.MaxSkills)
		}
	}
	// the reviewer runs the multi-dimension high-confidence review
	if _, skills, _, _ := RoleContextBytes("", "reviewer"); !contains(skills, "interrogate") || !contains(skills, "review-verdict") {
		t.Fatalf("reviewer must mount interrogate + review-verdict: %v", skills)
	}
}

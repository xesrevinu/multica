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
		for _, r := range e.Roles {
			if !valid[r] {
				t.Errorf("%s: bad role %s", n, r)
			}
		}
		if (e.Action == "keep" || e.Action == "adapt") && len(e.Roles) == 0 {
			t.Errorf("%s: unmapped", n)
		}
		if _, clash := presetSkillSet()[n]; clash {
			t.Errorf("%s clashes with a native preset skill", n)
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

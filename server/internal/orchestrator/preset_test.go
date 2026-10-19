package orchestrator

import (
	"strings"
	"testing"
)

func TestRenderPreset(t *testing.T) {
	sp := spec(t)
	roles, err := RenderPreset(sp, "ws")
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 2+len(sp.Roles.Owners) {
		t.Fatalf("roles: %d", len(roles))
	}
	for _, r := range roles {
		if strings.Contains(r.Instructions, "{{") {
			t.Fatalf("%s: unfilled placeholder", r.Agent)
		}
		if !strings.Contains(r.Instructions, "Workspace scope") || !strings.Contains(r.Instructions, "orchestrator_owned") {
			t.Fatalf("%s: team contract missing", r.Agent)
		}
		if len(r.Skills) == 0 {
			t.Fatalf("%s: no skills", r.Agent)
		}
	}
	files, err := PresetFiles("")
	if err != nil || files["skills/verify-gates/SKILL.md"] == "" || files["checklist.md"] == "" {
		t.Fatal("preset files", err)
	}
}

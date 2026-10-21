package execenv

import (
	"strings"
	"testing"
)

func compactCtx() TaskContextForEnv {
	return TaskContextForEnv{
		IssueID:            "11111111-2222-3333-4444-555555555555",
		OrchestratedBranch: "agent/ox-1",
		AgentName:          "heavy-engineer",
		AgentID:            "a-1",
		AgentInstructions:  "Persona text.",
		AgentSkills:        []SkillContextForEnv{{Name: "handoff-metadata"}, {Name: "multica-platform"}},
		ProjectID:          "p-1",
		ProjectTitle:       "Holt",
		WorkspaceContext:   "Workspace rules.",
	}
}

func TestCompactBriefOnlyForOrchestratedIssues(t *testing.T) {
	t.Parallel()
	ctx := compactCtx()
	compact := buildMetaSkillContent("yielded", ctx)
	ctx.OrchestratedBranch = ""
	full := buildMetaSkillContent("yielded", ctx)
	if !strings.Contains(compact, "## Orchestrator Contract") || strings.Contains(full, "## Orchestrator Contract") {
		t.Fatal("compact brief must be selected by OrchestratedBranch only")
	}
	if len(compact)*3 > len(full) {
		t.Fatalf("compact brief not compact: %d vs full %d bytes", len(compact), len(full))
	}
	chat := TaskContextForEnv{ChatSessionID: "c-1", OrchestratedBranch: "agent/x"}
	if strings.Contains(buildMetaSkillContent("yielded", chat), "## Orchestrator Contract") {
		t.Fatal("non-issue kinds keep their own brief")
	}
}

func TestCompactBriefCarriesTheEssentials(t *testing.T) {
	t.Parallel()
	got := buildMetaSkillContent("pi", compactCtx())
	for _, want := range []string{
		"**You are: heavy-engineer** (ID: `a-1`)",
		"Persona text.",
		"Workspace rules.",
		"Issue `11111111-2222-3333-4444-555555555555`",
		"multica issue get <id> --output json",
		"multica flow show <id>",
		"orchestrator_owned",
		"phase=review_requested",
		"verdict=approve|changes",
		"phase=blocked",
		"branch `agent/ox-1`",
		"~/.cursor/artifacts/<repo>/",
		"**Holt**",
		"`handoff-metadata`",
		"--content-file",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("compact brief missing %q", want)
		}
	}
	if again := buildMetaSkillContent("pi", compactCtx()); again != got {
		t.Fatal("compact brief must be byte-stable across runs")
	}
}

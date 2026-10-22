package orchestrator

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed preset
var presetFS embed.FS

// PresetRole is one rendered agent instruction: team contract + role file.
// Expert is set on expert roles (one role per expert and agent).
// Warn is a non-fatal context-budget note for per-expert skills.
type PresetRole struct {
	Role, Agent, Expert, Instructions, Warn string
	Skills                                  []string
}

// presetSkills: which preset skills each role gets attached.
var presetSkills = map[string][]string{
	"owner":    {"open-pr", "handoff-metadata", "rebase-conflict", "blocked-report"},
	"expert":   {"open-pr", "handoff-metadata", "rebase-conflict", "blocked-report"},                         // implement mode uses the owner's handoff skills
	"reviewer": {"verify-gates", "review-verdict", "resolve-conflict", "handoff-metadata", "blocked-report"}, // review-verdict step 8 writes metadata via handoff-metadata; resolve-conflict for phase=resolving_conflict
	"lead":     {"plan-split", "blocked-report"},
}

// RenderPreset fills the preset's role instructions from a spec. Placeholders
// left as {{KEY}} are filled per issue by the agent itself.
func RenderPreset(sp *Spec, workspace string) ([]PresetRole, error) {
	name := sp.Preset
	if name == "" {
		name = "impl-review-merge"
	}
	base := path.Join("preset", name)
	team, err := fs.ReadFile(presetFS, path.Join(base, "team.md"))
	if err != nil {
		return nil, fmt.Errorf("preset %q not found", name)
	}
	ps, _, err := loadPstack(base)
	if err != nil {
		return nil, err
	}
	repl := strings.NewReplacer("{{MAIN}}", sp.Repo.Main, "{{CHECK_CMD}}", orDefault(sp.Repo.CheckCmd, "the repo's tests"),
		"{{REPO}}", repoLabel(sp), "{{WORKSPACE}}", workspace, "{{KEY}}", "KEY")
	role := func(r, agent string) (PresetRole, error) {
		b, err := fs.ReadFile(presetFS, path.Join(base, r+".md"))
		if err != nil {
			return PresetRole{}, err
		}
		skills := append([]string{}, presetSkills[r]...)
		if ps != nil {
			skills = append(skills, ps.roleSkills(r)...)
		}
		return PresetRole{Role: r, Agent: agent, Skills: skills, Instructions: repl.Replace(string(b) + "\n" + string(team))}, nil
	}
	var out []PresetRole
	add := func(r, agent string) error {
		if agent == "" {
			return nil
		}
		x, err := role(r, agent)
		if err == nil {
			out = append(out, x)
		}
		return err
	}
	if err := add("lead", sp.Roles.Lead); err != nil {
		return nil, err
	}
	if len(out) > 0 && out[0].Role == "lead" {
		out[0].Instructions += routingSection(sp)
	}
	for _, o := range sp.Roles.Owners {
		if err := add("owner", o); err != nil {
			return nil, err
		}
	}
	for _, e := range sp.Experts {
		mode := e.Mode
		if mode == "" {
			mode = "implement"
		}
		for _, agent := range e.Agents {
			x, err := role("expert", agent)
			if err != nil {
				return nil, err
			}
			x.Expert = e.Name
			x.Skills = uniqAppend(x.Skills, e.Skills...)
			x.Instructions += fmt.Sprintf("\n## This expert\n\nExpert: %s. Mode: %s.\n", e.Name, mode)
			if strings.TrimSpace(e.Instructions) != "" {
				x.Instructions += "\n" + strings.TrimSpace(e.Instructions) + "\n"
			}
			x.Warn = warnExpertBudget(x.Skills, e.Name, agent)
			out = append(out, x)
		}
	}
	if err := add("reviewer", sp.Roles.Reviewer); err != nil {
		return nil, err
	}
	return out, nil
}

func uniqAppend(base []string, more ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(base)+len(more))
	for _, s := range append(append([]string{}, base...), more...) {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// warnExpertBudget counts per-expert skills from project.yaml on top of the
// expert role's mounted set. Over budget warns; render still succeeds.
func warnExpertBudget(skills []string, expertName, agent string) string {
	n, base, b, err := RoleContextBytes("", "expert")
	if err != nil || b.BudgetBytes <= 0 {
		return ""
	}
	have := map[string]bool{}
	for _, s := range base {
		have[s] = true
	}
	files, err := PresetFiles("")
	if err != nil {
		return ""
	}
	extra := 0
	for _, s := range skills {
		if have[s] {
			continue
		}
		body := files["pstack/skills/"+s+"/SKILL.md"]
		if body == "" {
			body = files["skills/"+s+"/SKILL.md"]
		}
		extra += len(body)
	}
	if n+extra > b.BudgetBytes {
		return fmt.Sprintf("expert %s (%s): skill context %d bytes > budget %d", expertName, agent, n+extra, b.BudgetBytes)
	}
	return ""
}

func routingSection(sp *Spec) string {
	var b strings.Builder
	b.WriteString("\n\n## Routing\n\nRouting (first match wins):\n")
	if len(sp.Experts) > 0 {
		parts := make([]string, 0, len(sp.Experts))
		for _, e := range sp.Experts {
			mode := e.Mode
			if mode == "" {
				mode = "implement"
			}
			parts = append(parts, fmt.Sprintf("%s ← %s (%s, %s)", e.Name, expertTrigger(e.When), strings.Join(e.Agents, ", "), mode))
		}
		b.WriteString("- experts: " + strings.Join(parts, "; ") + "\n")
	}
	if len(sp.Roles.Routes) > 0 {
		parts := make([]string, 0, len(sp.Roles.Routes))
		for _, r := range sp.Roles.Routes {
			parts = append(parts, strings.Join(r.Labels, ", ")+" → "+strings.Join(r.Owners, ", "))
		}
		b.WriteString("- routes: " + strings.Join(parts, "; ") + "\n")
	}
	b.WriteString("- otherwise: " + strings.Join(sp.Roles.Owners, ", ") + "\n")
	var tail []string
	if len(sp.Limits.Slots) > 0 {
		keys := make([]string, 0, len(sp.Limits.Slots))
		for k := range sp.Limits.Slots {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		slots := make([]string, 0, len(keys))
		for _, k := range keys {
			slots = append(slots, fmt.Sprintf("needs:%s (%d)", k, sp.Limits.Slots[k]))
		}
		tail = append(tail, "Slots: "+strings.Join(slots, ", "))
	}
	if len(sp.Acceptance.Labels) > 0 || len(sp.Acceptance.DefaultForLabels) > 0 {
		line := "Acceptance: " + strings.Join(sp.Acceptance.Labels, ", ")
		if len(sp.Acceptance.DefaultForLabels) > 0 {
			line += "; default for " + strings.Join(sp.Acceptance.DefaultForLabels, ", ")
		}
		tail = append(tail, line)
	}
	if len(tail) > 0 {
		b.WriteString(strings.Join(tail, ". ") + ".\n")
	}
	return b.String()
}

func expertTrigger(w ExpertWhen) string {
	var parts []string
	if len(w.Labels) > 0 {
		parts = append(parts, strings.Join(w.Labels, ", "))
	}
	if len(w.NotLabels) > 0 {
		parts = append(parts, "not "+strings.Join(w.NotLabels, ", "))
	}
	if len(w.Metadata) > 0 {
		keys := make([]string, 0, len(w.Metadata))
		for k := range w.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("meta %s=%s", k, w.Metadata[k]))
		}
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " ")
}

// PresetFiles returns every file of the preset (docs + skills), for export.
func PresetFiles(name string) (map[string]string, error) {
	if name == "" {
		name = "impl-review-merge"
	}
	base := path.Join("preset", name)
	ps, contract, err := loadPstack(base)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	err = fs.WalkDir(presetFS, base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(presetFS, p)
		rel, body := strings.TrimPrefix(p, base+"/"), string(b)
		if ps != nil {
			var ok bool
			if body, ok = ps.adaptFile(rel, body, contract); !ok {
				return err
			}
		}
		out[rel] = body
		return err
	})
	return out, err
}

// PresetNames lists embedded presets.
func PresetNames() []string {
	es, _ := fs.ReadDir(presetFS, "preset")
	var n []string
	for _, e := range es {
		n = append(n, e.Name())
	}
	sort.Strings(n)
	return n
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func repoLabel(sp *Spec) string {
	if sp.Repo.Mode == "github" {
		return "GitHub " + sp.Repo.Slug
	}
	return "the local remote"
}

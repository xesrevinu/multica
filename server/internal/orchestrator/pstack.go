package orchestrator

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// pstackEntry is one vendored pstack skill/agent and how the orchestrator uses it.
type pstackEntry struct {
	Action string   `yaml:"action"` // keep | adapt | operator | drop
	Roles  []string `yaml:"roles"`  // agents only: the role whose lens it is
	Note   string   `yaml:"note"`
}

// pstackRole is one role's mounted skill set and its context budget.
type pstackRole struct {
	BudgetBytes int      `yaml:"budget_bytes"`
	MaxSkills   int      `yaml:"max_skills"`
	Skills      []string `yaml:"skills"`
}

type pstackManifest struct {
	Skills map[string]pstackEntry `yaml:"skills"`
	Agents map[string]pstackEntry `yaml:"agents"`
	Roles  map[string]pstackRole  `yaml:"roles"`
}

func loadPstack(base string) (*pstackManifest, string, error) {
	b, err := fs.ReadFile(presetFS, path.Join(base, "pstack", "manifest.yaml"))
	if err != nil {
		return nil, "", nil // preset without pstack
	}
	var m pstackManifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, "", fmt.Errorf("pstack manifest: %w", err)
	}
	c, err := fs.ReadFile(presetFS, path.Join(base, "pstack", "CONTRACT.md"))
	return &m, string(c), err
}

// roleSkills: the pstack skills mounted for a role (manifest `roles:`), in
// manifest order. Everything else stays an unattached library.
func (m *pstackManifest) roleSkills(role string) []string {
	var out []string
	for _, n := range m.Roles[role].Skills {
		if e := m.Skills[n]; e.Action == "keep" || e.Action == "adapt" {
			out = append(out, n)
		}
	}
	return out
}

// adaptPstackFile maps a vendored file to its exported form; ok=false skips it.
func (m *pstackManifest) adaptFile(rel, body, contract string) (string, bool) {
	parts := strings.Split(rel, "/") // pstack/skills/<name>/...
	if len(parts) < 3 || parts[0] != "pstack" {
		return body, true
	}
	var e pstackEntry
	switch parts[1] {
	case "skills":
		e = m.Skills[parts[2]]
	case "agents":
		e = m.Agents[strings.TrimSuffix(parts[2], ".md")]
	default:
		return body, true
	}
	if e.Action == "drop" {
		return "", false
	}
	isDoc := (parts[1] == "skills" && len(parts) == 4 && parts[3] == "SKILL.md") || parts[1] == "agents"
	if !isDoc {
		return body, true
	}
	var b strings.Builder
	b.WriteString(body)
	b.WriteString("\n\n---\n")
	if e.Action == "operator" {
		b.WriteString("\n> Multica: operator-only. Not attached to orchestrator agents. " + e.Note + "\n")
		return b.String(), true
	}
	if e.Note != "" {
		b.WriteString("\n> Multica adaptation (overrides the text above): " + e.Note + "\n")
	}
	b.WriteString("\n" + contract)
	return b.String(), true
}

// RoleContextBytes is a role's prompt/context footprint when every mounted
// skill is loaded: role file + team contract + each attached preset and
// pstack SKILL.md as exported. The manifest's budget_bytes caps it.
func RoleContextBytes(preset, role string) (bytes int, skills []string, budget pstackRole, err error) {
	if preset == "" {
		preset = "impl-review-merge"
	}
	base := path.Join("preset", preset)
	files, err := PresetFiles(preset)
	if err != nil {
		return 0, nil, budget, err
	}
	m, _, err := loadPstack(base)
	if err != nil || m == nil {
		return 0, nil, budget, err
	}
	budget = m.Roles[role]
	bytes = len(files[role+".md"]) + len(files["team.md"])
	for _, n := range presetSkills[role] {
		bytes += len(files["skills/"+n+"/SKILL.md"])
		skills = append(skills, n)
	}
	for _, n := range m.roleSkills(role) {
		bytes += len(files["pstack/skills/"+n+"/SKILL.md"])
		skills = append(skills, n)
	}
	return bytes, skills, budget, nil
}

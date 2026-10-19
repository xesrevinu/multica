package orchestrator

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// pstackEntry is one vendored pstack skill/agent and how the orchestrator uses it.
type pstackEntry struct {
	Action string   `yaml:"action"` // keep | adapt | operator | drop
	Roles  []string `yaml:"roles"`
	Note   string   `yaml:"note"`
}

type pstackManifest struct {
	Skills map[string]pstackEntry `yaml:"skills"`
	Agents map[string]pstackEntry `yaml:"agents"`
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

// pstackRoleSkills: skills attached to a role (agents act as reviewer lenses, not skills).
func (m *pstackManifest) roleSkills(role string) []string {
	var out []string
	for n, e := range m.Skills {
		if e.Action == "drop" || e.Action == "operator" {
			continue
		}
		for _, r := range e.Roles {
			if r == role {
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
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

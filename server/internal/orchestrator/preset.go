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
type PresetRole struct {
	Role, Agent, Instructions string
	Skills                    []string
}

// presetSkills: which preset skills each role gets attached.
var presetSkills = map[string][]string{
	"owner":    {"open-pr", "handoff-metadata", "rebase-conflict", "blocked-report"},
	"reviewer": {"verify-gates", "review-verdict", "blocked-report"},
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
	repl := strings.NewReplacer("{{MAIN}}", sp.Repo.Main, "{{CHECK_CMD}}", orDefault(sp.Repo.CheckCmd, "the repo's tests"),
		"{{REPO}}", repoLabel(sp), "{{WORKSPACE}}", workspace, "{{KEY}}", "KEY")
	role := func(r, agent string) (PresetRole, error) {
		b, err := fs.ReadFile(presetFS, path.Join(base, r+".md"))
		if err != nil {
			return PresetRole{}, err
		}
		return PresetRole{Role: r, Agent: agent, Skills: presetSkills[r], Instructions: repl.Replace(string(b) + "\n" + string(team))}, nil
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
	for _, o := range sp.Roles.Owners {
		if err := add("owner", o); err != nil {
			return nil, err
		}
	}
	if err := add("reviewer", sp.Roles.Reviewer); err != nil {
		return nil, err
	}
	return out, nil
}

// PresetFiles returns every file of the preset (docs + skills), for export.
func PresetFiles(name string) (map[string]string, error) {
	if name == "" {
		name = "impl-review-merge"
	}
	base := path.Join("preset", name)
	out := map[string]string{}
	err := fs.WalkDir(presetFS, base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(presetFS, p)
		out[strings.TrimPrefix(p, base+"/")] = string(b)
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

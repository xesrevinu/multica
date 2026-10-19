package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/orchestrator"
)

// `multica team preset …` renders the embedded role preset (team contract,
// role instructions, skills) so agents get the same text the docs describe.

func runPresetRender(cmd *cobra.Command, _ []string) error {
	raw, err := readSpec(cmd)
	if err != nil {
		return err
	}
	sp, err := orchestrator.ParseSpec([]byte(raw))
	if err != nil {
		return err
	}
	ws, _ := cmd.Flags().GetString("workspace-name")
	roles, err := orchestrator.RenderPreset(sp, ws)
	if err != nil {
		return err
	}
	out, _ := cmd.Flags().GetString("out")
	if out == "" {
		for _, r := range roles {
			fmt.Fprintf(cmd.OutOrStdout(), "===== %s (%s) skills: %s\n%s\n", r.Agent, r.Role, strings.Join(r.Skills, ", "), r.Instructions)
		}
		return nil
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, r := range roles {
		p := filepath.Join(out, r.Agent+".md")
		if err := os.WriteFile(p, []byte(r.Instructions), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tskills: %s\n", p, r.Role, strings.Join(r.Skills, ", "))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "apply: multica agent update <id> --instructions \"$(cat <file>)\"; skills: multica team preset export <dir> then multica skill create/update --content-file <dir>/skills/<name>/SKILL.md\n")
	return nil
}

func runPresetExport(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("preset")
	files, err := orchestrator.PresetFiles(name)
	if err != nil {
		return err
	}
	for rel, body := range files {
		p := filepath.Join(args[0], rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), p)
	}
	return nil
}

func init() {
	preset := &cobra.Command{Use: "preset", Short: "Role preset: team contract, role instructions and skills (" + strings.Join(orchestrator.PresetNames(), ", ") + ")"}
	render := &cobra.Command{Use: "render", Short: "Render each role's agent instructions from a team spec", Args: cobra.NoArgs, RunE: runPresetRender}
	render.Flags().StringP("file", "f", "", "spec file (project.yaml with orchestrator: section, or a bare spec)")
	render.Flags().String("out", "", "write <agent>.md files into this directory instead of stdout")
	render.Flags().String("workspace-name", "", "workspace name used in the text")
	export := &cobra.Command{Use: "export <dir>", Short: "Write the preset's docs and skills to a directory", Args: cobra.ExactArgs(1), RunE: runPresetExport}
	export.Flags().String("preset", "impl-review-merge", "preset name")
	preset.AddCommand(render, export)
	teamCmd.AddCommand(preset)
}

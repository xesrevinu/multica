package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// `multica team …` manages a project's orchestrator team spec;
// `multica flow …` inspects and steers one orchestrated issue.

var teamCmd = &cobra.Command{Use: "team", Short: "Manage the orchestrator team of a project (apply/diff/pause/resume/status)"}
var flowCmd = &cobra.Command{Use: "flow", Short: "Inspect and steer an orchestrated issue (show/log/unblock/cancel/accept/reject/replay)"}

func orchClient(cmd *cobra.Command) (*cli.APIClient, context.Context, context.CancelFunc, error) {
	c, err := newAPIClient(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	return c, ctx, cancel, nil
}

func printOut(cmd *cobra.Command, v any, human func()) error {
	if o, _ := cmd.Flags().GetString("output"); o == "json" || human == nil {
		return cli.PrintJSON(os.Stdout, v)
	}
	human()
	return nil
}

func readSpec(cmd *cobra.Command) (string, error) {
	f, _ := cmd.Flags().GetString("file")
	if f == "" {
		return "", fmt.Errorf("--file is required (YAML with an orchestrator: section or a bare spec)")
	}
	b, err := os.ReadFile(f)
	return string(b), err
}

func teamApplyOrDiff(apply bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		spec, err := readSpec(cmd)
		if err != nil {
			return err
		}
		c, ctx, cancel, err := orchClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		human, _ := cmd.Flags().GetString("human")
		body := map[string]any{"project": args[0], "spec_yaml": spec, "human": human}
		var out map[string]any
		if apply {
			err = c.PutJSON(ctx, "/api/orchestrator/teams", body, &out)
		} else {
			err = c.PostJSON(ctx, "/api/orchestrator/teams/diff", body, &out)
		}
		if err != nil {
			return err
		}
		return printOut(cmd, out, func() {
			ch, _ := out["changes"].([]any)
			if len(ch) == 0 {
				fmt.Println("no changes")
			}
			for _, x := range ch {
				fmt.Println("~", x)
			}
			if apply {
				fmt.Println("applied to project", out["project_id"])
			}
		})
	}
}

func teamPause(pause bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		c, ctx, cancel, err := orchClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		reason, _ := cmd.Flags().GetString("reason")
		verb := "resume"
		if pause {
			verb = "pause"
		}
		var out map[string]any
		if err := c.PostJSON(ctx, "/api/orchestrator/teams/"+url.PathEscape(args[0])+"/"+verb, map[string]any{"reason": reason}, &out); err != nil {
			return err
		}
		return printOut(cmd, out, func() { fmt.Printf("%sd %s\n", verb, args[0]) })
	}
}

func runTeamStatus(cmd *cobra.Command, _ []string) error {
	c, ctx, cancel, err := orchClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var st, teams map[string]any
	if err := c.GetJSON(ctx, "/api/orchestrator/status", &st); err != nil {
		return err
	}
	if err := c.GetJSON(ctx, "/api/orchestrator/teams", &teams); err != nil {
		return err
	}
	st["teams"] = teams["teams"]
	return cli.PrintJSON(os.Stdout, st)
}

func runFlowShow(cmd *cobra.Command, args []string) error {
	c, ctx, cancel, err := orchClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out map[string]any
	if err := c.GetJSON(ctx, "/api/orchestrator/flow/"+url.PathEscape(args[0]), &out); err != nil {
		return err
	}
	if d, _ := cmd.Flags().GetBool("diagram"); d {
		fmt.Print(out["diagram"])
		return nil
	}
	return printOut(cmd, out, func() {
		fmt.Printf("%s  %s\n  status: %v   assignee: %v (%v)\n", out["key"], out["title"], out["status"], out["assignee"], out["assignee_type"])
		meta, _ := json.Marshal(out["metadata"])
		fmt.Printf("  metadata: %s\n", meta)
		if p, _ := out["paused"].(string); p != "" {
			fmt.Println("  PAUSED:", p)
		}
		if out["needs_acceptance"] == true {
			fmt.Println("  gated: human acceptance required before merge")
		}
		if n, _ := out["failures"].(float64); n > 0 {
			fmt.Printf("  failures: %v (backoff until %v) last error: %v\n", n, out["backoff_until"], out["last_error"])
		}
		if w, _ := out["waiting"].(string); w != "" {
			fmt.Println("  waiting:", w)
		}
		if nx, ok := out["next"].(map[string]any); ok && nx != nil {
			b, _ := json.Marshal(nx)
			fmt.Println("  next (dry, without repo probes):", string(b))
		} else {
			fmt.Println("  next: nothing to do until an agent/member acts")
		}
		fmt.Println("  recent transitions:")
		printTransitions(out["recent"])
	})
}

func printTransitions(v any) {
	l, _ := v.([]any)
	for i := len(l) - 1; i >= 0; i-- {
		e, _ := l[i].(map[string]any)
		d, _ := e["decision"].(map[string]any)
		extra := ""
		if w, _ := d["why"].(string); w != "" {
			extra = " — " + w
		}
		if er, _ := e["error"].(string); er != "" {
			extra += " ERROR: " + er
		}
		fmt.Printf("    %v  %-22v %-7v %v→%v %v%s\n", e["created_at"], e["tid"], e["state"], strOr(d["status"], "·"), strOr(d["assignee"], ""), e["source"], extra)
	}
}

func strOr(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func runFlowLog(cmd *cobra.Command, args []string) error {
	c, ctx, cancel, err := orchClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out map[string]any
	if err := c.GetJSON(ctx, "/api/orchestrator/flow/"+url.PathEscape(args[0])+"/log", &out); err != nil {
		return err
	}
	return printOut(cmd, out, func() { printTransitions(out["transitions"]) })
}

func flowAction(action string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		c, ctx, cancel, err := orchClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		body := map[string]any{}
		if f := cmd.Flags().Lookup("reason"); f != nil {
			body["reason"] = f.Value.String()
		}
		if f := cmd.Flags().Lookup("owner"); f != nil {
			body["owner"] = f.Value.String()
		}
		var out map[string]any
		if err := c.PostJSON(ctx, "/api/orchestrator/flow/"+url.PathEscape(args[0])+"/"+action, body, &out); err != nil {
			return err
		}
		return printOut(cmd, out, func() {
			b, _ := json.Marshal(out)
			fmt.Printf("%s %s: %s\n", action, args[0], strings.TrimSpace(string(b)))
		})
	}
}

func init() {
	apply := &cobra.Command{Use: "apply <project>", Short: "Create or update the project's team from a YAML spec", Args: cobra.ExactArgs(1), RunE: teamApplyOrDiff(true)}
	diff := &cobra.Command{Use: "diff <project>", Short: "Validate a spec and show what apply would change", Args: cobra.ExactArgs(1), RunE: teamApplyOrDiff(false)}
	for _, c := range []*cobra.Command{apply, diff} {
		c.Flags().StringP("file", "f", "", "spec file (project.yaml with orchestrator: section, or a bare spec)")
		c.Flags().String("human", "", "member (email/name/id) for acceptance and alerts; default: you")
	}
	pause := &cobra.Command{Use: "pause <project>", Short: "Kill switch: stop all orchestrator transitions for the project", Args: cobra.ExactArgs(1), RunE: teamPause(true)}
	pause.Flags().String("reason", "", "why")
	resume := &cobra.Command{Use: "resume <project>", Short: "Resume the orchestrator for the project (runs a reconcile)", Args: cobra.ExactArgs(1), RunE: teamPause(false)}
	status := &cobra.Command{Use: "status", Short: "Orchestrator heartbeat and teams of this workspace", Args: cobra.NoArgs, RunE: runTeamStatus}
	teamCmd.AddCommand(apply, diff, pause, resume, status)

	show := &cobra.Command{Use: "show <issue>", Short: "Show an orchestrated issue: state, next decision, recent transitions", Args: cobra.ExactArgs(1), RunE: runFlowShow}
	show.Flags().Bool("diagram", false, "print the mermaid state diagram with the current status highlighted")
	logc := &cobra.Command{Use: "log <issue>", Short: "Full transition log of an issue", Args: cobra.ExactArgs(1), RunE: runFlowLog}
	unblock := &cobra.Command{Use: "unblock <issue>", Short: "Send a blocked issue back to its owner (counters reset)", Args: cobra.ExactArgs(1), RunE: flowAction("unblock")}
	unblock.Flags().String("reason", "", "note for the owner")
	unblock.Flags().String("owner", "", "reassign to another owner agent")
	cancelc := &cobra.Command{Use: "cancel <issue>", Short: "Cancel a blocked issue", Args: cobra.ExactArgs(1), RunE: flowAction("cancel")}
	accept := &cobra.Command{Use: "accept <issue>", Short: "Human acceptance: approve a gated issue (it is then merged)", Args: cobra.ExactArgs(1), RunE: flowAction("accept")}
	reject := &cobra.Command{Use: "reject <issue>", Short: "Human acceptance: reject a gated issue back to its owner", Args: cobra.ExactArgs(1), RunE: flowAction("reject")}
	reject.Flags().String("reason", "", "what is wrong (required; the owner reads it)")
	replay := &cobra.Command{Use: "replay <issue>", Short: "Clear backoff and re-evaluate the issue now", Args: cobra.ExactArgs(1), RunE: flowAction("replay")}
	flowCmd.AddCommand(show, logc, unblock, cancelc, accept, reject, replay)
	for _, c := range append(teamCmd.Commands(), flowCmd.Commands()...) {
		if c.Flags().Lookup("output") == nil {
			c.Flags().String("output", "table", "output format: table or json")
		}
	}
}

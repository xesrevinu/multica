package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The reviewer must not post the review-verdict status (ATT-72: it did).
// Instructions forbid it, and the review-verdict skill ships the only GitHub
// write a reviewer needs, a script that can post a PR review comment and
// nothing else.
func TestReviewerCannotPostStatusesByInstruction(t *testing.T) {
	sp := &Spec{Preset: "impl-review-merge"}
	sp.Roles.Reviewer = "rev"
	sp.Repo.Mode, sp.Repo.Slug, sp.Repo.Main = "github", "o/r", "main"
	roles, err := RenderPreset(sp, "ws")
	if err != nil {
		t.Fatal(err)
	}
	var rev string
	for _, r := range roles {
		if r.Role == "reviewer" {
			rev = r.Instructions
		}
	}
	files, err := PresetFiles("impl-review-merge")
	if err != nil {
		t.Fatal(err)
	}
	skill := files["skills/review-verdict/SKILL.md"]
	for name, text := range map[string]string{"reviewer.md": rev, "review-verdict SKILL.md": skill} {
		for _, want := range []string{"pr-review-comment.sh", "gh api .../statuses/...", "check-runs", "--request-changes"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not mention %q", name, want)
			}
		}
	}
	if !strings.Contains(skill, "This skill's name is not a status") {
		t.Error("SKILL.md must separate the skill name from the status context")
	}
}

func TestReviewCommentScriptOnlyComments(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	files, err := PresetFiles("impl-review-merge")
	if err != nil {
		t.Fatal(err)
	}
	body, ok := files["skills/review-verdict/scripts/pr-review-comment.sh"]
	if !ok {
		t.Fatal("review-verdict skill must export scripts/pr-review-comment.sh")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "pr-review-comment.sh")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(script, []byte(body), 0o755))
	bin := filepath.Join(dir, "bin")
	must(os.Mkdir(bin, 0o755))
	log := filepath.Join(dir, "gh.log")
	must(os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\necho \"$@\" >> "+log+"\n"), 0o755))
	comment := filepath.Join(dir, "c.md")
	must(os.WriteFile(comment, []byte("1 important\nverdict: changes @ abc\n"), 0o644))
	empty := filepath.Join(dir, "e.md")
	must(os.WriteFile(empty, nil, 0o644))
	run := func(args ...string) (int, string) {
		cmd := exec.Command("bash", append([]string{script}, args...)...)
		cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), string(out)
		}
		return 0, string(out)
	}
	if code, out := run("9", comment); code != 0 {
		t.Fatalf("valid comment: %d %s", code, out)
	}
	for _, args := range [][]string{
		{"9"},
		{"9", comment, "--approve"},
		{"repos/o/r/statuses/abc", comment},
		{"9;gh", comment},
		{"9", empty},
	} {
		if code, out := run(args...); code != 2 || !strings.Contains(out, "refused") {
			t.Errorf("%v: want refusal, got %d %s", args, code, out)
		}
	}
	got, _ := os.ReadFile(log)
	if strings.TrimSpace(string(got)) != "pr review 9 --comment --body-file "+comment {
		t.Fatalf("gh calls:\n%s", got)
	}
}

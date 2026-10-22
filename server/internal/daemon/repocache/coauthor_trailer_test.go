package repocache

import (
	"os"
	"strings"
	"testing"
)

func TestCoAuthorTrailerDefaults(t *testing.T) {
	t.Setenv("MULTICA_CO_AUTHOR_NAME", "")
	t.Setenv("MULTICA_CO_AUTHOR_EMAIL", "")
	hook := prepareCommitMsgHook("")
	if !strings.Contains(hook, `TRAILER="Co-authored-by: multica-agent <github@multica.ai>"`) {
		t.Fatalf("default trailer missing:\n%s", hook)
	}
}

func TestCoAuthorTrailerUsesDeploymentOverride(t *testing.T) {
	t.Setenv("MULTICA_CO_AUTHOR_NAME", "woolpup[bot]")
	t.Setenv("MULTICA_CO_AUTHOR_EMAIL", "187665180+woolpup[bot]@users.noreply.github.com")
	t.Setenv("MULTICA_AGENT_GITHUB_APP", "0") // legacy: bot co-author, no App identity
	hook := prepareCommitMsgHook("")
	want := `TRAILER="Co-authored-by: woolpup[bot] <187665180+woolpup[bot]@users.noreply.github.com>"`
	if !strings.Contains(hook, want) {
		t.Fatalf("override trailer missing:\n%s", hook)
	}
}

func TestCoAuthorTrailerRejectsUnsafeOverride(t *testing.T) {
	t.Setenv("MULTICA_CO_AUTHOR_NAME", `bad"name`)
	t.Setenv("MULTICA_CO_AUTHOR_EMAIL", "ok@example.com")
	hook := prepareCommitMsgHook("")
	if !strings.Contains(hook, `TRAILER="Co-authored-by: multica-agent <github@multica.ai>"`) {
		t.Fatalf("unsafe override was written into the hook:\n%s", hook)
	}
}

// With the App identity on and no explicit override, agent commits are
// authored by the bot, so the trailer must credit the human — never the
// multica-agent product default.
func TestCoAuthorTrailerCreditsHumanUnderAppIdentity(t *testing.T) {
	gitconfig := t.TempDir() + "/gitconfig"
	if err := os.WriteFile(gitconfig, []byte("[user]\n\tname = Human Dev\n\temail = human@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	t.Setenv("MULTICA_AGENT_GITHUB_APP", "")
	t.Setenv("MULTICA_CO_AUTHOR_NAME", "")
	t.Setenv("MULTICA_CO_AUTHOR_EMAIL", "")
	hook := prepareCommitMsgHook("")
	if want := `TRAILER="Co-authored-by: Human Dev <human@example.com>"`; !strings.Contains(hook, want) {
		t.Fatalf("human trailer missing:\n%s", hook)
	}
	if strings.Contains(hook, "multica-agent") {
		t.Fatalf("product default leaked into an App-identity hook:\n%s", hook)
	}
}

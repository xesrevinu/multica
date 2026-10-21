package repocache

import (
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

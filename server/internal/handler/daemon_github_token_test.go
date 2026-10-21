package handler

import "testing"

func TestParseGitHubRepo(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/effect-anything/holt.git": "effect-anything/holt",
		"https://github.com/o/r":                      "o/r",
		"git@github.com:o/r.git":                      "o/r",
		"ssh://git@github.com/o/r.git":                "o/r",
	} {
		o, r, ok := parseGitHubRepo(in)
		if !ok || o+"/"+r != want {
			t.Errorf("%s: got %s/%s %v", in, o, r, ok)
		}
	}
	for _, in := range []string{"https://gitlab.com/o/r", "https://github.com/o", "https://github.com/o/r/../x", "https://evil.com/github.com/o/r", ""} {
		if _, _, ok := parseGitHubRepo(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestWorkspaceHasGitHubRepo(t *testing.T) {
	repos := []RepoData{{URL: "https://github.com/Effect-Anything/holt.git"}}
	if !workspaceHasGitHubRepo(repos, "effect-anything", "holt") {
		t.Fatal("own repo refused")
	}
	if workspaceHasGitHubRepo(repos, "effect-anything", "other") {
		t.Fatal("foreign repo allowed")
	}
}

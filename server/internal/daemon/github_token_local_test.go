package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitHubTokenCacheRenewsNearExpiry(t *testing.T) {
	var c githubTokenCache
	now := time.Now()
	c.put("t1", cachedGitHubToken{token: "a", exp: now.Add(time.Hour)})
	if got, ok := c.get("t1", now); !ok || got.token != "a" {
		t.Fatal("fresh token not reused")
	}
	if _, ok := c.get("t1", now.Add(51*time.Minute)); ok {
		t.Fatal("token within renew window must be re-minted")
	}
	if _, ok := c.get("other", now); ok {
		t.Fatal("token leaked across tasks")
	}
}

// The shim swaps in the daemon's fresh token and runs the real gh; when the
// daemon is unreachable it keeps the run's GH_TOKEN.
func TestGhShimRefreshesToken(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	fakeDir := filepath.Join(root, "fake")
	_ = os.MkdirAll(realDir, 0o755)
	_ = os.MkdirAll(fakeDir, 0o755)
	_ = os.WriteFile(filepath.Join(realDir, "gh"), []byte("#!/bin/sh\necho \"tok=$GH_TOKEN args=$*\"\n"), 0o755)
	shim := installGhShim(filepath.Join(root, "shim"), realDir)
	if shim == "" {
		t.Fatal("shim not installed")
	}
	if b, _ := os.ReadFile(filepath.Join(shim, "gh")); strings.Contains(string(b), "ghs_") || strings.Contains(string(b), "GH_TOKEN=start") {
		t.Fatal("shim must not contain a token")
	}
	run := func(path string) string {
		cmd := exec.Command(filepath.Join(shim, "gh"), "api", "user")
		cmd.Env = []string{"PATH=" + path + ":/usr/bin:/bin", "GH_TOKEN=start"}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	_ = os.WriteFile(filepath.Join(fakeDir, "multica"), []byte("#!/bin/sh\necho fresh\n"), 0o755)
	if got := run(fakeDir); got != "tok=fresh args=api user" {
		t.Fatalf("refresh: %q", got)
	}
	_ = os.WriteFile(filepath.Join(fakeDir, "multica"), []byte("#!/bin/sh\nexit 1\n"), 0o755)
	if got := run(fakeDir); got != "tok=start args=api user" {
		t.Fatalf("fallback: %q", got)
	}
}

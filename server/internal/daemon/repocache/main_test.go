package repocache

import (
	"os"
	"testing"
)

// TestMain pins the GitHub App identity off: with it on, the co-author trailer
// credits the developer's global git identity, so the default-trailer
// assertions would depend on whoever runs the suite. A test that covers the
// App path sets MULTICA_AGENT_GITHUB_APP itself.
func TestMain(m *testing.M) {
	os.Setenv("MULTICA_AGENT_GITHUB_APP", "0")
	os.Exit(m.Run())
}

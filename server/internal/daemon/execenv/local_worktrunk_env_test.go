package execenv

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktrunkEnvAppendsUserBinDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	local := filepath.Join(home, ".local", "bin")

	got := worktrunkEnv([]string{"FOO=1", "PATH=/usr/bin:/bin"})
	var path string
	for _, kv := range got {
		if strings.HasPrefix(kv, "PATH=") {
			path = strings.TrimPrefix(kv, "PATH=")
		}
	}
	parts := filepath.SplitList(path)
	if len(parts) < 3 || parts[0] != "/usr/bin" || parts[1] != "/bin" || parts[2] != local {
		t.Fatalf("PATH = %q, want /usr/bin:/bin then %s appended", path, local)
	}
	if got[0] != "FOO=1" {
		t.Fatalf("other env changed: %v", got)
	}

	again := worktrunkEnv([]string{"PATH=" + path})
	if again[0] != "PATH="+path {
		t.Fatalf("not idempotent: %q", again[0])
	}

	noPath := worktrunkEnv(nil)
	if len(noPath) != 1 || !strings.Contains(noPath[0], local) {
		t.Fatalf("missing PATH not filled: %v", noPath)
	}
}

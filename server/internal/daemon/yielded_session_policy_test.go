package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestYieldedFreshSessionReason(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.sqlite")
	if err := os.WriteFile(path, make([]byte, 1024), 0o600); err != nil {
		t.Fatal(err)
	}
	fp := skillsFingerprint([]execenv.SkillContextForEnv{{Name: "a", Content: "x"}})
	if r := yieldedFreshSessionReason(path, fp); r != "" {
		t.Fatalf("no marker, small: want resume, got %q", r)
	}
	if err := os.WriteFile(path+yieldedSkillsMarkerSuffix, []byte(fp+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := yieldedFreshSessionReason(path, fp); r != "" {
		t.Fatalf("same skills: got %q", r)
	}
	fp2 := skillsFingerprint([]execenv.SkillContextForEnv{{Name: "a", Content: "y"}})
	if r := yieldedFreshSessionReason(path, fp2); r != "skills_changed" {
		t.Fatalf("changed skills: got %q", r)
	}
	old := yieldedResumeMaxBytes
	yieldedResumeMaxBytes = 2000
	defer func() { yieldedResumeMaxBytes = old }()
	if err := os.WriteFile(path+"-wal", make([]byte, 1500), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := yieldedFreshSessionReason(path, fp); r != "session_too_large" {
		t.Fatalf("large (sqlite+wal): got %q", r)
	}
}

func TestSkillsFingerprintOrderIndependent(t *testing.T) {
	a := []execenv.SkillContextForEnv{{Name: "a", Content: "1"}, {Name: "b", Content: "2", Files: []execenv.SkillFileContextForEnv{{Path: "x", Content: "q"}, {Path: "w", Content: "r"}}}}
	b := []execenv.SkillContextForEnv{{Name: "b", Content: "2", Files: []execenv.SkillFileContextForEnv{{Path: "w", Content: "r"}, {Path: "x", Content: "q"}}}, {Name: "a", Content: "1"}}
	if skillsFingerprint(a) != skillsFingerprint(b) {
		t.Fatal("fingerprint depends on order")
	}
}

package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// yieldedResumeMaxBytes caps the on-disk size (sqlite + WAL) of a yielded
// session the daemon will resume. Resuming keeps the provider prefix cache warm,
// which is worth a lot within one line of work, but a long session carries
// hundreds of KB of reasoning and tool-call history into every later turn, and
// past a few MB yielded's history persistence can fail the whole turn at commit
// (OX-145, 2026-10-21: a 2.3 MB session failed with "Retained model context
// exceeds persistence bounds after elision"). Above this, start fresh: an
// orchestrated run's state lives in the issue, its comments and the branch.
var yieldedResumeMaxBytes int64 = 3 << 20

// yieldedSkillsMarkerSuffix names the sibling file recording the skills
// fingerprint a yielded session last ran with.
const yieldedSkillsMarkerSuffix = ".multica-skills"

// skillsFingerprint hashes the skills a run mounts (names, content, files), so
// a session that learned a workflow from an older skill text is not resumed
// after that skill changed: the remembered plan otherwise beats the new text
// (the OX-145 re-review kept the old scratch-checkout recipe).
func skillsFingerprint(skills []execenv.SkillContextForEnv) string {
	parts := make([]string, 0, len(skills))
	for _, s := range skills {
		var b strings.Builder
		b.WriteString(s.Name)
		b.WriteByte(0)
		b.WriteString(s.Content)
		files := append([]execenv.SkillFileContextForEnv(nil), s.Files...)
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		for _, f := range files {
			b.WriteByte(0)
			b.WriteString(f.Path)
			b.WriteByte(0)
			b.WriteString(f.Content)
		}
		parts = append(parts, b.String())
	}
	sort.Strings(parts)
	h := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return hex.EncodeToString(h[:])
}

func yieldedSessionDiskBytes(path string) int64 {
	var n int64
	for _, p := range []string{path, path + "-wal"} {
		if st, err := os.Stat(p); err == nil {
			n += st.Size()
		}
	}
	return n
}

// yieldedFreshSessionReason reports why a yielded resume should start fresh
// instead ("" = resume). Missing marker = unknown skills: resume (no churn for
// sessions made before this policy).
func yieldedFreshSessionReason(sessionPath, fingerprint string) string {
	if yieldedSessionDiskBytes(sessionPath) > yieldedResumeMaxBytes {
		return "session_too_large"
	}
	if raw, err := os.ReadFile(sessionPath + yieldedSkillsMarkerSuffix); err == nil {
		if strings.TrimSpace(string(raw)) != fingerprint {
			return "skills_changed"
		}
	}
	return ""
}

// applyYieldedSessionPolicy drops task.PriorSessionID for a yielded run when
// the policy says fresh. It is a deliberate restart, so no "context lost"
// notice: the issue, comments and branch carry the state.
func applyYieldedSessionPolicy(task *Task, taskCtx *execenv.TaskContextForEnv, provider string, taskLog *slog.Logger) {
	if provider != "yielded" || strings.TrimSpace(task.PriorSessionID) == "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	path := yieldedSessionPath(home, task.PriorSessionID)
	reason := yieldedFreshSessionReason(path, skillsFingerprint(taskCtx.AgentSkills))
	if reason == "" {
		return
	}
	if taskLog != nil {
		taskLog.Info("starting a fresh yielded session instead of resuming",
			"session_id", task.PriorSessionID, "reason", reason,
			"session_bytes", yieldedSessionDiskBytes(path))
	}
	task.PriorSessionID = ""
	taskCtx.PriorSessionResumed = false
}

// recordYieldedSessionSkills stamps the session the run ended with.
func recordYieldedSessionSkills(provider, sessionID string, skills []execenv.SkillContextForEnv) {
	if provider != "yielded" || strings.TrimSpace(sessionID) == "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	path := yieldedSessionPath(home, sessionID)
	if _, err := os.Stat(path); err != nil {
		return
	}
	_ = os.WriteFile(path+yieldedSkillsMarkerSuffix, []byte(skillsFingerprint(skills)+"\n"), 0o600)
}

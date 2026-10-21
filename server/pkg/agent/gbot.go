package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func chooseDirectInvocation(_, lookedUp string, args []string, _ *slog.Logger) (string, []string) {
	return lookedUp, args
}

// gbotBackend runs one Multica task through grok-bot-cli. The Grok Bot gateway
// has no workspace, project, or durable task API, so isolation lives in the
// arguments: workspace id, checkout, task id, and a workspace-scoped session
// file. The CLI owns idempotency and the unknown-delivery rule.
type gbotBackend struct {
	cfg Config
}

type gbotTaskResult struct {
	Status      string `json:"status"`
	TaskID      string `json:"taskId"`
	WorkspaceID string `json:"workspaceId"`
	SessionID   string `json:"sessionId"`
	Reply       string `json:"reply"`
	ExitCode    int    `json:"exitCode"`
}

func (b *gbotBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if opts.WorkspaceID == "" {
		return nil, errors.New("gbot requires the Multica workspace id")
	}
	if opts.Cwd == "" {
		return nil, errors.New("gbot requires the task checkout")
	}
	if b.cfg.TaskID == "" {
		return nil, errors.New("gbot requires the Multica task id")
	}
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = "gbot"
	}
	lookedUp, err := exec.LookPath(execName)
	if err != nil {
		return nil, fmt.Errorf("gbot executable not found at %q: %w", execName, err)
	}
	bot := strings.TrimSpace(b.cfg.Env["MULTICA_GBOT_BOT"])
	if bot == "" {
		return nil, errors.New("gbot agent names no bot: set `gbot: <bot name>` on the agent in the project manifest and run sync")
	}
	if err := b.checkBot(ctx, execName, lookedUp, bot); err != nil {
		return nil, err
	}
	issue := strings.TrimSpace(b.cfg.Env["MULTICA_ISSUE_IDENTIFIER"])
	sessionFile := gbotSessionFile(b.cfg.Env, opts.WorkspaceID, bot, issue, b.cfg.TaskID)
	args := []string{
		"task", "run",
		"--workspace-id", opts.WorkspaceID,
		"--bot", bot,
		"--task-id", b.cfg.TaskID,
		"--cwd", opts.Cwd,
		"--session-file", sessionFile,
		"--json",
	}
	if issue != "" {
		args = append(args, "--issue", issue)
	}
	if strings.TrimSpace(prompt) != "" {
		args = append(args, "--", prompt)
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	runCtx, cancel := runContext(ctx, timeout)
	cmd, _, _ := b.cfg.commandAt(execName).execVia(runCtx, chooseDirectInvocation, lookedUp, args, b.cfg.Logger)
	cmd.Dir = opts.Cwd
	cmd.Env = buildEnv(b.cfg.Env)
	cmd.WaitDelay = 10 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	messages := make(chan Message, 16)
	results := make(chan Result, 1)
	started := time.Now()
	go func() {
		defer cancel()
		defer close(messages)
		defer close(results)
		err := cmd.Run()
		results <- gbotResult(stdout.Bytes(), stderr.Bytes(), err, time.Since(started))
	}()
	messages <- Message{Type: MessageText, Content: prompt}
	return &Session{Messages: messages, Result: results}, nil
}

func gbotResult(stdout, stderr []byte, runErr error, elapsed time.Duration) Result {
	result := Result{Status: "failed", DurationMs: elapsed.Milliseconds()}
	parsed := gbotTaskResult{}
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &parsed); err != nil {
		result.Error = "gbot returned no task result"
		if runErr != nil {
			result.Error = runErr.Error()
		}
		return result
	}
	result.SessionID = parsed.SessionID
	result.Output = parsed.Reply
	switch parsed.Status {
	case "completed":
		result.Status = "completed"
	case "unknown":
		result.Status = "failed"
		result.Error = "gbot delivery is unknown; inspect the bot thread before retrying"
	default:
		result.Status = "failed"
		if parsed.Reply != "" {
			result.Error = parsed.Reply
		} else if runErr != nil {
			result.Error = runErr.Error()
		} else {
			result.Error = "gbot task failed"
		}
	}
	return result
}

// GbotArtifactRootEnv overrides where gbot keeps run state outside checkouts.
const GbotArtifactRootEnv = "MULTICA_ARTIFACT_ROOT"

// ArtifactRoot is the per-project state root, the sibling of the worktrunk
// worktree root (~/.cursor/worktrees/<repo>): ~/.cursor/artifacts/<repo>.
// Multica's own run state (gbot sessions) lives under the "multica" key.
func ArtifactRoot(env map[string]string, project string) string {
	base := strings.TrimSpace(env[GbotArtifactRootEnv])
	if base == "" {
		base = strings.TrimSpace(os.Getenv(GbotArtifactRootEnv))
	}
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = os.TempDir()
		}
		base = filepath.Join(home, ".cursor", "artifacts")
	}
	return filepath.Join(base, gbotSlug(project))
}

// gbotSessionFile keeps one session per issue (per task when there is no
// issue) for one bot in one workspace. It lives outside the checkout so a
// recycled worktree does not lose the continuation cursor.
func gbotSessionFile(env map[string]string, workspaceID, bot, issue, taskID string) string {
	key := issue
	if key == "" {
		key = "task-" + taskID
	}
	return filepath.Join(ArtifactRoot(env, "multica"), "gbot", gbotSlug(workspaceID), gbotSlug(bot), gbotSlug(key)+".session.json")
}

func gbotSlug(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "_"
	}
	return out
}

// checkBot fails the run when the named bot does not exist, instead of
// delivering the task to some other bot.
func (b *gbotBackend) checkBot(ctx context.Context, execName, lookedUp, bot string) error {
	checkCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd, _, _ := b.cfg.commandAt(execName).execVia(checkCtx, chooseDirectInvocation, lookedUp, []string{"bots", "get", bot, "--json"}, b.cfg.Logger)
	cmd.Env = buildEnv(b.cfg.Env)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gbot bot %q not found or unreachable (gbot bots get failed: %v); create it with sync or fix `gbot:` on the agent", bot, err)
	}
	var rec struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &rec); err != nil || rec.ID == "" {
		return fmt.Errorf("gbot bot %q not found: gbot bots get returned no record", bot)
	}
	if rec.Kind == "group" {
		return fmt.Errorf("gbot target %q is a group, not a bot", bot)
	}
	return nil
}

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
	bot := os.Getenv("MULTICA_GBOT_BOT")
	if bot == "" {
		bot = "Grok Bot"
	}
	sessionFile := filepath.Join(opts.Cwd, ".multica", "gbot", opts.WorkspaceID+".session.json")
	args := []string{
		"task", "run",
		"--workspace-id", opts.WorkspaceID,
		"--bot", bot,
		"--task-id", b.cfg.TaskID,
		"--cwd", opts.Cwd,
		"--session-file", sessionFile,
		"--json",
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

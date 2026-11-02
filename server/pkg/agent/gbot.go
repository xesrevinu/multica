package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

func chooseDirectInvocation(_, lookedUp string, args []string, _ *slog.Logger) (string, []string) {
	return lookedUp, args
}

// gbotBackend runs one Multica task through the gbot CLI. The Grok Bot surface
// has no workspace or durable task API, so isolation is the CLI arguments:
// workspace id, checkout, task id, and (by default) a session file under
// MULTICA_ARTIFACT_ROOT. The CLI owns idempotency, scope reattach, and the
// unknown-delivery rule — Multica always passes the current Multica task id.
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
	FailureKind string `json:"failureKind"`
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
	// Always pass the current Multica task id. gbot reattaches in-flight
	// deliveries by (workspace, bot, issue) scope so T5_rerun does not mint a
	// second cloud continue into a busy thread.
	args := []string{
		"bot", "task", "run",
		"--workspace-id", opts.WorkspaceID,
		"--bot", bot,
		"--task-id", b.cfg.TaskID,
		"--cwd", opts.Cwd,
		"--json",
	}
	if issue != "" {
		args = append(args, "--issue", issue)
	}
	if strings.TrimSpace(prompt) != "" {
		args = append(args, "--", prompt)
	}
	// Zero Timeout means no wall-clock deadline (same as other backends /
	// runContext / MUL-3064). Do not invent a gbot-only 30m default — that
	// SIGKILLed long Multica runs and surfaced as a false process crash (FX-179).
	runCtx, cancel := runContext(ctx, opts.Timeout)
	cmd, _, _ := b.cfg.commandAt(execName).execVia(runCtx, chooseDirectInvocation, lookedUp, args, b.cfg.Logger)
	cmd.Dir = opts.Cwd
	cmd.Env = buildEnv(b.cfg.Env)
	cmd.WaitDelay = 10 * time.Second

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("gbot stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("gbot stderr pipe: %w", err)
	}

	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	messages := make(chan Message, 16)
	results := make(chan Result, 1)
	started := time.Now()

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("gbot start: %w", err)
	}

	go func() {
		defer cancel()
		defer close(messages)
		defer close(results)

		var stdoutBuf bytes.Buffer
		var stderrBuf bytes.Buffer
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = io.Copy(&stdoutBuf, stdoutPipe)
		}()
		go func() {
			defer wg.Done()
			forwardGbotStderr(stderrPipe, &stderrBuf, messages)
		}()
		wg.Wait()
		err := cmd.Wait()
		results <- gbotResult(stdoutBuf.Bytes(), stderrBuf.Bytes(), err, time.Since(started))
	}()

	messages <- Message{Type: MessageText, Content: prompt}
	return &Session{Messages: messages, Result: results}, nil
}

// forwardGbotStderr copies stderr for diagnostics and turns heartbeat NDJSON
// lines into MessageStatus so the daemon idle watchdog sees activity without
// posting transcript spam. Orchestrator idle_min still uses server task
// messages; gbot runs keep idle_min_by_backend gbot:-1 until that path exists.
func forwardGbotStderr(r io.Reader, buf *bytes.Buffer, messages chan<- Message) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		buf.WriteString(line)
		buf.WriteByte('\n')
		var event struct {
			Type   string `json:"type"`
			TaskID string `json:"taskId"`
		}
		if json.Unmarshal([]byte(line), &event) != nil || event.Type != "heartbeat" {
			continue
		}
		content := "gbot heartbeat"
		if event.TaskID != "" {
			content = "gbot heartbeat " + event.TaskID
		}
		select {
		case messages <- Message{Type: MessageStatus, Content: content}:
		default:
			// Drop when the drain loop is behind; another heartbeat will follow.
		}
	}
}

func gbotResult(stdout, stderr []byte, runErr error, elapsed time.Duration) Result {
	result := Result{Status: "failed", DurationMs: elapsed.Milliseconds()}
	parsed := gbotTaskResult{}
	raw := bytes.TrimSpace(stdout)
	// New gbot --json prints one result object on stdout. Keep a last-object
	// recovery only for mixed-version CLIs that still emit a banner (OX-220).
	if err := json.Unmarshal(raw, &parsed); err != nil {
		if obj := lastJSONObject(raw); len(obj) == 0 || json.Unmarshal(obj, &parsed) != nil {
			result.Error = gbotNoResultError(runErr, stderr, raw)
			return result
		}
	}
	result.SessionID = parsed.SessionID
	result.Output = parsed.Reply
	switch parsed.Status {
	case "completed":
		result.Status = "completed"
	case "unknown":
		result.Status = "failed"
		if parsed.FailureKind == "unknown_delivery" {
			result.Error = "gbot failureKind=unknown_delivery; inspect the bot thread before retrying"
		} else {
			result.Error = "gbot delivery is unknown; inspect the bot thread before retrying"
		}
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

// lastJSONObject returns the last top-level JSON object in b, or nil.
func lastJSONObject(b []byte) []byte {
	for i := bytes.LastIndexByte(b, '{'); i >= 0; i = bytes.LastIndexByte(b[:i], '{') {
		dec := json.NewDecoder(bytes.NewReader(b[i:]))
		var raw json.RawMessage
		if err := dec.Decode(&raw); err == nil {
			return raw
		}
		if i == 0 {
			break
		}
	}
	return nil
}

func gbotNoResultError(runErr error, stderr, stdout []byte) string {
	parts := make([]string, 0, 3)
	if runErr != nil {
		parts = append(parts, runErr.Error())
	} else {
		parts = append(parts, "gbot returned no task result")
	}
	if s := strings.TrimSpace(string(stderr)); s != "" {
		parts = append(parts, "stderr: "+shortTail(s, 800))
	}
	if s := strings.TrimSpace(string(stdout)); s != "" {
		parts = append(parts, "stdout: "+shortTail(s, 400))
	}
	return strings.Join(parts, "; ")
}

func shortTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// GbotArtifactRootEnv overrides where gbot keeps run state outside checkouts.
// Kept exported so tests and docs can set the same variable the CLI reads.
const GbotArtifactRootEnv = "MULTICA_ARTIFACT_ROOT"

// checkBot fails the run when the named bot does not exist, instead of
// delivering the task to some other bot.
func (b *gbotBackend) checkBot(ctx context.Context, execName, lookedUp, bot string) error {
	checkCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd, _, _ := b.cfg.commandAt(execName).execVia(checkCtx, chooseDirectInvocation, lookedUp, []string{"bot", "get", bot, "--json"}, b.cfg.Logger)
	cmd.Env = buildEnv(b.cfg.Env)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gbot bot %q not found or unreachable (gbot bot get failed: %v); create it with sync or fix `gbot:` on the agent", bot, err)
	}
	var rec struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &rec); err != nil || rec.ID == "" {
		return fmt.Errorf("gbot bot %q not found: gbot bot get returned no record", bot)
	}
	if rec.Kind == "group" {
		return fmt.Errorf("gbot target %q is a group, not a bot", bot)
	}
	return nil
}

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// yieldedBlockedArgs are the flags this backend owns. The prompt is the
// raw stdin body, so these must not be rewritten by custom args. `-p` and
// `--mode` are not part of the contract; they stay blocked so a custom arg
// cannot put them back.
var yieldedBlockedArgs = map[string]blockedArgMode{
	"models":             blockedStandalone,
	"--models":           blockedWithValue,
	"--list-models":      blockedStandalone,
	"-p":                 blockedStandalone,
	"--print":            blockedStandalone,
	"--mode":             blockedWithValue,
	"--model":            blockedWithValue,
	"--thinking":         blockedWithValue,
	"--session":          blockedWithValue,
	"--session-id":       blockedWithValue,
	"--no-session":       blockedStandalone,
	"--no-context-files": blockedStandalone,
	"--skill":            blockedWithValue,
	"--no-skills":        blockedStandalone,
	"--no-mcp":           blockedStandalone,
	"--system-prompt":    blockedWithValue,
	"--append-system":    blockedWithValue,
	"--tools":            blockedWithValue,
	"--exclude-tools":    blockedWithValue,
	"--no-tools":         blockedStandalone,
	"--help":             blockedStandalone,
	"-h":                 blockedStandalone,
}

// yieldedMissingSession is the startup error yielded-agent emits when
// --session names an id with no sqlite file. It is the only failure this
// backend reports as Result.ResumeRejected.
const yieldedMissingSession = "session not found"

// yieldedBackend runs one yielded-agent process:
// `yielded-agent --model <selector> --thinking <level> --session <id>`, with
// the prompt written to stdin and closed. Stdout is one JSON event per line.
// The process exits when the turn finishes.
type yieldedBackend struct {
	cfg Config
}

func (b *yieldedBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "yielded-agent"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("yielded-agent executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	args := []string{}
	if model := strings.TrimSpace(opts.Model); model != "" {
		args = append(args, "--model", model)
	}
	if level := strings.TrimSpace(opts.ThinkingLevel); level != "" {
		args = append(args, "--thinking", level)
	}
	if id := strings.TrimSpace(opts.ResumeSessionID); id != "" {
		args = append(args, "--session", id)
	}
	if sp := strings.TrimSpace(opts.SystemPrompt); sp != "" {
		// Runtime brief inline: the tracked AGENTS.md in the worktree stays clean.
		args = append(args, "--append-system", opts.SystemPrompt)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, yieldedBlockedArgs, b.cfg.Logger)...)
	cmd := b.cfg.commandAt(execPath).exec(runCtx, args...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("yielded-agent stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("yielded-agent stdin pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("yielded-agent stderr pipe: %w", err)
	}
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start yielded-agent: %w", err)
	}
	go func() {
		_, _ = io.Copy(newLogWriter(b.cfg.Logger, "[yielded:stderr] "), stderr)
	}()

	if _, err := io.WriteString(stdin, prompt); err != nil {
		cancel()
		return nil, fmt.Errorf("yielded-agent stdin: %w", err)
	}
	_ = stdin.Close()

	msgCh := make(chan Message, 64)
	resCh := make(chan Result, 1)
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			_ = cmd.Wait()
			releaseProcessGroup(cmd)
		}()

		start := time.Now()
		var text strings.Builder
		var output string
		var sessionID string
		status := "completed"
		var errText string
		resumeRejected := false
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var event struct {
				Type      string         `json:"type"`
				Delta     string         `json:"delta"`
				ID        string         `json:"id"`
				Name      string         `json:"name"`
				Input     map[string]any `json:"input"`
				Output    string         `json:"output"`
				Status    string         `json:"status"`
				Text      string         `json:"text"`
				SessionID string         `json:"sessionId"`
				Stop      string         `json:"stop"`
				Phase     string         `json:"phase"`
				Message   string         `json:"message"`
			}
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				continue
			}
			switch event.Type {
			case "text":
				text.WriteString(event.Delta)
				trySend(msgCh, Message{Type: MessageText, Content: event.Delta})
			case "reasoning":
				trySend(msgCh, Message{Type: MessageThinking, Content: event.Delta})
			case "tool":
				trySend(msgCh, Message{Type: MessageToolUse, Tool: event.Name, CallID: event.ID, Input: event.Input})
			case "tool_result":
				trySend(msgCh, Message{Type: MessageToolResult, Tool: event.Name, CallID: event.ID, Output: event.Output, Status: event.Status})
			case "done":
				output = event.Text
				sessionID = event.SessionID
				if event.Stop == "cancelled" {
					status = "aborted"
					errText = "yielded-agent cancelled the turn"
				} else if event.Stop == "length" {
					status = "failed"
					errText = "yielded-agent stopped because the turn hit its length limit"
				}
			case "error":
				status = "failed"
				errText = event.Message
				if strings.TrimSpace(opts.ResumeSessionID) != "" && event.Phase == "startup" && event.Message == yieldedMissingSession {
					resumeRejected = true
				}
			}
		}
		if err := scanner.Err(); err != nil && status == "completed" {
			status = "failed"
			errText = err.Error()
		}
		if runCtx.Err() == context.DeadlineExceeded {
			status = "timeout"
			errText = fmt.Sprintf("yielded-agent timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled && status == "completed" {
			status = "aborted"
			errText = "execution cancelled"
		}
		if output == "" {
			output = text.String()
		}
		resCh <- Result{
			Status:         status,
			Output:         output,
			Error:          errText,
			DurationMs:     time.Since(start).Milliseconds(),
			SessionID:      sessionID,
			ResumeRejected: resumeRejected,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}
